package v4host

import (
	"context"
	"errors"
	"log"
	"sort"
	"sync"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/device/discover"
)

// probeWorkers 扫描并发探测数（与 v2 扫描的 runIdxPool 同构，16 worker）：
// 探测是 IO 密集的局域网 HTTP 请求，单台超时 1.5s，16 并发把大候选池的
// 最坏等待压到 ~2 批 × 1.5s；开销仅 16 个 goroutine + HTTP 连接，无压力
const probeWorkers = 16

// Discoverer 发现层抽象：UDP 7678 广播（internal/device/discover.Discoverer 满足）
type Discoverer interface {
	Discover(ctx context.Context) ([]discover.Device, error)
}

// Result 单台 V4 设备的扫描结果（前端雷达/日志展示用）
type Result struct {
	IP         string `json:"ip"`
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
	Version    string `json:"version"`
	Online     bool   `json:"online"`   // /health 验证通过
	IsNew      bool   `json:"isNew"`    // 本轮新进「待添加」
	IDFromIP   bool   `json:"idFromIp"` // deviceId 由 IP 兜底（/health 无 ID 字段）
}

// Scanner V4 扫描器：UDP 7678 广播拿候选 IP → 8088 /health 验证 → 按 DeviceID 落库 + 推事件。
// 不做型号白名单（V4 无白名单信息）；也不复用 v3 的 LocalDevices 版本过滤——
// V4 设备的广播名后缀格式未定，按版本硬过滤会把合法 V4 设备全丢掉。
// 对广播名后缀判为 V2/V3 的候选只做「降级探测」（短超时 /health，见 Scan），
// 不直接排除：tebox 可能装在 V3 盒子上，这些设备的 8088 /health 是通的。
type Scanner struct {
	discoverer Discoverer
	cm         *config.Manager
	// emit 事件推送（main 包注入 app.Event.Emit；nil = 不推送，便于单测）
	emit func(event string, data any)
	// health 探测实现（默认 Health，单测注入假实现）
	health func(ctx context.Context, ip string) (*Info, error)
	// healthQuick 短超时探测实现（默认 HealthQuick，用于 V2/V3 名候选降级探测）
	healthQuick func(ctx context.Context, ip string) (*Info, error)
}

// NewScanner 创建 V4 扫描器
func NewScanner(d Discoverer, cm *config.Manager) *Scanner {
	return &Scanner{discoverer: d, cm: cm, health: Health, healthQuick: HealthQuick}
}

// SetEmitter 注入事件推送函数（pendinglist:updated / devicelist:updated）
func (s *Scanner) SetEmitter(fn func(event string, data any)) { s.emit = fn }

