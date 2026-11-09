package screenshot

import (
	"context"

	"github.com/moyunteng/myt/internal/config"
)

// ContainerTarget 截图轮询目标。
//
// 三种抓图通道共用这一个结构，靠字段区分（见 screenshot_fetch.go 的 fetchSingle）：
//   - DeviceIP 是 device_id（非 IP）→ 公网网关透传（publicSnap）
//   - InstanceID 非空 → 自定义抓图（targetFetcher，V4 走 tobox 8088 截图接口）
//   - 其余 → 本地直连容器 9082 /task=snap
type ContainerTarget struct {
	DeviceID     string
	DeviceIP     string
	IndexNum     int
	ContainerIP  string
	NetworkName  string
	PortBindings map[string][]config.PortBinding

	// 以下两项非空表示该目标走自定义抓图通道（V4：8088 GET /android/:id/screenshot），
	// 不走 9082。InstanceID/Backend 直接对应 tobox 的实例主键 (backend, id)。
	InstanceID string
	Backend    string
}

// TargetFetcher 自定义抓图函数：返回图片原始字节 + Content-Type。
// ctx 由截图服务传入（单次抓取的生命周期），实现方需自行再加超时上限。
type TargetFetcher func(ctx context.Context, target ContainerTarget) ([]byte, string, error)
