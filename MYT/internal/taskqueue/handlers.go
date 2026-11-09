package taskqueue

import (
	"context"
	"fmt"
	"time"
)

func millisNow() int64 { return time.Now().UnixMilli() }

// 初始化注册所有任务类型 handler
func init() {
	// ── 批量操作类（5 路并发） ──
	registerBatch("restart", func(sdk SdkApiProvider, t map[string]any) error {
		return sdk.RestartAndroid(str(t, "deviceIp"), str(t, "name"))
	})
	registerBatch("start", func(sdk SdkApiProvider, t map[string]any) error {
		return sdk.StartAndroid(str(t, "deviceIp"), str(t, "name"))
	})
	registerBatch("shutdown", func(sdk SdkApiProvider, t map[string]any) error {
		return sdk.StopAndroid(str(t, "deviceIp"), str(t, "name"))
	})
	registerBatch("delete", func(sdk SdkApiProvider, t map[string]any) error {
		return sdk.DeleteAndroid(str(t, "deviceIp"), str(t, "name"))
	})
	registerBatch("reset", func(sdk SdkApiProvider, t map[string]any) error {
		// start 默认 true（兼容批量按钮不带该字段的提交）；右键重置弹窗按选项传 true/false
		return sdk.ResetAndroid(str(t, "deviceIp"), str(t, "name"), t["start"] == nil || t["start"] == true)
	})
	registerBatch("shake", func(sdk SdkApiProvider, t map[string]any) error {
		return sdk.Shake(str(t, "deviceIp"), str(t, "name"))
	})
	registerBatch("closeS5", func(sdk SdkApiProvider, t map[string]any) error {
		return sdk.StopProxy(str(t, "deviceIp"), str(t, "name"))
	})
	// SDK 升级：触发设备自升级
	registerBatch("sdkUpgrade", func(sdk SdkApiProvider, t map[string]any) error {
		upgraded, err := sdk.SdkUpgradeDevice(str(t, "deviceIp"))
		if err != nil {
			return err
		}
		if !upgraded {
			// 已是最新版本，不算失败
			return nil
		}
		return nil
	})

	// 维护类清理：单设备任务，按 category 真实推进度（single handler 自己控制）
	registerSingle("cleanLog", func(ctx context.Context, e *Executor, t *Task) {
		runCleanSingle(ctx, e, t, func(sdk SdkApiProvider, deviceIp, name string, categories []string, cb func(int, int, string)) (bool, string, error) {
			return sdk.CleanLogsWithProgress(deviceIp, name, categories, cb)
		})
	})
	registerSingle("cleanCache", func(ctx context.Context, e *Executor, t *Task) {
		runCleanSingle(ctx, e, t, func(sdk SdkApiProvider, deviceIp, name string, categories []string, cb func(int, int, string)) (bool, string, error) {
			return sdk.CleanAndroidCacheWithProgress(deviceIp, name, categories, cb)
		})
	})
	// 镜像清理：单次 PruneImages，无内置进度，走 batch（进行中→完成）
	registerBatch("cleanImage", func(sdk SdkApiProvider, t map[string]any) error {
		_, _, err := sdk.PruneImages(str(t, "deviceIp"))
		return err
	})
	// 格式化磁盘：设备级清盘，指令下发即完成（设备随后自动重启，约 2 分钟恢复）。
	// 与 sdkUpgrade 同型：不在任务里等待设备恢复——设备上下线由设备列表的 WS 状态体现，
	// 任务不占队列并发槽。
	registerBatch("formatDisk", func(sdk SdkApiProvider, t map[string]any) error {
		success, msg, err := sdk.ResetDevice(str(t, "deviceIp"))
		if err != nil {
			return err
		}
		if !success {
			return fmt.Errorf("%s", msg)
		}
		return nil
	})

	// TODO: switchModel/updateImage/cleanDisk/uninstallAgent/installVpn 等需要更多依赖
	// TODO: downloadImage/uploadImage/copy/importAndroid/exportAndroid/moveAndroid/uploadFile/create 长任务

	// iSCSI 无盘挂载/卸载（单设备长任务）
	registerSingle("iscsiMount", func(ctx context.Context, e *Executor, t *Task) {
		runIscsiSingle(ctx, e, t, func(sdk SdkApiProvider, target map[string]any) (bool, string, error) {
			return sdk.MountIscsi(str(target, "deviceIp"), str(target, "portal"), str(target, "iqn"), target["autoInit"] == true, str(target, "localIP"))
		}, []string{"登录", "分区格式化", "挂载", "迁移容器"}, mountStageProbe)
	})
	registerSingle("iscsiUmount", func(ctx context.Context, e *Executor, t *Task) {
		runIscsiSingle(ctx, e, t, func(sdk SdkApiProvider, target map[string]any) (bool, string, error) {
			return sdk.UmountIscsi(str(target, "deviceIp"), str(target, "portal"), str(target, "iqn"), target["force"] == true)
		}, []string{"迁移容器", "数据回传", "卸载文件系统", "登出"}, umountStageProbe)
	})
}

