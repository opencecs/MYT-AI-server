package taskqueue

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// Executor 任务执行器：单任务内 5 路并发，context 取消
type Executor struct {
	queue *Queue
	sdk   SdkApiProvider
}

// SdkApiProvider SDK API 提供者接口（避免直接依赖 main 包）
type SdkApiProvider interface {
	RestartAndroid(deviceIP, name string) error
	StartAndroid(deviceIP, name string) error
	StopAndroid(deviceIP, name string) error
	DeleteAndroid(deviceIP, name string) error
	ResetAndroid(deviceIP, name string, start bool) error
	Shake(deviceIP, androidName string) error
	StopProxy(deviceIP, androidName string) error
	SdkUpgradeDevice(deviceIP string) (bool, error)
	// InstallSdkVersion 安装指定版本 SDK：客户端下载 zip → 上传设备 → 轮询确认版本变化
	InstallSdkVersion(deviceIP string, version int) error
	// InstallSdkVersionAny 按部署模式分流安装：v2 → InstallV2SdkVersion（string 版本），
	// 其他 → InstallSdkVersion（V3，string 版本 Atoi 转 int）。任务队列统一入口。
	InstallSdkVersionAny(deviceIP, version string) error
	// 维护类清理（单设备任务，返回 success/message，success=false 时记为失败）
	CleanLogs(deviceIP, androidName string, categories []string) (bool, string, error)
	CleanAndroidCache(deviceIP, androidName string, categories []string) (bool, string, error)
	PruneImages(deviceIP string) (int, string, error)
	// 带进度回调的清理（任务队列用，按 category 推进度）
	CleanLogsWithProgress(deviceIP, androidName string, categories []string, cb func(done, total int, stage string)) (bool, string, error)
	CleanAndroidCacheWithProgress(deviceIP, androidName string, categories []string, cb func(done, total int, stage string)) (bool, string, error)
	// iSCSI 无盘挂载/卸载（单设备长任务）
	MountIscsi(deviceIP, portal, iqn string, autoInit bool, localIP string) (bool, string, error)
	UmountIscsi(deviceIP, portal, iqn string, force bool) (bool, string, error)
	// IscsiDeviceState 返回设备 iSCSI 原始状态（有无 session / 是否已挂载），供挂载/卸载进度推断
	IscsiDeviceState(deviceIP string) (hasSession, isMounted bool)
	// ResetDevice 格式化设备磁盘（设备级清盘，设备随后自动重启）
	ResetDevice(deviceIP string) (bool, string, error)
}

// NewExecutor 创建执行器
func NewExecutor(q *Queue) *Executor {
	return &Executor{queue: q}
}

// SetSdkApi 注入 SDK API 提供者
func (e *Executor) SetSdkApi(sdk SdkApiProvider) {
	e.sdk = sdk
}

// Run 执行任务：5 路并发消费 targets，context 取消则停止
func (e *Executor) Run(ctx context.Context, t *Task) {
	defer e.queue.onTaskDone(t)

	if e.sdk == nil {
		t.setStatus(StatusFailed)
		t.Error = "SDK API 未注入"
		return
	}

	// 批量操作类：按 target 逐个执行，5 路并发
	if handler, ok := batchHandlers[t.Type]; ok {
		e.runBatch(ctx, t, handler)
		return
	}

	// 非批量类（下载/上传/复制等长任务）：单线程执行，由具体 handler 推进度
	if handler, ok := singleHandlers[t.Type]; ok {
		handler(ctx, e, t)
		// single handler 执行完，根据结果设置终态（handler 自己只推进度，不设状态）
		t.mu.Lock()
		canceled := ctx.Err() != nil
		failed := t.Failed
		completed := t.Completed
		if canceled {
			t.Status = StatusCanceled
		} else if failed > 0 || completed == 0 {
			t.Status = StatusFailed
			if failed == 0 {
				t.Error = "任务未完成"
			}
		} else {
			t.Status = StatusDone
		}
		if t.EndTime == nil {
			now := millisNow()
			t.EndTime = &now
		}
		t.mu.Unlock()
		return
	}

	t.setStatus(StatusFailed)
	t.Error = "未知任务类型: " + t.Type
}

