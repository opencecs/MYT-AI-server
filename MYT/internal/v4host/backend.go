package v4host

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// --- V4 后端（tebox 栈）列表与媒体端口推导 ---
//
// 一台 V4 主机上可以跑多套 tebox 栈（gRPC 50051 一套、50052 一套…），SDK 用
// 「后端名」区分（GET /backend/list）。每套栈各自监听**独立的全局媒体端口**，
// 播放器必须连到实例所属后端那一组端口，否则握手后立即被断开。
//
// 端口规律（2026-10 对 10.10.2.142 实测，见 CastPorts 注释）：
//
//	后端 gRPC 50051 → 视频 9901 / 音频 9904 / 控制 9905
//	后端 gRPC 50052 → 视频 9911 / 音频 9914 / 控制 9915
//
// 即媒体端口 = 默认端口 + (gRPC 端口 - 50051)，每套栈间隔 10。

// Backend 一个已注册的 tebox 后端（GET /backend/list 的 backends 元素）
type Backend struct {
	Name string `json:"name"`
	// Address tebox-api gRPC 地址（形如 127.0.0.1:50051），媒体端口由它推导
	Address string `json:"address"`
	// Status connected / unreachable
	Status        string `json:"status"`
	InstanceCount int    `json:"instanceCount"`
	Default       bool   `json:"default"`
}

// BackendListResult GET /backend/list 的 data 结构
type BackendListResult struct {
	Backends []Backend `json:"backends"`
}

// ListBackends 拉取后端列表（GET /backend/list）。聚合探测连通性，可能慢几秒。
func (c *InstanceClient) ListBackends(ctx context.Context) (*BackendListResult, error) {
	body, err := c.do(ctx, http.MethodGet, "/backend/list", nil, listTimeout)
	if err != nil {
		return nil, err
	}
	data, err := unwrapEnvelope(body)
	if err != nil {
		return nil, err
	}
	var res BackendListResult
	if len(data) > 0 {
		if err := json.Unmarshal(data, &res); err != nil {
			return nil, fmt.Errorf("/backend/list 响应解析失败: %w", err)
		}
	}
	if res.Backends == nil {
		res.Backends = []Backend{}
	}
	return &res, nil
}

// CastPorts 一组 tebox 全局媒体端口（播放器直连，与实例内部 cast_port 无关）。
type CastPorts struct {
	Video int // 视频 TCP（握手 TEBOX-CAST/1）
	Audio int // 音频 TCP（握手 TEBOX-AUDIO/1）
	Ctrl  int // 控制 TCP+UDP（UDP 无握手，事件自带 id/seq）
}

// 默认后端（gRPC 50051）的媒体端口，即上游 tebox 的全局端口。
const (
	DefaultCastVideoPort = 9901
	DefaultCastAudioPort = 9904
	DefaultCastCtrlPort  = 9905
	// defaultGRPCPort 端口偏移的基准：默认后端的 tebox-api gRPC 端口
	defaultGRPCPort = 50051
	// backendPortStep 相邻后端之间媒体端口的间隔（实测 50051→9901、50052→9911）
	backendPortStep = 10
)

// CastPortsForBackend 由后端 gRPC 地址推导该后端的全局媒体端口。
//
// 依据（2026-10 对 10.10.2.142 实测，非猜测）：
//   - GET /backend/list 给出 default=127.0.0.1:50051、coloros=127.0.0.1:50052；
//   - 端口扫描：9901/9904/9905 与 9911/9914/9915 均监听，9900/9902/9903/9921/9931 不监听；
//   - 握手验证：9901 只接受 default 后端的 origin-1，9911 只接受 coloros 后端的
//     coloros-1；拿错实例 id 或连错后端端口时服务端**立即断开**（实测 dt≈2–7ms），
//     正确时立即下发关键帧（codec=0 flags=1）。音频 9904/9914 同样按后端分区。
//
// 因此「媒体端口 = 默认端口 + (gRPC端口 - 50051) × 10」，即第 n 套栈（gRPC 50051+n）
// 用 9901+10n / 9904+10n / 9905+10n。
//
// 不确定度说明：实测只有两个数据点（50051→9901、50052→9911），"每套栈 +10" 的线性
// 关系是这两点外推得到的（第三套 50053 按此推 9921，本机未部署该栈、无法实测）。
// 若将来出现不符合该规律的部署，改成从设备侧配置读取或让用户在主机设置里手填即可，
// 只需替换本函数。address 缺失/非法时按默认后端处理（单后端部署的绝大多数情况）。
func CastPortsForBackend(address string) CastPorts {
	offset := 0
	if port, ok := grpcPortOf(address); ok && port > defaultGRPCPort {
		offset = (port - defaultGRPCPort) * backendPortStep
	}
	return CastPorts{
		Video: DefaultCastVideoPort + offset,
		Audio: DefaultCastAudioPort + offset,
		Ctrl:  DefaultCastCtrlPort + offset,
	}
}

// grpcPortOf 从 "127.0.0.1:50051" 解析端口（无端口/非法返回 false）
func grpcPortOf(address string) (int, bool) {
	address = strings.TrimSpace(address)
	i := strings.LastIndex(address, ":")
	if i < 0 || i == len(address)-1 {
		return 0, false
	}
	port, err := strconv.Atoi(address[i+1:])
	if err != nil || port <= 0 || port > 65535 {
		return 0, false
	}
	return port, true
}

// ResolveCastPorts 按后端名查全局媒体端口：命中 /backend/list 用其 address 推导，
// 未命中（未知后端 / 拉取失败）回落默认端口。
//
// 单后端部署下 /backend/list 与 /android 的 backend 字段一一对应；多后端时这是
// 唯一能区分「实例属于哪套栈」的信息源——不能靠实例 id 前缀猜。
func (c *InstanceClient) ResolveCastPorts(ctx context.Context, backend string) CastPorts {
	if backend == "" {
		return CastPortsForBackend("")
	}
	res, err := c.ListBackends(ctx)
	if err != nil {
		return CastPortsForBackend("")
	}
	for _, b := range res.Backends {
		if b.Name == backend {
			return CastPortsForBackend(b.Address)
		}
	}
	return CastPortsForBackend("")
}
