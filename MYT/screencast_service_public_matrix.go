package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/screencast"
)

// 公网矩阵投屏（RTSP 直连，对齐本地矩阵形态）：
//
// 只启动主控一个窗口，被控不单独开窗（与本地矩阵一致）。
// 主控 -list 目标 = 各被控的网关控制端口（119.96.73.226:<control_port>，# 分隔），
// player ControlChannel.SetTargetList 把目标加入 UDP targets，Send() 的消息
// （鼠标/键盘/导航键 CmdAndroidKey）自动 WriteTo 扇出到全部目标——
// 因此输入与导航按钮广播都不需要 JS 注入/grouprpa/fanout（WebURL v5 那套整体废弃）。
//
// 端口 600s 过期由 publicSessions 刷新器统一处理：
// 到期前重拉全部（主控+被控）端口并重启主控窗口（被控无窗口，无需重启）。
func (s *ScreencastServiceWrapper) startPublicMatrixProjection(items []BatchProjectionItem, scale float64, landscape bool) error {
	if s.publicMgr == nil {
		return fmt.Errorf("公网投屏未初始化（缺少账号凭据）")
	}
	if len(items) == 0 {
		return fmt.Errorf("未选择投屏设备")
	}
	if scale < 0.3 {
		scale = 0.3
	}
	if scale > 1.0 {
		scale = 1.0
	}

	// 先停掉旧矩阵主控（如有），避免双主控窗口
	s.stopPublicMatrixPrimary()

	primaryItem := items[0]
	primaryRef := screencast.SlotRef{DeviceIP: primaryItem.DeviceIP, IndexNum: primaryItem.IndexNum}

	rtspParams, matrixParams, expiresIn, errs := s.fetchPublicMatrixPorts(items)
	if rtspParams.VideoPort == 0 {
		return fmt.Errorf("主控设备 %s 坑位 %d 获取投屏端口失败: %v", primaryItem.DeviceIP, primaryItem.IndexNum, errs)
	}

	androidImage := ""
	if a := s.findRunningAndroid(primaryRef); a != nil {
		androidImage = a.Image
	}

	cfg := screencast.ProjectionConfig{
		RTSPParams: rtspParams,
		MatrixParams: screencast.MatrixParams{
			CtrlList:    matrixParams.CtrlList,
			AllCtrlList: matrixParams.AllCtrlList,
			SelfAddr:    matrixParams.SelfAddr,
		},
		Title:        "[主控] " + s.publicProjectionTitle(primaryItem.DeviceIP, primaryItem.IndexNum),
		Controlled:   true,
		WindowLayout: screencast.WindowLayout{Scale: scale},
		Landscape:    landscape,
		NoSidebar:    s.cm.GetNoSidebar(),
		GpuAccel:     s.gpuAccelFor(androidImage),
	}
	if err := s.svc.StartProjectionWithConfig(primaryRef, cfg); err != nil {
		return fmt.Errorf("主控设备 %s 坑位 %d 启动投屏失败: %w", primaryItem.DeviceIP, primaryItem.IndexNum, err)
	}

	s.registerPublicSession(primaryRef, cfg, expiresIn, items, true)
	if len(errs) > 0 {
		// 被控端口获取失败只影响该被控的扇出，主控与其他被控仍可用
		log.Printf("[screencast] public matrix: 部分被控端口获取失败（扇出目标跳过）: %s", strings.Join(errs, "; "))
	}
	log.Printf("[screencast] public matrix started (RTSP): primary=%s controlled=%d", primaryRef.String(), countCtrlTargets(cfg.CtrlList))
	return nil
}