// runBatch 批量任务：5 路并发执行
func (e *Executor) runBatch(ctx context.Context, t *Task, handler BatchHandler) {
	t.setStatus(StatusRunning)
	e.queue.emitTask(t.Snapshot()) // 立即推送 running 状态，让前端进度条进入进行中视觉
	total := len(t.Targets)
	if total == 0 {
		t.setStatus(StatusDone)
		return
	}

	taskCh := make(chan map[string]any, total)
	for _, target := range t.Targets {
		taskCh <- target
	}
	close(taskCh)

	workers := MaxTaskConcurrency
	if workers > total {
		workers = total
	}
	var wg sync.WaitGroup
	var stopOnce sync.Once

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for target := range taskCh {
				select {
				case <-ctx.Done():
					// 取消：停止接收新任务
					stopOnce.Do(func() {
						log.Printf("[taskqueue] 任务 %s 被取消", t.ID)
					})
					return
				default:
				}
				deviceIp, _ := target["deviceIp"].(string)
				if deviceIp == "" {
					deviceIp, _ = target["deviceIP"].(string)
				}
				name, _ := target["name"].(string)
				// 标记设备运行中（设备维度进度展示）
				e.updateDeviceProgress(t, deviceIp, DeviceProgress{Total: 1, Completed: 0, Stage: "running"})
				// 安装类任务加单台兜底超时：挂死设备标失败继续跑其他 target，
				// 不占住 worker/锁让任务永远「处理中」
				var err error
				if installLikeTasks[t.Type] {
					err = runWithTimeout(handler, e.sdk, target, installTaskTimeout)
				} else {
					err = handler(e.sdk, target)
				}
				if err != nil {
					t.addFailed(deviceIp, name, nil, err)
					e.updateDeviceProgress(t, deviceIp, DeviceProgress{Total: 1, Completed: 1, Failed: 1, Stage: "failed"})
				} else {
					t.addCompleted()
					e.updateDeviceProgress(t, deviceIp, DeviceProgress{Total: 1, Completed: 1, Stage: "done"})
				}
				e.queue.emitTask(t.Snapshot())
			}
		}()
	}
	wg.Wait()

	t.mu.Lock()
	failed := t.Failed
	canceled := ctx.Err() != nil
	if canceled {
		t.Status = StatusCanceled
		if t.EndTime == nil {
			now := millisNow()
			t.EndTime = &now
		}
	} else if failed > 0 {
		t.Status = StatusFailed
		t.Error = fmt.Sprintf("%d/%d 台失败", failed, t.Total)
	} else {
		t.Status = StatusDone
	}
	t.mu.Unlock()
	e.queue.emitTask(t.Snapshot())
}

// emitProgress 推送进度更新（供 singleHandler 调用）
func (e *Executor) emitProgress(t *Task) {
	e.queue.emitTask(t.Snapshot())
}

// updateDeviceProgress 更新设备维度进度
func (e *Executor) updateDeviceProgress(t *Task, ip string, dp DeviceProgress) {
	t.setDeviceProgress(ip, dp)
	e.queue.emitTask(t.Snapshot())
}

// installTaskTimeout SDK 安装类任务的单台兜底超时：handler 内部虽有整体
// 超时守护（InstallSdkVersion/InstallV2SdkVersion 10min），但 SSH/SFTP 挂死
// 可能绕过所有内层超时（goroutine 泄漏不返回）——worker 层再兜一道，超时把
// 该台标失败继续跑其他 target，不让一台挂死设备占住任务永远「处理中」。
const installTaskTimeout = 15 * time.Minute

// BatchHandler 批量任务单设备执行函数
type BatchHandler func(sdk SdkApiProvider, target map[string]any) error

// runWithTimeout 单台 target 执行守护：handler 超时后返回错误（handler
// goroutine 留在后台，由其内部超时机制最终退出），不阻塞 worker 循环。
func runWithTimeout(h BatchHandler, sdk SdkApiProvider, target map[string]any, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() {
		done <- h(sdk, target)
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("单台执行超时（%d 分钟），已跳过", int(timeout.Minutes()))
	}
}

// installLikeTasks 需要单台兜底超时的任务类型（安装类：内层有锁互斥，一台
// 挂死会占住锁挡住所有安装任务——worker 层强制标失败保住队列吞吐）
var installLikeTasks = map[string]bool{
	"sdkInstall":      true,
	"superSdkInstall": true,
}

// SingleHandler 长任务执行函数（自己推进度）
type SingleHandler func(ctx context.Context, e *Executor, t *Task)

var batchHandlers = map[string]BatchHandler{}
var singleHandlers = map[string]SingleHandler{}

func registerBatch(taskType string, h BatchHandler) {
	batchHandlers[taskType] = h
}

func registerSingle(taskType string, h SingleHandler) {
	singleHandlers[taskType] = h
}

// hasHandler 判断任务类型是否注册了后端 handler（batch 或 single）。
// 有则由后端执行，无则视为前端编排的长任务（仅存状态）。
func hasHandler(taskType string) bool {
	_, ok1 := batchHandlers[taskType]
	_, ok2 := singleHandlers[taskType]
	return ok1 || ok2
}
