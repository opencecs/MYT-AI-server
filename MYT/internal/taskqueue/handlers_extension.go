package taskqueue

import (
	"context"
	"fmt"
	"time"

	"github.com/moyunteng/myt/internal/maintenance"
)

// unlockFn tebox 安装锁释放钩子（main 包注入 UnlockTeboxInstall，setup 时
// SetTeboxInstallUnlock 设置）。taskqueue 不能反向依赖 main，nil 时跳过释放
// （锁泄漏风险由客户端重启兜底——重启后锁内存态消失）。
var unlockFn func(ip string)

// SetTeboxInstallUnlock 注入 tebox 安装锁释放函数（setup 调用）。
func SetTeboxInstallUnlock(fn func(ip string)) { unlockFn = fn }

// resultOf 把 maintenance 包统一的 map 返回（success/message）转成 (bool, string, error)

// 扩展服务（魔云互联 / 公网穿透）的装、卸任务。
//
// 与 cleanLog / iscsiMount 走 SdkApiProvider 不同：这几个操作自带设备 SSH 凭据与 HTTP 下载，
// 不需要「带密码的 SDK 客户端」，所以直接调用 maintenance 包（该包不依赖 taskqueue，无循环）。
// 用 single handler 而非 batch：单设备长任务，需要分阶段推进度；公网穿透还要把 frps 分配到的
// 公网地址写回任务详情（Details 会被 flattenTask 平铺到顶层）供前端信息卡展示。
func init() {
	registerSingle("installMytPanel", func(_ context.Context, e *Executor, t *Task) {
		runMaintenanceSingle(e, t, "installing", func(deviceIP string) (bool, string, error) {
			return resultOf(maintenance.InstallMytPanel(deviceIP))
		})
	})
	registerSingle("uninstallMytPanel", func(_ context.Context, e *Executor, t *Task) {
		runMaintenanceSingle(e, t, "卸载中", func(deviceIP string) (bool, string, error) {
			return resultOf(maintenance.UninstallMytPanel(deviceIP))
		})
	})
	registerSingle("installTunnel", func(_ context.Context, e *Executor, t *Task) {
		runTunnelInstall(e, t)
	})
	registerSingle("uninstallTunnel", func(_ context.Context, e *Executor, t *Task) {
		runTunnelUninstall(e, t)
	})
	registerSingle("teboxInstall", func(_ context.Context, e *Executor, t *Task) {
		runTeboxInstall(e, t)
	})
}

// runTeboxInstall tebox_player SDK 安装（单设备阻塞长任务）：
// 取包（newapi tebox_sdk）→ load 基座镜像 → SFTP 上传脚本+zip → 执行
// install-sdk.sh → 执行 start-tebox-hw.sh 启动基座容器 → /health + 50051 验证。
// 进度经 deviceProgress.stage 流式推送（任务面板显示当前阶段/脚本输出行）。
// 整体兜底 20 分钟（内层：镜像拉取 10min + load 5min + 安装脚本 10min +
// 基座启动 2min + verify 轮询 2min，正常不叠加到上限），挂死强制标失败不占队列。
func runTeboxInstall(e *Executor, t *Task) {
	t.setStatus(StatusRunning)
	e.emitProgress(t)
	target, deviceIP, ok := firstTarget(t)
	if !ok {
		e.emitProgress(t)
		return
	}
	// 安装锁释放（提交侧 tryLockTeboxInstall 获取——经 hook 注入避免反向依赖；
	// 无论成功失败终态都必须释放）
	if unlockFn != nil {
		ip := str(target, "deviceIp")
		defer unlockFn(ip)
	}

	e.updateDeviceProgress(t, deviceIP, DeviceProgress{Total: 1, Completed: 0, Stage: "准备", CurrentProgress: 5})
	done := make(chan error, 1)
	go func() {
		done <- maintenance.InstallTeboxSdkFromCloud(deviceIP, func(stage, message string) {
			// 脚本 [N/8] 输出映射为进度：阶段+当前输出行（截断防长行刷屏）
			msg := message
			if len(msg) > 120 {
				msg = msg[:120]
			}
			pct := teboxStagePct(stage)
			e.updateDeviceProgress(t, deviceIP, DeviceProgress{Total: 1, Completed: 0, Stage: teboxStageLabel(stage), CurrentProgress: pct, Error: msg})
			e.emitProgress(t)
		})
	}()
	var err error
	select {
	case err = <-done:
	case <-time.After(20 * time.Minute):
		err = fmt.Errorf("tebox SDK 安装整体超时（20 分钟），已中止")
	}
	if err != nil {
		finishDevice(t, e, deviceIP, 1, false, "", err)
		return
	}
	finishDevice(t, e, deviceIP, 1, true, "", nil)
}

// teboxStagePct 安装阶段 → 进度百分比（单调递增，任务面板进度条用）
func teboxStagePct(stage string) int {
	switch stage {
	case "prepare":
		return 5
	case "pull":
		return 12
	case "load":
		return 18
	case "upload":
		return 40
	case "install":
		return 65
	case "base":
		return 80
	case "verify":
		return 95
	default:
		return 50
	}
}

// teboxStageLabel 安装阶段 → 中文标签（任务面板展示）
func teboxStageLabel(stage string) string {
	switch stage {
	case "prepare":
		return "获取安装包"
	case "pull":
		return "拉取云机镜像"
	case "load":
		return "加载基座镜像"
	case "upload":
		return "上传到设备"
	case "install":
		return "执行安装"
	case "base":
		return "启动基座容器"
	case "verify":
		return "验证服务"
	default:
		return stage
	}
}

