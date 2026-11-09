package main

import (
	"fmt"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/screencast"
)

// StartMatrixProjection 启动矩阵投屏（群控 Tab 多台设备）
// 只启动主控投屏窗口（带 CtrlList 广播控制），不启动被控窗口。
// 主控操作时通过 CtrlList 广播控制指令到所有被控设备。
// scale: 窗口缩放因子（0.6~1.0，默认0.6）。
func (s *ScreencastServiceWrapper) StartMatrixProjection(items []BatchProjectionItem, scale float64) error {
	if len(items) == 0 {
		return fmt.Errorf("未选择投屏设备")
	}
	// v2 前端传序列号，统一解析为真实 IP（running 索引/SlotRef 全链路以 IP 为键）
	items = s.normalizeItems(items)

	landscape := orientationToLandscape(items[0].Orientation)

	// 公网模式：每台一个 WebURL 窗口，首台主控，输入经 UDP 扇出（详见 public_matrix）
	if s.cm != nil && s.cm.GetDeployMode() == "public" {
		return s.startPublicMatrixProjection(items, scale, landscape)
	}

	s.checkAccessibilityOnce()

	// 开启前先优雅关闭所有旧窗口并等 GPU 资源释放完（防“关马上开”黑闪）：
	// 上次关闭可能未完全释放（强杀/崩溃），残留进程/脏 GPU 状态会让新进程 D3D11CreateDevice 撞黑闪。
	s.svc.StopAll()

	// 一次性构建运行安卓索引，避免批量查询时重复遍历
	runningIdx := s.buildRunningAndroidIndex()

	// 查找所有选中的 running 安卓
	var androids []config.AndroidInfo
	for _, item := range items {
		ref := screencast.SlotRef{DeviceIP: item.DeviceIP, IndexNum: item.IndexNum}
		a, ok := runningIdx[ref.String()]
		if !ok {
			if s.existsInAllList(ref) {
				return fmt.Errorf("安卓 %s 坑位 %d 未开机，请先启动安卓", item.DeviceIP, item.IndexNum)
			}
			return fmt.Errorf("未找到设备 %s 坑位 %d 的安卓实例", item.DeviceIP, item.IndexNum)
		}
		androids = append(androids, a)
	}

	// 第一个作为主控，组装 CtrlList（包含所有被控，用于广播控制）
	primary := androids[0]
	deviceIPs := make([]string, len(items))
	for i, item := range items {
		deviceIPs[i] = item.DeviceIP
	}
	ctrlList := screencast.BuildCtrlList(androids, primary, deviceIPs)
	allCtrlList := screencast.BuildAllCtrlList(androids, deviceIPs)
	primaryAddr := screencast.CtrlAddr(primary, items[0].DeviceIP)

	// 缩放因子校验
	if scale < 0.3 {
		scale = 0.3
	}
	if scale > 1.0 {
		scale = 1.0
	}

	// 只启动主控窗口（带 CtrlList 广播控制，侧边栏可拖拽）
	primaryRef := screencast.SlotRef{DeviceIP: items[0].DeviceIP, IndexNum: primary.IndexNum}
	if err := s.svc.StartMatrixProjectionWindow(primaryRef, primary, screencast.ProjectionConfig{
		MatrixParams: screencast.MatrixParams{
			CtrlList:    ctrlList,
			AllCtrlList: allCtrlList,
			SelfAddr:    primaryAddr,
		},
		Controlled: true,
		WindowLayout: screencast.WindowLayout{
			Scale: scale,
		},
		Landscape: landscape,
		NoSidebar: s.cm.GetNoSidebar(),
		GpuAccel:  s.gpuAccelFor(primary.Image),
	}); err != nil {
		return err
	}

	return nil
}

// SwitchMatrixPrimary 更换矩阵投屏的主控设备
// newPrimary: 要升级为主控的设备；当前主控降级为被控
// scale: 窗口缩放因子（0.6~1.0）
func (s *ScreencastServiceWrapper) SwitchMatrixPrimary(items []BatchProjectionItem, newPrimaryDeviceIP string, newPrimaryIndexNum int, scale float64) error {
	if len(items) < 2 {
		return fmt.Errorf("至少需要 2 台设备才能切换主控")
	}
	// v2 前端传序列号，统一解析为真实 IP（与 Start 系入口同规）
	items = s.normalizeItems(items)
	newPrimaryDeviceIP = s.resolveSlotDeviceIP(newPrimaryDeviceIP)

	landscape := orientationToLandscape(items[0].Orientation)

	// 公网模式：停旧主控窗口 + 重开新主控 + 重建 fanout 会话
	if s.cm != nil && s.cm.GetDeployMode() == "public" {
		return s.switchPublicMatrixPrimary(items, newPrimaryDeviceIP, newPrimaryIndexNum, scale, landscape)
	}

	s.checkAccessibilityOnce()

	// 一次性构建运行安卓索引
	runningIdx := s.buildRunningAndroidIndex()

	// 查找所有选中的 running 安卓
	var androids []config.AndroidInfo
	for _, item := range items {
		ref := screencast.SlotRef{DeviceIP: item.DeviceIP, IndexNum: item.IndexNum}
		a, ok := runningIdx[ref.String()]
		if !ok {
			return fmt.Errorf("未找到设备 %s 坑位 %d 的安卓实例", item.DeviceIP, item.IndexNum)
		}
		androids = append(androids, a)
	}

	// 找到新的主控
	var newPrimary *config.AndroidInfo
	var newPrimaryItem *BatchProjectionItem
	for i, a := range androids {
		if a.IP == newPrimaryDeviceIP && a.IndexNum == newPrimaryIndexNum {
			newPrimary = &androids[i]
			newPrimaryItem = &items[i]
			break
		}
	}
	if newPrimary == nil {
		return fmt.Errorf("未找到新主控设备 %s 坑位 %d", newPrimaryDeviceIP, newPrimaryIndexNum)
	}

	// 先停掉所有矩阵投屏窗口
	for _, item := range items {
		ref := screencast.SlotRef{DeviceIP: item.DeviceIP, IndexNum: item.IndexNum}
		s.svc.StopProjection(ref)
	}

	// 等待进程退出
	time.Sleep(500 * time.Millisecond)

	// 用新主控重新启动所有窗口
	deviceIPs := make([]string, len(items))
	for i, item := range items {
		deviceIPs[i] = item.DeviceIP
	}
	ctrlList := screencast.BuildCtrlList(androids, *newPrimary, deviceIPs)
	allCtrlList := screencast.BuildAllCtrlList(androids, deviceIPs)
	primaryAddr := screencast.CtrlAddr(*newPrimary, newPrimaryItem.DeviceIP)

	// 新主控窗口
	newPrimaryRef := screencast.SlotRef{DeviceIP: newPrimaryItem.DeviceIP, IndexNum: newPrimary.IndexNum}
	if err := s.svc.StartMatrixProjectionWindow(newPrimaryRef, *newPrimary, screencast.ProjectionConfig{
		MatrixParams: screencast.MatrixParams{
			CtrlList:    ctrlList,
			AllCtrlList: allCtrlList,
			SelfAddr:    primaryAddr,
		},
		Controlled: true,
		WindowLayout: screencast.WindowLayout{
			Scale: scale,
		},
		Landscape: landscape,
		NoSidebar: s.cm.GetNoSidebar(),
		GpuAccel:  s.gpuAccelFor(newPrimary.Image),
	}); err != nil {
		return err
	}

	return nil
}