// iscsiStageProbe 在阻塞调用期间推断当前 iSCSI 阶段索引和进度百分比。
// stageIdx 对应 stages 数组下标；progress 为 0-100 的 currentProgress 值。
type iscsiStageProbe func(sdk SdkApiProvider, deviceIP string) (stageIdx int, progress int)

// mountStageProbe 挂载进度推断：根据 session/挂载状态返回阶段索引和进度
func mountStageProbe(sdk SdkApiProvider, deviceIP string) (int, int) {
	hasSession, isMounted := sdk.IscsiDeviceState(deviceIP)
	if isMounted {
		return 3, 85 // 已挂载 → docker 切换/迁移中
	}
	if hasSession {
		return 1, 40 // session 已建立 → 分区格式化/等设备
	}
	return 0, 15 // 登录中
}

// umountStageProbe 卸载进度推断
func umountStageProbe(sdk SdkApiProvider, deviceIP string) (int, int) {
	hasSession, isMounted := sdk.IscsiDeviceState(deviceIP)
	if isMounted {
		return 0, 20 // 迁移容器/数据回传中
	}
	if hasSession {
		return 2, 70 // 文件系统已卸载 → 登出中
	}
	return 3, 90 // 登出完成
}

// runIscsiSingle iSCSI 挂载/卸载通用 single handler
// fn 包装实际的挂载/卸载调用；stages 为阶段名称列表（用于进度展示）
// probe 在 fn 阻塞期间定期探测设备状态，推进 stage 和 currentProgress，避免 UI 卡在首个阶段
func runIscsiSingle(ctx context.Context, e *Executor, t *Task, fn func(sdk SdkApiProvider, target map[string]any) (bool, string, error), stages []string, probe iscsiStageProbe) {
	t.setStatus(StatusRunning)
	e.emitProgress(t)
	if len(t.Targets) == 0 {
		t.setStatus(StatusDone)
		e.emitProgress(t)
		return
	}
	target := t.Targets[0]
	deviceIp := str(target, "deviceIp")
	total := len(stages)
	e.updateDeviceProgress(t, deviceIp, DeviceProgress{Total: total, Completed: 0, Stage: stages[0], CurrentProgress: 10})
	e.emitProgress(t)

	// 检查取消
	select {
	case <-ctx.Done():
		t.setStatus(StatusCanceled)
		e.emitProgress(t)
		return
	default:
	}

	// 启动进度轮询 goroutine：fn 阻塞期间每 3s 探测设备状态推进 stage，
	// 解决大磁盘格式化/docker 重启等耗时操作期间 UI 卡在"登录"不动的问题
	probeStop := make(chan struct{})
	probeDone := make(chan struct{})
	go func() {
		defer close(probeDone)
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		lastStage := -1
		for {
			select {
			case <-probeStop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if probe == nil {
					return
				}
				stageIdx, progress := probe(e.sdk, deviceIp)
				if stageIdx > lastStage {
					lastStage = stageIdx
					stageName := stages[0]
					if stageIdx < len(stages) {
						stageName = stages[stageIdx]
					}
					e.updateDeviceProgress(t, deviceIp, DeviceProgress{
						Total: total, Completed: stageIdx, Stage: stageName, CurrentProgress: progress,
					})
					e.emitProgress(t)
				}
			}
		}
	}()

	// 执行挂载/卸载（整块操作，无法分阶段取消）
	success, msg, err := fn(e.sdk, target)
	close(probeStop)
	<-probeDone

	if ctx.Err() != nil {
		// 任务被取消
		e.updateDeviceProgress(t, deviceIp, DeviceProgress{Total: total, Completed: total, Stage: "canceled"})
		t.setStatus(StatusCanceled)
		e.emitProgress(t)
		return
	}

	if err != nil {
		t.addFailed(deviceIp, "", nil, err)
		e.updateDeviceProgress(t, deviceIp, DeviceProgress{Total: total, Completed: total, Failed: 1, Stage: "failed", Error: err.Error()})
	} else if !success {
		t.addFailed(deviceIp, "", nil, fmt.Errorf("%s", msg))
		e.updateDeviceProgress(t, deviceIp, DeviceProgress{Total: total, Completed: total, Failed: 1, Stage: "failed", Error: msg})
	} else {
		t.addCompleted()
		e.updateDeviceProgress(t, deviceIp, DeviceProgress{Total: total, Completed: total, Stage: "done"})
	}
	t.setProgress(100)
	e.emitProgress(t)
}

