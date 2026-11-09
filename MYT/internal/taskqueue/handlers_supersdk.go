package taskqueue

import (
	"context"
	"fmt"
	"time"

	"github.com/moyunteng/myt/internal/v2host"
)

// 超级 SDK 一键安装任务（superSdkInstall，single handler）。
//
// v2 准入放宽后（仅 UDP+2375 可达即可添加），81 缺失的主机由用户点「安装超级
// SDK」触发安装。走任务队列而非 Wails 直接执行：镜像拉取分钟级，需要持久化/
// 恢复/任务面板进度（TaskQueueRow 直接读 dp.stage/dp.currentProgress）。
// 执行体复用 internal/v2host 的 EnsureSuperSdk（Go 版 initMytSdk：镜像检查/
// 拉取 → myt_sdk 容器创建/启动 → 81 回验），阶段回调映射为 deviceProgress。

// superSdkStagePct 阶段 → 进度百分比（单调递增，任务面板进度条用）
func superSdkStagePct(stage string) int {
	switch stage {
	case "check":
		return 5
	case "pull":
		return 15
	case "create":
		return 55
	case "recreate":
		// 端口段幂等重建：删除旧容器+创建+启动，耗时同初装，进度回退到创建段
		return 55
	case "start":
		return 70
	case "verify":
		return 90
	case "done":
		return 100
	default:
		return 50
	}
}

// superSdkStageLabel 阶段 → 中文标签（任务面板展示；pull 明确提示分钟级耗时）
func superSdkStageLabel(stage string) string {
	switch stage {
	case "check":
		return "检测超级 SDK 状态"
	case "pull":
		return "拉取镜像（可能需要几分钟）"
	case "create":
		return "创建超级 SDK 容器"
	case "recreate":
		return "重建超级 SDK 容器（补端口段）"
	case "start":
		return "启动超级 SDK 容器"
	case "verify":
		return "验证超级 SDK 服务"
	case "done":
		return "完成"
	default:
		return stage
	}
}

func init() {
	registerSingle("superSdkInstall", runSuperSdkInstall)
}

// runSuperSdkInstall 单设备阻塞长任务（与 runTeboxInstall 同型）：
// 置 running → EnsureSuperSdk（阶段回调映射 deviceProgress）→ 按结果记完成/失败。
// 整体兜底 15 分钟（镜像拉取分钟级 + 81 回验 60s，正常远不到上限），挂死强制
// 标失败不占队列。终态由 executor 统一判定（handler 只推进度与计数）。
func runSuperSdkInstall(ctx context.Context, e *Executor, t *Task) {
	t.setStatus(StatusRunning)
	e.emitProgress(t)
	target, deviceIP, ok := firstTarget(t)
	if !ok {
		e.emitProgress(t)
		return
	}
	_ = target // 单任务仅 deviceIp 一个字段（与 teboxInstall 同款 payload）

	client := v2host.NewClient()
	// cancel 与超时分支联动：EnsureSuperSdk 内部的 HTTP 请求均绑 ctx，超时/失败
	// 时取消，防止后台 goroutine 携带分钟级镜像拉取/60s 回验轮询脱管空跑。
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := client.EnsureSuperSdk(ctx, deviceIP, func(stage, message string) {
			e.updateDeviceProgress(t, deviceIP, DeviceProgress{
				Total:           1,
				Completed:       0,
				Stage:           superSdkStageLabel(stage),
				CurrentProgress: superSdkStagePct(stage),
			})
			e.emitProgress(t)
		})
		done <- err
	}()
	var err error
	select {
	case err = <-done:
	case <-ctx.Done():
		err = ctx.Err()
	case <-time.After(15 * time.Minute):
		// 超时中止：立刻掐断在途 HTTP 请求（EnsureSuperSdk 的镜像拉取/回验轮询
		// 均绑 ctx），goroutine 不脱管；defer cancel 对已取消 ctx 幂等无害。
		cancel()
		err = fmt.Errorf("超级 SDK 安装整体超时（15 分钟），已中止")
	}
	if err != nil {
		finishDevice(t, e, deviceIP, 1, false, "", err)
		return
	}
	finishDevice(t, e, deviceIP, 1, true, "", nil)
}
