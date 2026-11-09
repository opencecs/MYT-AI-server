// Package clientreport 客户端启动上报：把本机客户端的启动与日活上报给魔云腾后端。
//
// 接口在服务端按「设备 + 天」去重（Redis），所以客户端侧的本地记录只用于
// 省掉无收益的重复请求，不承担正确性兜底；任何失败一律静默（只记日志），
// 既不阻塞启动，也不影响其他功能。
package clientreport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/util"
)

const (
	// defaultEndpoint 客户端启动上报接口（后端公开接口，无需鉴权）。
	//
	// 端口用 443 而不是文档示例里的 10443：该域名 CNAME 到网宿 CDN，
	// CDN 在 10443 上只回默认证书（default.chinanetcenter.com），
	// 客户端默认 TLS 校验必然失败（x509 主机名不匹配）；443 实测正常、
	// 响应与文档逐字段一致。若后端在 CDN 为 10443 配好 *.moyunteng.com 证书，
	// 把这里改回 ":10443" 即可。
	defaultEndpoint = "https://newapi.moyunteng.com/api/v1/device/startup"

	// clientTypeAI AI 客户端（1 = 老 v3 客户端）
	clientTypeAI = 2

	// reportInterval 跨天检查间隔：长期开机的客户端靠它产生每日上报
	reportInterval = 30 * time.Minute

	// requestTimeout 单次上报请求超时
	requestTimeout = 10 * time.Second
)

// cst 跨天判定所用时区：固定东八区，不随客户端机器时区设置漂移
// （机器时区异常最多导致多报/漏报一次，服务端按天去重兜底）。
var cst = time.FixedZone("CST", 8*3600)

// StateStore 上报状态的持久化接口，由 config.Manager 实现。
type StateStore interface {
	GetClientReportState() config.ReportState
	SaveClientReportState(config.ReportState) error
}

// Options Reporter 构造参数。Store 必填，其余留空时使用默认值。
type Options struct {
	Store   StateStore
	Version string           // 客户端版本（app_service.go 的 Version）
	OSName  string           // 操作系统标识，留空则按平台探测
	UID     func() string    // 当前登录 uid，未登录返回空串
	Enabled bool             // 是否启用上报（当前恒为 true，保留作为能力开关便于测试与临时关闭）
	HTTP    *http.Client     // 可选：注入测试用
	Now     func() time.Time // 可选：注入测试用
	// Endpoint 上报接口地址，留空则用默认地址；测试时注入 httptest 地址
	Endpoint string
}

// payload 上报请求体，字段与后端 /api/v1/device/startup 约定一致。
type payload struct {
	DeviceID      string `json:"device_id"`
	UID           string `json:"uid"`
	IP            string `json:"ip"`
	OS            string `json:"os"`
	ClientVersion string `json:"client_version"`
	StartupTime   int64  `json:"startup_time"`
	ClientType    int    `json:"client_type"`
}

// response 上报响应；Recorded=false 表示当天已上报过（服务端去重命中），仍算本次成功。
type response struct {
	CodeID int    `json:"code_id"`
	Msg    string `json:"msg"`
	Data   struct {
		Recorded bool `json:"recorded"`
	} `json:"data"`
}

// Reporter 上报器：启动上报一次，之后每 reportInterval 检查是否跨天。
type Reporter struct {
	store     StateStore
	endpoint  string
	version   string
	osName    string
	uidFn     func() string
	enabled   bool
	client    *http.Client
	now       func() time.Time
	startedAt int64 // 进程启动时刻（Unix 秒），作为 startup_time 上报

	mu        sync.Mutex // 串行化上报：启动与登录补报可能几乎同时触发
	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

// New 构造上报器。
func New(opts Options) *Reporter {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	client := opts.HTTP
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}
	osName := opts.OSName
	if osName == "" {
		osName = OSName()
	}
	version := opts.Version
	if version == "" {
		version = "dev"
	}
	uidFn := opts.UID
	if uidFn == nil {
		uidFn = func() string { return "" }
	}
	endpoint := opts.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}

	return &Reporter{
		store:     opts.Store,
		endpoint:  endpoint,
		version:   version,
		osName:    osName,
		uidFn:     uidFn,
		enabled:   opts.Enabled,
		client:    client,
		now:       now,
		startedAt: now().Unix(),
		stopCh:    make(chan struct{}),
	}
}

