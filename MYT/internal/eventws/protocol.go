// Package eventws 实现 SDK v206+ 的 /ws/events 事件推送通道客户端。
// 协议文档：myt-sdk-go/docs/event-push-client-guide.md
//
// 本文件：帧定义、解析、seq 断档检测、boot 事件状态映射。纯逻辑，无 IO。
package eventws

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// 帧类型（服务端 → 客户端四种）
const (
	frameHello          = "hello"
	frameSnapshot       = "snapshot"
	frameEvent          = "event"
	frameResyncRequired = "resync-required"
)

// 事件类型
const (
	EventContainer = "container"
	EventBoot       = "boot"
	EventSystem     = "system"
)

// HelloFrame 建连第一帧。身份字段（DeviceID/DeviceVersion/Model）为 v206+ 可缺省。
type HelloFrame struct {
	Proto         int    `json:"proto"`
	SDKVersion    string `json:"sdkVersion"`
	ServerTime    int64  `json:"serverTime"`
	DeviceID      string `json:"deviceId,omitempty"`
	DeviceVersion string `json:"deviceVersion,omitempty"`
	Model         string `json:"model,omitempty"`
	// Tags 本机标签名全集（设备端 v208+，omitempty：无标签/老版本零开销）。
	// 数据源是设备端 container_tags 全表跨容器去重后的扁平标签名字典
	//（与 GET /android/tags 同源同序）——注意是设备级全集，不含容器归属；
	// 容器级 tags 仍以 snapshot/stats 帧逐容器下发的为准。
	Tags []string `json:"tags,omitempty"`
}

// SDKVersionNum SDK 版本数值（"v206" → 206）。设备重启（升级/回退/重启）必经
// WS 重连 hello——这是设备当前 SDK 版本最权威的实时源（前端版本缓存的失效信号）。
// 格式异常/缺省返回 0（调用方忽略）。
func (h *HelloFrame) SDKVersionNum() int {
	if h == nil || h.SDKVersion == "" {
		return 0
	}
	v, err := strconv.Atoi(strings.TrimPrefix(h.SDKVersion, "v"))
	if err != nil || v <= 0 {
		return 0
	}
	return v
}

// PortBindingItem Docker 端口映射条目（与 sdkapi/config 的 PortBinding 同构）。
type PortBindingItem struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

// SnapshotItem snapshot.data.list 元素，与 GET /android 响应列表项同构
// （JSON 键一致，字段名对齐 internal/sdkapi 的 androidInfo；该类型未导出，此处独立定义，
// 由 androidlist 集成层做转换——与现有 REST 查询的 sdkapi→config 转换同构）。
type SnapshotItem struct {
	ID           string                          `json:"id"`
	Name         string                          `json:"name"`
	Status       string                          `json:"status"`
	Image        string                          `json:"image"`
	IndexNum     int                             `json:"indexNum"`
	IPAddress    string                          `json:"ip"`
	NetworkName  string                          `json:"networkName"`
	PortBindings map[string][]PortBindingItem     `json:"portBindings"`
	ModelPath    string                          `json:"modelPath"`
	CPUUsage     float64                         `json:"cpuUsage"`
	MemUsage     int64                           `json:"memUsage"`
	MemLimit     int64                           `json:"memLimit"`
	DiskUsage    int64                           `json:"diskUsage"`
	// Tags 容器标签（快照复用 GET /android 列表项，字段随设备端下发；无标签为空数组）
	Tags         []string                        `json:"tags"`
}

// SnapshotFrame 建连第二帧 / resync 响应：整体覆盖本地容器状态。
type SnapshotFrame struct {
	Seq int64 `json:"seq"`
	// data.list 与 GET /android 同构；count 为容器总数
	Data struct {
		Count int            `json:"count"`
		List  []SnapshotItem `json:"list"`
	} `json:"data"`
}

// ContainerEventData container lifecycle 事件 data（单对象）。
type ContainerEventData struct {
	ContainerID string `json:"containerId"`
	Name        string `json:"name"`
	Status      string `json:"status"`
}

