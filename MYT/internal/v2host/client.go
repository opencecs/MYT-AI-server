package v2host

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	// DefaultPort V2 主机 Docker Engine API 端口（与 V2 Web 端 docker.sdk.js 的 _port 一致）
	DefaultPort = 2375
	// SuperSdkPort 超级 SDK（myt_sdk 容器）Web API 端口：宿主机 81 → 容器 5000
	SuperSdkPort = 81
	// requestTimeout 单请求超时：局域网探测要快失败，与 UDP 发现的 listenTimeout 同量级
	requestTimeout = 2500 * time.Millisecond
	// maxConcurrentProbes 批量探测并发上限（扫描需对全量 UDP 回复设备验证 2375）
	maxConcurrentProbes = 16
)

// Client V2 主机 Docker API 客户端（无状态，可并发复用）
type Client struct {
	port    int
	sdkPort int          // myt_sdk 容器 Web API 端口（默认 81，单测可注入 httptest 端口）
	hc      *http.Client // 探测/列表用（快速失败）
	opsHC   *http.Client // 主机操作用（插件安装/更新可达分钟级，长超时）
	pullHC  *http.Client // 镜像拉取用（无上限，超时由调用方 ctx 控制）
	sdkHC   *http.Client // mytsdk 81 API 专用：接口常态响应 3s+（内部串行调 Docker
	//             API 组装数据），2.5s 短超时必然超时——实测「列表永远为空」的根因
}

func NewClient() *Client {
	transport := &http.Transport{Proxy: nil} // 不走系统代理：内网直连，代理会导致全部失败
	return &Client{
		port:    DefaultPort,
		sdkPort: SuperSdkPort,
		hc: &http.Client{
			Timeout:   requestTimeout,
			Transport: transport,
		},
		opsHC: &http.Client{
			Timeout:   150 * time.Second,
			Transport: transport,
		},
		pullHC: &http.Client{
			Timeout:   0, // 镜像拉取分钟级，靠调用方 ctx 控制超时
			Transport: transport,
		},
		sdkHC: &http.Client{
			Timeout:   15 * time.Second, // mytsdk 81 接口常态 3s+，忙时更久；总闸由调用方 ctx（20s）
			Transport: transport,
		},
	}
}

// SDKBase myt_sdk 容器 Web API 根地址（81 端口，导出供 main 包服务层拼 URL）
func (c *Client) SDKBase(ip string) string { return c.sdkBaseURL(ip) }

// OpsHTTPClient 长超时 HTTP 客户端（操作类请求复用，导出版）
func (c *Client) OpsHTTPClient() *http.Client { return c.opsHC }

// baseURL 拼主机 API 根地址
func (c *Client) baseURL(ip string) string {
	return fmt.Sprintf("http://%s:%d", ip, c.port)
}

// sdkBaseURL myt_sdk 容器的 Web API 根地址（默认 81 端口：宿主机 5000→容器 5000，
// Docker 映射 81；V2 Web 的主机详情/系统信息都走这里。sdkPort 单测可注入）
func (c *Client) sdkBaseURL(ip string) string {
	return fmt.Sprintf("http://%s:%d", ip, c.sdkPort)
}

// SdkContainerEntry 81 /dc_api/v1/list 的容器条目（mytsdk 加工视图：
// index/adb/api_port/webrtc 均为现成值，只含云机容器——系统容器已被过滤）
type SdkContainerEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Index   int    `json:"index"`
	Adb     int    `json:"adb"`
	APIPort int    `json:"api_port"`
	CtrPort int    `json:"ctr_port"`
	WebRTC  int    `json:"webrtc"`
	Width   string `json:"width"`
	Height  string `json:"height"`
	DPI     string `json:"dpi"`
}

// ListContainersViaSDK 经 81 查容器列表（壳子同款通道）。
// 主用 /dc_api/v1/get/{ip}，失败（旧版 mytsdk 无此端点）回退 /dc_api/v1/list/{ip}——
// 两者返回格式一致（{code, data:[容器数组]}）。
// 无 NAT 端口明细（PortBindings 空，截图/投屏端口由调用方公式兜底）。
func (c *Client) ListContainersViaSDK(ctx context.Context, ip string) ([]SdkContainerEntry, error) {
	entries, err := c.fetchContainerList(ctx, ip, "get")
	if err != nil {
		return c.fetchContainerList(ctx, ip, "list")
	}
	return entries, nil
}

// fetchContainerList 调 81 的 get/list 容器列表端点（sdkHC 15s 超时——
// 接口常态 3s+，勿用 hc 的 2.5s 短超时，见 Client.sdkHC 注释）
func (c *Client) fetchContainerList(ctx context.Context, ip, ep string) ([]SdkContainerEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/dc_api/v1/%s/%s", c.sdkBaseURL(ip), ep, ip), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.sdkHC.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, err
	}
	var r struct {
		Code int                 `json:"code"`
		Data []SdkContainerEntry `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("解析容器列表失败: %w", err)
	}
	if r.Code != 200 && r.Code != 0 {
		return nil, fmt.Errorf("容器列表接口错误: code=%d", r.Code)
	}
	return r.Data, nil
}

// SystemInfo 拉取主机设备信息：GET http://{ip}:81/systeminfo。
// 返回 {code:200, msg:{cpu, devCode, disk_total, disk_percent, mem_total, mem_percent, ...}}。
func (c *Client) SystemInfo(ctx context.Context, ip string) (*SystemInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.sdkBaseURL(ip)+"/systeminfo", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.sdkHC.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	var wrapper struct {
		Code int        `json:"code"`
		Msg  SystemInfo `json:"msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return nil, err
	}
	if wrapper.Code != 200 {
		return nil, fmt.Errorf("code %d", wrapper.Code)
	}
	return &wrapper.Msg, nil
}

// Info 探测主机：GET /info。失败返回 error（含超时/拒绝连接）。
func (c *Client) Info(ctx context.Context, ip string) (*HostInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL(ip)+"/info", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	var info HostInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

// ListContainers 拉取容器列表：GET /containers/json?all=1（含已停止容器）。
func (c *Client) ListContainers(ctx context.Context, ip string) ([]ContainerInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL(ip)+"/containers/json?all=1", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	var list []ContainerInfo
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	return list, nil
}

// ProbeBatch 批量并发探测主机（并发上限 maxConcurrentProbes）。
// 返回结果顺序与输入 IP 一一对应，单台失败不影响其他（OK=false + Err）。
func (c *Client) ProbeBatch(ctx context.Context, ips []string) []ProbeResult {
	results := make([]ProbeResult, len(ips))
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentProbes)
	for i, ip := range ips {
		wg.Add(1)
		go func(idx int, addr string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = c.probeOne(ctx, addr)
		}(i, ip)
	}
	wg.Wait()
	return results
}

// probeOne 探测单台主机
func (c *Client) probeOne(ctx context.Context, ip string) ProbeResult {
	info, err := c.Info(ctx, ip)
	if err != nil {
		return ProbeResult{IP: ip, OK: false, Err: err.Error()}
	}
	return ProbeResult{
		IP:         ip,
		OK:         true,
		HostID:     info.ID,
		HostName:   info.Name,
		Version:    info.ServerVersion,
		Containers: info.Containers,
	}
}
