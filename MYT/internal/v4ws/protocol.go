// Package v4ws 实现 V4（tobox_player）SDK 的 /ws/events 事件推送通道客户端。
//
// 协议文档：tobox_player/docs/websocket.md（proto:1）。
// 与 V3 的 internal/eventws 是两套互不兼容的协议（帧字段名、事件类型、seq 语义均不同），
// 因此独立成包、互不复用：本包不 import eventws，eventws 也不引用本包。
//
// 本文件：帧定义、解析、seq 断档检测、实例联合主键约定。纯逻辑，无 IO。
package v4ws

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/moyunteng/myt/internal/config"
)

// 帧类型（服务端 → 客户端四种，协议文档 §4）
const (
	frameHello          = "hello"
	frameSnapshot       = "snapshot"
	frameEvent          = "event"
	frameResyncRequired = "resync-required"
)

// 事件类型（协议文档 §5.1）
const (
	EventInstance  = "instance"
	EventClipboard = "clipboard"
	EventSystem    = "system"
)

// 事件动作（协议文档 §5.1）
const (
	ActionState  = "state"
	ActionPush   = "push"
	ActionResync = "resync"
)

// ProtoVersion 本客户端支持的协议版本（hello.proto）。不匹配视为不兼容（停止重连）。
const ProtoVersion = 1

// StateDeleted 事件专用状态：实例已从后端消失（不会出现在 snapshot 里，协议文档 §5.3）。
// 收到即从本地缓存移除该实例——它不是「关机」，是「不存在」。
const StateDeleted = "deleted"

// instanceKeySep 实例联合主键分隔符
const instanceKeySep = "/"

// InstanceKey 联合主键拼接：不同 backend 可以有同名 id（协议文档 §6），
// 只用 id 做缓存键会互相覆盖。backend 为空（事件缺字段的容错）时退化为裸 id。
func InstanceKey(backend, id string) string {
	if backend == "" {
		return id
	}
	return backend + instanceKeySep + id
}

// SplitInstanceKey 拆回 (backend, id)。无分隔符时 backend 为空（默认后端语义）。
func SplitInstanceKey(key string) (backend, id string) {
	if b, rest, ok := strings.Cut(key, instanceKeySep); ok {
		return b, rest
	}
	return "", key
}

// HelloFrame 建连第一帧：协议握手与版本校验。
type HelloFrame struct {
	Proto      int    `json:"proto"`
	SDKVersion string `json:"sdkVersion"`
	// ServerTime Unix 秒（V3 同名字段，单位不同）
	ServerTime int64 `json:"serverTime"`
}

// DeviceProfile 实例设备档案（协议文档 §4.2 的 profile 字段）。
// 单一事实源在 config.AndroidProfile（v4host 的 REST /android/configure 用同一契约），
// 此处仅为别名——snapshot 解析与 configure 请求共用一份结构，不做字段映射。
type DeviceProfile = config.AndroidProfile