// Start 启动上报循环：先尝试一次启动上报，之后每 reportInterval 检查是否跨天。
// Enabled=false 时不做任何事；重复调用无副作用。
func (r *Reporter) Start() {
	if !r.enabled {
		return
	}
	r.startOnce.Do(func() {
		r.wg.Add(1)
		util.GoSafe(func() {
			defer r.wg.Done()
			r.report("startup")

			ticker := time.NewTicker(reportInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					r.report("daily")
				case <-r.stopCh:
					return
				}
			}
		})
	})
}

// Stop 停止上报循环（幂等）。
func (r *Reporter) Stop() {
	if !r.enabled {
		return
	}
	r.stopOnce.Do(func() { close(r.stopCh) })
	r.wg.Wait()
}

// ReportNow 异步触发一次上报判定（登录成功后调用，让 uid 补上）。
func (r *Reporter) ReportNow(reason string) {
	if !r.enabled {
		return
	}
	util.GoSafe(func() { r.report(reason) })
}

// report 执行一次上报判定。
//
// 判定规则：本地记录的日期与 uid 都与当前一致时跳过，否则上报。
// 覆盖三种场景——跨天（含首次运行）、当天上报时未登录而登录后补报、
// 当天已用相同 uid 上报过则跳过（服务端同样按天去重，重复请求无收益）。
func (r *Reporter) report(reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state, err := r.loadState()
	if err != nil {
		log.Printf("[clientreport] %s 读取本地状态失败: %v", reason, err)
		return
	}

	today := r.now().In(cst).Format("2006-01-02")
	uid := r.uidFn()
	if state.LastDate == today && state.LastUID == uid {
		return
	}

	recorded, err := r.post(state.ClientID, uid)
	if err != nil {
		// 失败不落日期：下一轮 ticker 自然重试
		log.Printf("[clientreport] %s 上报失败: %v", reason, err)
		return
	}

	state.LastDate = today
	state.LastUID = uid
	if err := r.store.SaveClientReportState(state); err != nil {
		log.Printf("[clientreport] %s 保存上报状态失败: %v", reason, err)
		return
	}
	log.Printf("[clientreport] %s 上报成功 recorded=%v uid=%q", reason, recorded, uid)
}

// loadState 读取本地状态；首次运行（无 client_id）时生成并落盘。
func (r *Reporter) loadState() (config.ReportState, error) {
	state := r.store.GetClientReportState()
	if state.ClientID != "" {
		return state, nil
	}
	id, err := newClientID()
	if err != nil {
		return state, err
	}
	state.ClientID = id
	if err := r.store.SaveClientReportState(state); err != nil {
		return state, err
	}
	log.Printf("[clientreport] 生成客户端标识 %s", id)
	return state, nil
}

// post 发送一次上报，返回服务端的 recorded 值。
func (r *Reporter) post(deviceID, uid string) (bool, error) {
	body, err := json.Marshal(payload{
		DeviceID:      deviceID,
		UID:           uid,
		IP:            "", // 刻意留空：客户端在 NAT 后自知的只是内网地址，由服务端按请求来源解析地区
		OS:            r.osName,
		ClientVersion: r.version,
		StartupTime:   r.startedAt,
		ClientType:    clientTypeAI,
	})
	if err != nil {
		return false, err
	}

	req, err := http.NewRequest(http.MethodPost, r.endpoint, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return false, err
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var res response
	if err := json.Unmarshal(data, &res); err != nil {
		return false, fmt.Errorf("响应解析失败: %w", err)
	}
	if res.CodeID != 200 {
		return false, fmt.Errorf("code_id=%d msg=%s", res.CodeID, res.Msg)
	}
	return res.Data.Recorded, nil
}