// runCleanSingle 清理类 single handler 通用逻辑：取单个 target，按 category 进度回调推进度。
// cleanFn 包装实际的清理调用（CleanLogsWithProgress / CleanAndroidCacheWithProgress）。
func runCleanSingle(ctx context.Context, e *Executor, t *Task, cleanFn func(sdk SdkApiProvider, deviceIp, name string, categories []string, cb func(int, int, string)) (bool, string, error)) {
	t.setStatus(StatusRunning)
	e.emitProgress(t)
	if len(t.Targets) == 0 {
		t.setStatus(StatusDone)
		e.emitProgress(t)
		return
	}
	target := t.Targets[0]
	deviceIp := str(target, "deviceIp")
	name := str(target, "name")
	categories := toStringSlice(target["categories"])
	total := len(categories)
	if total == 0 {
		total = 1
	}
	e.updateDeviceProgress(t, deviceIp, DeviceProgress{Total: total, Completed: 0, Stage: "running"})
	success, msg, err := cleanFn(e.sdk, deviceIp, name, categories, func(done, tot int, stage string) {
		select {
		case <-ctx.Done():
			return
		default:
		}
		t.setProgress(done * 100 / tot)
		e.updateDeviceProgress(t, deviceIp, DeviceProgress{Total: tot, Completed: done, Stage: stage})
		e.emitProgress(t)
	})
	if err != nil {
		t.addFailed(deviceIp, name, nil, err)
		e.updateDeviceProgress(t, deviceIp, DeviceProgress{Total: total, Completed: total, Failed: 1, Stage: "failed"})
	} else if !success {
		t.addFailed(deviceIp, name, nil, fmt.Errorf("%s", msg))
		e.updateDeviceProgress(t, deviceIp, DeviceProgress{Total: total, Completed: total, Failed: 1, Stage: "failed"})
	} else {
		t.addCompleted()
		e.updateDeviceProgress(t, deviceIp, DeviceProgress{Total: total, Completed: total, Stage: "done"})
	}
	t.setProgress(100)
	e.emitProgress(t)
}

// str 从 map 安全取字符串
func str(t map[string]any, key string) string {
	v, _ := t[key].(string)
	if v == "" {
		// 兼容 deviceIP 大写
		if key == "deviceIp" {
			v, _ = t["deviceIP"].(string)
		}
	}
	return v
}

// toStringSlice 从 map 取字符串切片（清理任务 categories 用）
func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