// resultOf 把 maintenance 包统一的 map 返回（success/message）转成 (bool, string, error)
func resultOf(r map[string]interface{}) (bool, string, error) {
	success, _ := r["success"].(bool)
	message, _ := r["message"].(string)
	return success, message, nil
}

// intOf 从任务 payload 取整数（JSON 反序列化后数值是 float64）
func intOf(target map[string]any, key string) int {
	switch v := target[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

// firstTarget 取单设备任务的唯一 target 与设备 IP；无 target 时返回 false（调用方直接结束任务）
func firstTarget(t *Task) (map[string]any, string, bool) {
	if len(t.Targets) == 0 {
		t.setStatus(StatusDone)
		return nil, "", false
	}
	target := t.Targets[0]
	return target, str(target, "deviceIp"), true
}

// runMaintenanceSingle 单设备阻塞型维护操作：置 running → 调 fn → 按结果记完成/失败。
// 终态由 executor 统一判定（与 runCleanSingle 同型），这里只推进度与计数。
func runMaintenanceSingle(e *Executor, t *Task, stage string, fn func(deviceIP string) (bool, string, error)) {
	t.setStatus(StatusRunning)
	e.emitProgress(t)
	if _, deviceIP, ok := firstTarget(t); ok {
		e.updateDeviceProgress(t, deviceIP, DeviceProgress{Total: 1, Completed: 0, Stage: stage})
		success, msg, err := fn(deviceIP)
		finishDevice(t, e, deviceIP, 1, success, msg, err)
	}
	e.emitProgress(t)
}

// runTunnelInstall 公网穿透安装：先在用户服务器部署 frps，再在设备安装 frpc，
// 最后把 frps 实际分配到的公网地址写回任务详情（前端直接读 task.webAddress / task.remoteAddress）。
func runTunnelInstall(e *Executor, t *Task) {
	t.setStatus(StatusRunning)
	e.emitProgress(t)
	target, deviceIP, ok := firstTarget(t)
	if !ok {
		e.emitProgress(t)
		return
	}
	serverAddr := str(target, "serverAddr")
	bindPort := intOf(target, "bindPort")

	// 第一步：服务器端 frps
	e.updateDeviceProgress(t, deviceIP, DeviceProgress{Total: 2, Completed: 0, Stage: "部署服务端"})
	success, msg, err := resultOf(maintenance.InstallTunnelServer(
		serverAddr, str(target, "serverSSHUser"), str(target, "serverSSHPassword"),
		intOf(target, "serverSSHPort"), bindPort, 7500, "admin", "admin",
	))
	if err != nil || !success {
		finishDevice(t, e, deviceIP, 2, false, msg, err)
		return
	}

	// 第二步：设备端 frpc
	e.updateDeviceProgress(t, deviceIP, DeviceProgress{Total: 2, Completed: 1, Stage: "安装客户端"})
	r := maintenance.InstallTunnel(deviceIP, serverAddr, bindPort, "")
	ok2, msg2, err2 := resultOf(r)
	if err2 != nil || !ok2 {
		finishDevice(t, e, deviceIP, 2, false, msg2, err2)
		return
	}

	t.setDetail("serverAddr", serverAddr)
	if v, _ := r["remoteAddress"].(string); v != "" {
		t.setDetail("remoteAddress", v)
	}
	if v, _ := r["webAddress"].(string); v != "" {
		t.setDetail("webAddress", v)
	}
	finishDevice(t, e, deviceIP, 2, true, "", nil)
}

// runTunnelUninstall 公网穿透卸载：先卸设备端 frpc，再卸服务器端 frps（有服务器凭据时才做）。
func runTunnelUninstall(e *Executor, t *Task) {
	t.setStatus(StatusRunning)
	e.emitProgress(t)
	target, deviceIP, ok := firstTarget(t)
	if !ok {
		e.emitProgress(t)
		return
	}
	serverAddr := str(target, "serverAddr")
	serverPwd := str(target, "serverSSHPassword")
	total := 1
	if serverAddr != "" && serverPwd != "" {
		total = 2
	}

	// 第一步：设备端 frpc
	e.updateDeviceProgress(t, deviceIP, DeviceProgress{Total: total, Completed: 0, Stage: "卸载客户端"})
	ok1, msg1, err1 := resultOf(maintenance.UninstallTunnel(deviceIP))
	if err1 != nil || !ok1 {
		finishDevice(t, e, deviceIP, total, false, msg1, err1)
		return
	}

	// 第二步：服务器端 frps（可选）
	if total == 2 {
		e.updateDeviceProgress(t, deviceIP, DeviceProgress{Total: total, Completed: 1, Stage: "卸载服务端"})
		ok2, msg2, err2 := resultOf(maintenance.UninstallTunnelServer(
			serverAddr, str(target, "serverSSHUser"), serverPwd, intOf(target, "serverSSHPort"),
		))
		if err2 != nil || !ok2 {
			finishDevice(t, e, deviceIP, total, false, msg2, err2)
			return
		}
	}
	finishDevice(t, e, deviceIP, total, true, "", nil)
}

// finishDevice 按结果更新设备进度与计数；err 为 nil 且 success=false 时用 msg 作错误文案。
func finishDevice(t *Task, e *Executor, deviceIP string, total int, success bool, msg string, err error) {
	if err == nil && !success {
		err = fmt.Errorf("%s", msg)
	}
	if success {
		t.addCompleted()
		e.updateDeviceProgress(t, deviceIP, DeviceProgress{Total: total, Completed: total, Stage: "done"})
	} else {
		t.addFailed(deviceIP, "", nil, err)
		e.updateDeviceProgress(t, deviceIP, DeviceProgress{Total: total, Completed: total, Failed: 1, Stage: "failed", Error: err.Error()})
	}
	t.setProgress(100)
	e.emitProgress(t)
}
