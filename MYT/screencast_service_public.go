package main

import (
	"fmt"
	"net"
	"strings"

	"github.com/moyunteng/myt/internal/screencast"
)

// startPublicBatchProjections 公网模式批量投屏（RTSP 直连）。
//
// 与本地批量投屏完全同构：网格布局/首帧错开/标题/GPU 软解逻辑复用，
// 唯一差异是每台设备的连接参数来自网关 mobile API（video/control 端口组，
// 600s 过期，由 publicSessions 刷新器自动续期），而非本地 CalcProjectionParams。
// go_legacy 收到 401 Digest 挑战后自动用默认 u001/p001 应答。
func (s *ScreencastServiceWrapper) startPublicBatchProjections(items []BatchProjectionItem, scale float64, landscape bool, sidebar bool) error {
	if s.publicMgr == nil {
		return fmt.Errorf("公网投屏未初始化（缺少账号凭据）")
	}
	if scale < 0.3 {
		scale = 0.3
	}
	if scale > 1.0 {
		scale = 1.0
	}

	// 过滤出实际要启动的窗口（跳过已运行），预计算网格位置。
	// 端口获取/启动失败的设备不占位置，不影响其他窗口。
	var launches []screencast.SlotRef
	for _, item := range items {
		ref := screencast.SlotRef{DeviceIP: item.DeviceIP, IndexNum: item.IndexNum}
		if s.svc.IsRunning(ref) {
			continue
		}
		launches = append(launches, ref)
	}
	if len(launches) == 0 {
		return nil
	}

	// 少量窗口（<8）用 8 台网格尺寸作为标准大小，避免 1~3 台时窗口过大。
	// 单台留哨兵居中，多台按网格定位（与本地 batch.go 逻辑一致）。
	// 横屏投屏格子按 1280x720 算（与本地一致），否则横屏视频塞竖屏格子上下大黑边。
	noSidebar := s.cm.GetNoSidebar()
	sideW := 0
	if !noSidebar {
		sideW = screencast.SideCollapsedW
	}
	screencast.SetSideW(sideW)
	screencast.SetLandscape(landscape)
	arrange := len(launches) > 1

	// 单台横屏：视频原生 1:1 大窗（与本地 batch.go 一致）
	var positions []screencast.WindowPos
	if len(launches) == 1 && landscape {
		pos := screencast.ComputeSingleLandscapeLayout(sideW)
		positions = []screencast.WindowPos{pos}
		arrange = false
	} else {
		gridCount := len(launches)
		baseCount := singleDefaultCount
		if landscape {
			baseCount = landscapeSingleDefaultCount
		}
		if gridCount < baseCount {
			gridCount = baseCount
		}
		layout := screencast.ComputeGridLayout(gridCount, scale, 0, sideW, landscape)
		positions = layout.LayoutPositions()
	}
	posIdx := 0

	// 首帧就绪错开启动（与本地批量一致）：防多窗口并发 D3D11 创建打爆 GPU
	var notifyConn *net.UDPConn
	var notifyPort int
	if len(launches) > 1 {
		if conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}); err == nil {
			notifyConn = conn
			notifyPort = conn.LocalAddr().(*net.UDPAddr).Port
		}
	}
	defer func() {
		if notifyConn != nil {
			notifyConn.Close()
		}
	}()

	var errs []string
	for _, ref := range launches {
		// 拉网关 RTSP 端口组（失败重试 3 次，仍失败则单独报错）
		mp, err := s.publicMgr.GetMobilePorts(ref.DeviceIP, ref.IndexNum)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s:%d %v", ref.DeviceIP, ref.IndexNum, err))
			continue
		}

		pos := positions[posIdx]
		posIdx++
		androidImage := ""
		if a := s.findRunningAndroid(ref); a != nil {
			androidImage = a.Image
		}
		cfg := screencast.ProjectionConfig{
			RTSPParams: screencast.RTSPParams{
				DeviceIP:    mp.VideoHost,
				VideoPort:   mp.VideoPort,
				ControlPort: mp.ControlPort,
			},
			Title:        s.publicProjectionTitle(ref.DeviceIP, ref.IndexNum),
			WindowLayout: screencast.WindowLayout{Scale: scale},
			Controlled:   true,
			Landscape:    landscape,
			NoSidebar:    noSidebar,
			GpuAccel:     s.gpuAccelFor(androidImage),
			NotifyPort:   notifyPort,
		}
		// 统一传尺寸（WinW>0 → go_legacy 跳过 fit 撑大，避免与排列互相拉扯）
		cfg.WinW = pos.W
		cfg.WinH = pos.H
		// 多台传网格位置；单台留哨兵居中（不传 -winx/-winy）
		if arrange {
			cfg.WinX = pos.X
			cfg.WinY = pos.Y
		} else {
			cfg.WinX = screencast.WinPosUnset
			cfg.WinY = screencast.WinPosUnset
		}

		if err := s.svc.StartProjectionWithConfig(ref, cfg); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", ref, err))
			posIdx-- // 启动失败，回退位置索引
			continue
		}
		s.registerPublicSession(ref, cfg, mp.ExpiresIn, nil, false)

		// 等前一台首帧就绪（即 RTSP 握手+解码完成，15s 超时兜底）再启动下一个：
		// 避免 N 路 RTSP 连接同时冲击网关/容器端口 backlog（SYN 丢弃 → 3s RTO 重传）
		waitFirstFrameOrTimeout(notifyConn, ref.String())
	}

	if len(errs) > 0 {
		return fmt.Errorf("部分投屏失败: %s", strings.Join(errs, "; "))
	}
	// 布局一次定型：窗口启动时已定位到网格，无需后续重排。
	return nil
}