// ContainerStatsItem container/stats 事件 data 数组元素（每台云机的指标）。
type ContainerStatsItem struct {
	ContainerID string  `json:"containerId"`
	Name        string  `json:"name"`
	Status      string  `json:"status"`
	CPUUsage    float64 `json:"cpuUsage"`
	MemUsage    int64  `json:"memUsage"`
	DiskUsage   int64   `json:"diskUsage"`
	// Tags 容器标签（设备端 v208+ 随周期推送下发；老版本缺省为 nil，解析层不兜底——
	// 合并层以 nil 判断「本帧未带标签」，只有非 nil 才覆盖缓存条目）
	Tags        []string `json:"tags"`
}

// BootEventData boot 事件 data：只需 containerId 定位容器，状态由 action 映射。
type BootEventData struct {
	ContainerID string `json:"containerId"`
}

// ContainerStatsPatch 数值补丁（config 层落缓存用，避免反向依赖）。
type ContainerStatsPatch struct {
	ContainerID string
	CPUUsage    float64
	MemUsage    int64
	DiskUsage   int64
}

// EventFrame 增量事件。Data 为 json.RawMessage，按 Event/Action 二次解析。
type EventFrame struct {
	Seq      int64           `json:"seq"`
	Event    string          `json:"event"`
	Action   string          `json:"action"`
	TimeNano int64           `json:"timeNano"`
	Data     json.RawMessage `json:"data"`
}

// Frame 解析后的统一帧表示。
type Frame struct {
	Kind string // hello | snapshot | event | resync-required
	Hello    *HelloFrame
	Snapshot *SnapshotFrame
	Event    *EventFrame
}

// parseFrame 解析一帧 JSON 文本。未知 type 返回 (nil, nil)——前向兼容，静默忽略。
func parseFrame(data []byte) (*Frame, error) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("帧头解析失败: %w", err)
	}
	f := &Frame{Kind: head.Type}
	switch head.Type {
	case frameHello:
		f.Hello = &HelloFrame{}
		if err := json.Unmarshal(data, f.Hello); err != nil {
			return nil, fmt.Errorf("hello 帧解析失败: %w", err)
		}
	case frameSnapshot:
		f.Snapshot = &SnapshotFrame{}
		if err := json.Unmarshal(data, f.Snapshot); err != nil {
			return nil, fmt.Errorf("snapshot 帧解析失败: %w", err)
		}
	case frameEvent:
		f.Event = &EventFrame{}
		if err := json.Unmarshal(data, f.Event); err != nil {
			return nil, fmt.Errorf("event 帧解析失败: %w", err)
		}
	case frameResyncRequired:
		// 无附加字段
	default:
		return nil, nil // 未知帧类型：忽略（协议前向兼容）
	}
	return f, nil
}

// ParseContainerEventData 解析 container lifecycle 事件的 data（单对象）。
func ParseContainerEventData(raw json.RawMessage) (ContainerEventData, error) {
	var d ContainerEventData
	if err := json.Unmarshal(raw, &d); err != nil {
		return d, fmt.Errorf("container 事件 data 解析失败: %w", err)
	}
	return d, nil
}

// ParseContainerStatsData 解析 container/stats 事件的 data（数组）。
func ParseContainerStatsData(raw json.RawMessage) ([]ContainerStatsItem, error) {
	var d []ContainerStatsItem
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("container/stats 事件 data 解析失败: %w", err)
	}
	return d, nil
}

// ParseBootEventData 解析 boot 事件的 data。
func ParseBootEventData(raw json.RawMessage) (BootEventData, error) {
	var d BootEventData
	if err := json.Unmarshal(raw, &d); err != nil {
		return d, fmt.Errorf("boot 事件 data 解析失败: %w", err)
	}
	return d, nil
}

// ParseTagsChangedData 解析 system/tagsChanged 事件的 data。
// 载荷是变更后的本机标签名扁平全集（设备端跨容器去重、字典序，与 hello.tags 同构）；
// 老版本无此事件（消费层按 action 分发，自然兼容）。
func ParseTagsChangedData(raw json.RawMessage) ([]string, error) {
	var d struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("tagsChanged 事件 data 解析失败: %w", err)
	}
	return d.Tags, nil
}

// SystemDiskStats system/stats 磁盘帧（新 SDK，与 panel 帧互斥）。
// 三项均为字节（panel 帧的 mmcuse/mmctotal 是 MB——单位不同，勿混用）。
type SystemDiskStats struct {
	DiskTotal int64 `json:"diskTotal"`
	DiskUsed  int64 `json:"diskUsed"`
	DiskFree  int64 `json:"diskFree"`
}