// fetchPublicMatrixPorts 拉取主控+全部被控的网关端口组。
// 返回：主控 RTSP 参数、主控扇出 MatrixParams（CtrlList=被控列表/AllCtrlList=含主控/SelfAddr=主控自身）、
// 过期秒数、被控获取失败信息。
func (s *ScreencastServiceWrapper) fetchPublicMatrixPorts(items []BatchProjectionItem) (screencast.RTSPParams, screencast.MatrixParams, int, []string) {
	var rtspParams screencast.RTSPParams
	var matrixParams screencast.MatrixParams
	var expiresIn int
	var errs []string

	// 主控
	pm, err := s.publicMgr.GetMobilePorts(items[0].DeviceIP, items[0].IndexNum)
	if err != nil {
		return rtspParams, matrixParams, 0, []string{err.Error()}
	}
	rtspParams = screencast.RTSPParams{
		DeviceIP:    pm.VideoHost,
		VideoPort:   pm.VideoPort,
		ControlPort: pm.ControlPort,
	}
	expiresIn = pm.ExpiresIn
	selfAddr := ctrlAddrOf(pm)
	allAddrs := []string{selfAddr}

	// 被控（只为拿 control_port 做扇出目标；其 video 端口闲置）
	var ctrlAddrs []string
	for i := 1; i < len(items); i++ {
		cm, err := s.publicMgr.GetMobilePorts(items[i].DeviceIP, items[i].IndexNum)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s:%d %v", items[i].DeviceIP, items[i].IndexNum, err))
			continue
		}
		addr := ctrlAddrOf(cm)
		ctrlAddrs = append(ctrlAddrs, addr)
		allAddrs = append(allAddrs, addr)
		if cm.ExpiresIn < expiresIn || expiresIn == 0 {
			expiresIn = cm.ExpiresIn
		}
	}

	matrixParams = screencast.MatrixParams{
		CtrlList:    strings.Join(ctrlAddrs, "#"),
		AllCtrlList: strings.Join(allAddrs, "#"),
		SelfAddr:    selfAddr,
	}
	return rtspParams, matrixParams, expiresIn, errs
}

// ctrlAddrOf 组装网关控制通道地址 ip:port（UDP）
func ctrlAddrOf(mp *screencast.MobilePorts) string {
	host := mp.ControlHost
	if host == "" {
		host = mp.VideoHost
	}
	return fmt.Sprintf("%s:%d", host, mp.ControlPort)
}

// countCtrlTargets 统计 -list 扇出目标数量
func countCtrlTargets(list string) int {
	if list == "" {
		return 0
	}
	return len(strings.Split(list, "#"))
}

// switchPublicMatrixPrimary 切换公网矩阵主控：
// 停旧主控 → items 重排（新主控置首）→ 重新拉全部端口 → 开新主控窗口。
func (s *ScreencastServiceWrapper) switchPublicMatrixPrimary(items []BatchProjectionItem, newPrimaryDeviceIP string, newPrimaryIndexNum int, scale float64, landscape bool) error {
	if len(items) < 2 {
		return fmt.Errorf("至少需要 2 台设备才能切换主控")
	}
	newIdx := -1
	for i, item := range items {
		if item.DeviceIP == newPrimaryDeviceIP && item.IndexNum == newPrimaryIndexNum {
			newIdx = i
			break
		}
	}
	if newIdx < 0 {
		return fmt.Errorf("未找到新主控设备 %s 坑位 %d", newPrimaryDeviceIP, newPrimaryIndexNum)
	}
	reordered := make([]BatchProjectionItem, 0, len(items))
	reordered = append(reordered, items[newIdx])
	for i, item := range items {
		if i != newIdx {
			reordered = append(reordered, item)
		}
	}
	return s.startPublicMatrixProjection(reordered, scale, landscape)
}

// publicProjectionTitle 公网投屏窗口标题（备注优先，其次安卓名短名，兜底 deviceID_index；
// 追加设备标识后缀「 (deviceID)」——公网 android.IP 存的就是 device_id）。
func (s *ScreencastServiceWrapper) publicProjectionTitle(deviceID string, indexNum int) string {
	ref := screencast.SlotRef{DeviceIP: deviceID, IndexNum: indexNum}
	if android := s.findRunningAndroid(ref); android != nil {
		base := config.ContainerDisplayName(android.Name, android.DeviceID, android.IndexNum)
		if note := s.cm.GetAndroidNote(android.ID); note != "" {
			base = note
		}
		return screencast.WithDeviceIdentifier(base, android.IP)
	}
	return screencast.WithDeviceIdentifier(
		fmt.Sprintf("%s_%d", deviceID[:min(8, len(deviceID))], indexNum), deviceID)
}