// InstanceItem 实例信息（snapshot.data.instances 元素 / instance 事件 data 子集）。
//
// 注意字段命名：V4 是 snake_case（与 V3 的 camelCase 不同），不可混用。
// cast_port / control_port 是宿主机 loopback 内部端口，外部投屏走全局 9901/9905 + 握手行，
// 不能直接拿这两个端口连——此处仅落库备查。
type InstanceItem struct {
	// Backend 实例所属后端名（快照里每条必有；事件里也有，缺省按默认后端）
	Backend string `json:"backend"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Variant string `json:"variant"`
	// State stopped / starting / running / stopping / unknown / deleted（§5.3）
	State   string `json:"state"`
	Running bool   `json:"running"`
	PID     int32  `json:"pid"`

	ADBPort     int32 `json:"adb_port"`
	CastPort    int32 `json:"cast_port"`
	ControlPort int32 `json:"control_port"`

	CPUCores   int32 `json:"cpu_cores"`
	MemoryMB   int32 `json:"memory_mb"`
	DiskMB     int32 `json:"disk_mb"`
	DiskUsedMB int32 `json:"disk_used_mb"`

	SystemImage    string `json:"system_image"`
	VendorImage    string `json:"vendor_image"`
	InitramfsImage string `json:"initramfs_image"`

	Profile *DeviceProfile `json:"profile"`
}

// Key 实例联合主键（backend/id）
func (i InstanceItem) Key() string { return InstanceKey(i.Backend, i.ID) }

// SnapshotFrame 建连第二帧 / resync 响应：整体覆盖本地实例状态。
// Seq 是「快照基准」而非消费进度——之后只接受 seq > Seq 的事件（协议文档 §3）。
type SnapshotFrame struct {
	Seq  int64 `json:"seq"`
	Data struct {
		Instances []InstanceItem `json:"instances"`
		Total     int            `json:"total"`
	} `json:"data"`
}

// EventFrame 增量事件。Data 为 json.RawMessage，按 event/action 二次解析。
type EventFrame struct {
	Seq int64 `json:"seq"`
	// Event instance / clipboard / system
	Event string `json:"event"`
	// Action state / push / resync
	Action string `json:"action"`
	// TimeNano Unix 纳秒（V3 同名字段单位一致）
	TimeNano int64           `json:"timeNano"`
	Data     json.RawMessage `json:"data"`
}

// Frame 解析后的统一帧表示。
type Frame struct {
	Kind     string // hello | snapshot | event | resync-required
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
		// 无附加字段（协议文档 §4.4）
	default:
		return nil, nil // 未知帧类型：忽略（协议前向兼容）
	}
	return f, nil
}

// ParseInstanceStateData 解析 instance/state 事件的 data（单对象，§5.2）。
// data 是实例信息的子集：backend/id/name/state/running，其余字段为零值。
func ParseInstanceStateData(raw json.RawMessage) (InstanceItem, error) {
	var d InstanceItem
	if err := json.Unmarshal(raw, &d); err != nil {
		return d, fmt.Errorf("instance 事件 data 解析失败: %w", err)
	}
	return d, nil
}

// ClipboardEventData clipboard/push 事件 data（§5.2）。
type ClipboardEventData struct {
	Backend string `json:"backend"`
	ID      string `json:"id"`
	Text    string `json:"text"`
}

// ParseClipboardData 解析 clipboard/push 事件的 data。
func ParseClipboardData(raw json.RawMessage) (ClipboardEventData, error) {
	var d ClipboardEventData
	if err := json.Unmarshal(raw, &d); err != nil {
		return d, fmt.Errorf("clipboard 事件 data 解析失败: %w", err)
	}
	return d, nil
}

// seqTracker 实现协议文档 §7.2 的 seq 规则（每连接一份；消费 goroutine 串行调用，
// 非并发安全）：
//
//	hello            → 清空（新会话，旧 seq 跨进程无意义）
//	snapshot         → lastSeq = seq（快照基准，不是「已消费进度」）
//	event seq<=last  → 丢弃（窗口重放或重复，幂等无害）
//	event seq==last+1 → 应用
//	event seq>last+1  → 应用 + 断档（中间有事件丢失，需 resync）
type seqTracker struct {
	last   int64
	hasSeq bool
}

func (t *seqTracker) onHello() {
	t.last, t.hasSeq = 0, false
}

func (t *seqTracker) onSnapshot(seq int64) {
	t.last, t.hasSeq = seq, true
}

// onEvent 返回 (apply 是否应用, gap 是否断档需 resync)。
func (t *seqTracker) onEvent(seq int64) (apply, gap bool) {
	if !t.hasSeq {
		// 未收到 snapshot 就来了事件（异常时序）：应用并以此为基准
		t.last, t.hasSeq = seq, true
		return true, false
	}
	switch {
	case seq <= t.last:
		return false, false
	case seq == t.last+1:
		t.last = seq
		return true, false
	default:
		t.last = seq
		return true, true
	}
}