// panel 帧键集（/info/device 动态字段；system/stats panel 帧的判别依据）。
var systemPanelKeys = map[string]bool{
	"cputemp": true, "cpuload": true, "memuse": true,
	"mmctotal": true, "mmcuse": true, "mmcread": true, "mmcwrite": true,
	"sysuptime": true, "mmctemp": true,
}

// disk 帧键集（任一出现即判为磁盘帧）。
var systemDiskKeys = map[string]bool{
	"diskTotal": true, "diskUsed": true, "diskFree": true,
}

// ParseSystemStatsFrame 判别 system/stats 的 data 形态：disk 帧（新 SDK）或
// panel 帧（/info/device 动态字段，老 SDK）。
//
//	含任一 disk* 键 → (nil, disk, nil)
//	含任一 panel 键  → (raw 原样, nil, nil)
//	两者都不含      → (raw 原样, nil, nil)——前向兼容未知帧，按 panel 走旧路径
func ParseSystemStatsFrame(raw json.RawMessage) (panel json.RawMessage, disk *SystemDiskStats, err error) {
	var keys map[string]json.RawMessage
	if err = json.Unmarshal(raw, &keys); err != nil {
		return nil, nil, fmt.Errorf("system/stats data 解析失败: %w", err)
	}
	isDisk := false
	for k := range keys {
		if systemDiskKeys[k] {
			isDisk = true
			break
		}
	}
	if isDisk {
		var d SystemDiskStats
		if err = json.Unmarshal(raw, &d); err != nil {
			return nil, nil, fmt.Errorf("system/stats 磁盘帧解析失败: %w", err)
		}
		return nil, &d, nil
	}
	// 非磁盘帧：面板帧（或未知帧）原样返回，由消费方按旧路径解析
	return raw, nil, nil
}

// seqTracker 实现 §6 seq 规则（每连接一份，非并发安全——消费循环串行调用）：
//
//	收到 hello    → 清空状态
//	收到 snapshot → lastSeq = seq
//	收到 event    → seq <= last        → 丢弃
//	               seq == last + 1    → 应用
//	               seq >  last + 1    → 应用 + 断档（发 resync）
type seqTracker struct {
	lastSeq  int64
	hasSeq   bool
}

func (t *seqTracker) onHello() {
	t.lastSeq, t.hasSeq = 0, false
}

func (t *seqTracker) onSnapshot(seq int64) {
	t.lastSeq, t.hasSeq = seq, true
}

// onEvent 返回 (apply 是否应用, gap 是否断档需 resync)。
func (t *seqTracker) onEvent(seq int64) (apply, gap bool) {
	if !t.hasSeq {
		// 未收到 snapshot 就来了事件（异常时序）：应用并以此为基准
		t.lastSeq, t.hasSeq = seq, true
		return true, false
	}
	switch {
	case seq <= t.lastSeq:
		return false, false // 重复，丢弃
	case seq == t.lastSeq+1:
		t.lastSeq = seq
		return true, false
	default:
		t.lastSeq = seq
		return true, true // 断档：应用 + resync
	}
}

// BootActionToStatus boot 事件 action → 容器 UI 状态映射（协议文档 §4 boot 表）。
// 未列出的 action 返回空串（调用方忽略）。
func BootActionToStatus(action string) string {
	switch action {
	case "queued", "booting":
		return "starting"
	case "completed", "timeout", "ready":
		return "running"
	case "aborted":
		return "stopped"
	default:
		return ""
	}
}

// ContainerActionToStatus container lifecycle action → 容器状态（协议文档 §4 container 表）。
// stats 返回空串（不改状态，由专用路径处理）；未列出的 action 返回空串（前向兼容）。
func ContainerActionToStatus(action string) string {
	switch action {
	case "create":
		return "created"
	case "start":
		return "running"
	case "restart":
		return "restarting"
	case "pause", "unpause":
		// pause/unpause：容器进程仍在（ADB 可用），UI 不离开运行侧——归 starting
		//（暂停中排队语义，恢复后经 start/die 事件收敛到 running/stopped）
		return "starting"
	case "stop", "die", "kill":
		// kill：容器被强杀，通常紧跟 die 事件收敛到最终态；此处保守映射 stopped
		return "stopped"
	case "destroy":
		return "removed"
	case "oom":
		return "running" // 状态不变，告警标记（本期只记日志）
	default:
		return ""
	}
}