// Scan 一轮 V4 扫描。返回本轮验证通过的设备（含在线但已在正式列表的）。
//
// commitValid 是提交前校验钩子（main 包传入「模式代际未变」的判定）：探测是异步的，
// 期间用户可能切走模式——此时必须丢弃本轮结果，不能把 V4 设备写进别的模式的数据桶。
// 传 nil 表示总是有效（单测/无代际语义的调用方）。
//
// 空结果安全：发现层无回复（V4 设备不响应 UDP 7678 广播）时返回 (nil, nil)，
// 不做任何写入——这是已知限制（见包文档）。
func (s *Scanner) Scan(ctx context.Context, commitValid func() bool) ([]Result, error) {
	if s.cm == nil {
		return nil, errors.New("v4 扫描未注入 config.Manager")
	}
	if s.discoverer == nil {
		return nil, errors.New("v4 扫描未注入发现器")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	found, err := s.discoverer.Discover(ctx)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		log.Printf("[v4] UDP 发现无回复：tebox 设备需响应 UDP 7678 广播才能被扫描到，本轮结果为空")
		return nil, nil
	}

	// 同一 IP 多包回复只保留首个；按 IP 排序保证结果顺序稳定
	byIP := make(map[string]discover.Device, len(found))
	for _, dev := range found {
		if dev.IP == "" {
			continue
		}
		if _, dup := byIP[dev.IP]; dup {
			continue
		}
		byIP[dev.IP] = dev
	}
	ips := make([]string, 0, len(byIP))
	for ip := range byIP {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	if len(ips) == 0 {
		return nil, nil
	}

	// V2/V3 名候选不再直接排除，改为「降级探测」：广播名后缀（ParseVersion）判为
	// v2/v3 的设备大概率 8088 上没有 tebox，但 tebox 确实可能装在 V3 盒子上
	//（此时 /health 是通的），硬过滤会导致这类设备永远无法被自动发现。因此：
	//   - 已在 v4 正式列表的（按 IP，兼容 DeviceID 为空的旧条目）：直接跳过不探测，
	//     状态由别的心跳/状态上报维护，扫描不重复验证；
	//   - 其余的用短超时（500ms，v0/v1 候选为 1.5s）探测，失败静默丢弃（与 v0
	//     候选探测失败同路径）。V4 设备的广播名后缀格式未确认（可能解析为 v0
	//     裸名），所以 v0/v1/空版本仍走常规探测。
	probeIPs := make([]string, 0, len(ips))
	probeQuick := make([]bool, 0, len(ips))
	skippedInList := 0
	for _, ip := range ips {
		if s.cm.GetDeviceByIP(ip) != nil {
			skippedInList++
			continue
		}
		probeIPs = append(probeIPs, ip)
		probeQuick = append(probeQuick, byIP[ip].Version == "v2" || byIP[ip].Version == "v3")
	}
	if skippedInList > 0 {
		log.Printf("[v4] 扫描: 跳过 %d 台已在列表的设备（IP 命中，不重复探测）", skippedInList)
	}
	if len(probeIPs) == 0 {
		return nil, nil
	}

	// 并发 /health 验证（worker 只写各自下标，无共享写竞态）。
	// V2/V3 名候选用短超时探测（probeQuick 标记），失败静默丢弃 = 非 tebox 设备。
	results := make([]Result, len(probeIPs))
	infos := make([]*Info, len(probeIPs))
	runIdxPool(ctx, len(probeIPs), func(idx int) {
		ip := probeIPs[idx]
		probe := s.health
		if probeQuick[idx] {
			probe = s.healthQuick
		}
		info, err := probe(ctx, ip)
		if err != nil {
			return // 8088 不可达/非 200/非 JSON = 非 V4 设备（含未装 tebox 的 V2/V3）
		}
		infos[idx] = info
		results[idx] = Result{
			IP:         ip,
			DeviceID:   info.DeviceID,
			DeviceName: info.DeviceName,
			Online:     true,
			IDFromIP:   info.DeviceIDFromIP,
		}
	})
	// 探测失败数在池跑完后统计（只读 worker 写好的 results，无竞态）
	probeFailed := 0
	for idx := range probeIPs {
		if !results[idx].Online {
			probeFailed++
		}
	}

	// 提交前代际校验：扫描期间模式已切换则整体丢弃（不写任何数据桶）
	if commitValid != nil && !commitValid() {
		log.Printf("[v4] 模式已切换，丢弃本轮 V4 扫描结果")
		return nil, nil
	}
	// 模式守卫（兜底）：写入入口按「当前 mode」取数据桶指针，若此刻已不在 v4，
	// 落库会写进别的模式的列表——无论如何都要拦下。
	if s.cm.GetDeployMode() != "v4" {
		log.Printf("[v4] 当前部署模式已不是 v4，丢弃本轮 V4 扫描结果")
		return nil, nil
	}

	online := make([]Result, 0, len(probeIPs))
	pendingAdded := 0
	listChanged := false
	for idx, ip := range probeIPs {
		res := results[idx]
		if !res.Online {
			continue
		}
		udpDev := byIP[ip]
		info := infos[idx]
		cacheItem := config.DeviceCache{
			DeviceID:   res.DeviceID,
			IP:         ip,
			DeviceType: deviceType(info, udpDev),
			DeviceName: deviceName(info, udpDev),
			// V2/V3 名候选通过降级探测 = 装了 tebox 的 V3 盒子，版本原样保留（v2/v3）；
			// V4 版本标识未定，UDP 回复带的版本同样原样保留
			Version: udpDev.Version,
			State:   0, // /health 通过 = 在线
			Order:   0,
		}
		res.DeviceName = cacheItem.DeviceName
		res.Version = cacheItem.Version

		if s.cm.GetDevice(res.DeviceID) != nil {
			// 已在正式列表：刷新信息 + 置在线
			if s.cm.UpsertDeviceCheck(cacheItem) {
				listChanged = true
			}
			s.cm.UpdateDeviceState(res.DeviceID, 0)
		} else {
			// 不在正式列表：进待添加（自动扫描不自动加设备）
			s.cm.UpsertPendingDevice(cacheItem)
			res.IsNew = true
			pendingAdded++
		}
		online = append(online, res)
	}

	if listChanged {
		s.cm.SortDeviceListByIP()
	}
	log.Printf("[v4] 扫描完成: UDP 回复 %d 台, 跳过已在列表 %d 台, /health 通过 %d 台, 探测失败丢弃 %d 台, 新进待添加 %d 台",
		len(found), skippedInList, len(online), probeFailed, pendingAdded)

	if pendingAdded > 0 {
		s.notify("pendinglist:updated", s.cm.GetPendingDevices())
	}
	if listChanged {
		s.notify("devicelist:updated", s.cm.GetDeviceList())
	}
	return online, nil
}

func (s *Scanner) notify(event string, data any) {
	if s.emit == nil {
		return
	}
	s.emit(event, data)
}

// deviceName 显示名优先取 /health（V4 权威来源），缺字段时退回 UDP 回复的设备名
func deviceName(info *Info, udpDev discover.Device) string {
	if info != nil && info.DeviceName != "" {
		return info.DeviceName
	}
	return udpDev.DeviceName
}

// deviceType 型号优先取 UDP 回复的真实型号（与 local/v2 一致，创建表单按它过滤镜像），
// 无 UDP 型号时退回 /health 的 model 字段
func deviceType(info *Info, udpDev discover.Device) string {
	if udpDev.DeviceType != "" {
		return udpDev.DeviceType
	}
	if info != nil {
		return info.Model
	}
	return ""
}

// ProbeIPs 定向探测一组 IP（手动添加用）：逐台 /health 验证。
// 返回成功的设备信息与无响应 IP（顺序与入参一致，便于前端展示）。
func (s *Scanner) ProbeIPs(ctx context.Context, ips []string) ([]*Info, []string) {
	if ctx == nil {
		ctx = context.Background()
	}
	infos := make([]*Info, len(ips))
	ok := make([]bool, len(ips))
	runIdxPool(ctx, len(ips), func(idx int) {
		info, err := s.health(ctx, ips[idx])
		if err != nil {
			return
		}
		infos[idx] = info
		ok[idx] = true
	})

	found := make([]*Info, 0, len(ips))
	var failed []string
	for idx, ip := range ips {
		if ok[idx] {
			found = append(found, infos[idx])
			continue
		}
		failed = append(failed, ip)
	}
	return found, failed
}

// runIdxPool 并发池（16 worker，与 main 包 v2 扫描的 runIdxPool 同构）：
// 把 [0,n) 下标分发给 worker 执行 fn；响应 ctx 取消——已分发的跑完，未分发的跳过。
func runIdxPool(ctx context.Context, n int, fn func(idx int)) {
	idxCh := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < probeWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[PANIC] v4 probe worker recovered: %v", r)
				}
			}()
			for idx := range idxCh {
				select {
				case <-ctx.Done():
					return
				default:
				}
				fn(idx)
			}
		}()
	}
feed:
	for i := 0; i < n; i++ {
		select {
		case idxCh <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(idxCh)
	wg.Wait()
}
