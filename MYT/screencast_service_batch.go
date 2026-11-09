package main

import (
	"fmt"
	"net"
	"strings"

	"github.com/moyunteng/myt/internal/screencast"
)

// singleDefaultCount 单台/少量投屏的基准网格数：用批量 8 台的网格尺寸作为标准大小，
// 避免 ComputeGridLayout(1~3,...) 算出接近全屏高度的过大窗口。
const singleDefaultCount = 8

// landscapeSingleDefaultCount 横屏单台/少量投屏的基准网格数：横屏格 16:9 偏矮胖
// （8 台格仅 542x342，侧栏展开后高不足 300，功能面板全挤在滚动条里），
// 少量横屏窗口用 2x2 格（≈881x515）作标准大小，与竖屏单台高度对齐。
const landscapeSingleDefaultCount = 4

// StartBatchIndividualProjections 批量逐台投屏（每台独立窗口，无主控/被控关系）
// 串行启动错开间隔避免 GPU 过载；启动时直接传网格位置，窗口一创建就在目标位置，
// 视频比例变化由 go_legacy 内部 letterbox 处理，布局层一次定型不重排。
func (s *ScreencastServiceWrapper) StartBatchIndividualProjections(items []BatchProjectionItem, scale float64, sidebar bool) error {
	if len(items) == 0 {
		return fmt.Errorf("未选择投屏设备")
	}
	// v2 前端传序列号，统一解析为真实 IP（running 索引/SlotRef 全链路以 IP 为键）
	items = s.normalizeItems(items)

	landscape := orientationToLandscape(items[0].Orientation)

	s.checkAccessibilityOnce()

	// 公网模式：通过 myt-agent 投屏
	if s.cm != nil && s.cm.GetDeployMode() == "public" {
		return s.startPublicBatchProjections(items, scale, landscape, sidebar)
	}

	// 批量投屏（>1 台）：开启前先优雅关闭所有旧窗口并等 GPU 资源释放完（防"关马上开"黑闪）。
	// 单台投屏不关旧窗口——用户单台点开新云机时不应关掉已有的投屏窗口。
	if len(items) > 1 {
		s.svc.StopAll()
	}

	// 一次性构建运行安卓索引，避免重复遍历
	runningIdx := s.buildRunningAndroidIndex()

	// 缩放因子校验（与前端滑动条一致 0.3~1.0）
	if scale < 0.3 {
		scale = 0.3
	}
	if scale > 1.0 {
		scale = 1.0
	}

	// 第一趟：过滤出实际要启动的窗口（跳过未开机/已运行），用于预计算网格位置。
	// 必须先过滤再算网格，否则跳过的窗口会让位置索引错位。
	var launches []screencast.SlotRef
	var errs []string
	for _, item := range items {
		ref := screencast.SlotRef{DeviceIP: item.DeviceIP, IndexNum: item.IndexNum}
		if _, ok := runningIdx[ref.String()]; !ok {
			if s.existsInAllList(ref) {
				errs = append(errs, fmt.Sprintf("%s:%d 未开机", item.DeviceIP, item.IndexNum))
			} else {
				errs = append(errs, fmt.Sprintf("%s:%d 未找到", item.DeviceIP, item.IndexNum))
			}
			continue
		}
		if s.svc.IsRunning(ref) {
			continue
		}
		launches = append(launches, ref)
	}
	if len(launches) == 0 {
		if len(errs) > 0 {
			return fmt.Errorf("部分投屏失败: %s", strings.Join(errs, "; "))
		}
		return nil
	}

	// 预计算网格：按实际启动数算出每个窗口的绝对位置。
	// 窗口一创建就在网格位置（传 -winx/-winy/-winw/-winh），避免堆叠后重排抖动。
	// 少量窗口（<8）用 8 台网格尺寸作为标准大小，避免 1~3 台时窗口占满全屏高度太大。
	// 单台位置留哨兵居中，多台按网格定位。
	// sideW：有侧边栏时 winw 必须含 SideCollapsedW，否则 sdlArea 比例偏移黑边。
	// SetSideW 同步给排列按钮用（doArrangePlayerWindows 读 GetSideW）。
	// SetLandscape 同理：排列按钮按本批投屏的横竖屏排格子（横屏=1280x720 格）。
	noSidebar := s.cm.GetNoSidebar()
	sideW := 0
	if !noSidebar {
		sideW = screencast.SideCollapsedW
	}
	screencast.SetSideW(sideW)
	screencast.SetLandscape(landscape)
	arrange := len(launches) > 1

	// 单台横屏：视频原生 1280x720 1:1 大窗（~1348x799，非网格）——
	// 用户期望单台吃满剩余屏宽 + 侧栏展开后高度足够不滚动。
	// 单台竖屏：传主程序预计算的 -winw/-winh（ComputeSinglePortraitLayout）。
	// 尺寸换算保证 SDL 区（客户区-侧栏-导航）比例恒等于视频比例（720:1280），
	// letterbox 零黑边；默认倍率 0.7 → 画面 448x798（高度对齐壳子画面 798，
	// 宽 449 比壳子 480 窄 31px——壳子画面比例 0.6015 与流比例 0.5625 不一致，
	// player 无裁剪模式，宽度无法既对齐又无黑边，用户拍板高度对齐即可）。
	// 之前「不传尺寸交 player 默认+fit」的方案已废弃：-scale 语义与滑条百分比
	// 错位（100% 才等于壳子大小），改为滑条直接映射画面倍率。
	// 批量分支不受影响，仅 len(launches)==1 生效。
	positions := make([]screencast.WindowPos, len(launches)) // 零值 W=0 → 不传尺寸
	if len(launches) == 1 && landscape {
		pos := screencast.ComputeSingleLandscapeLayout(sideW)
		singleScale := s.cm.GetSingleCastScale()
		pos.W = int(float64(pos.W) * singleScale)
		pos.H = int(float64(pos.H) * singleScale)
		positions[0] = pos
		arrange = false // 单台居中（哨兵）
	} else if len(launches) == 1 {
		// 竖屏单台：画面基准 686x1140（壳子画面比例 480:798）× 用户倍率 + chrome。
		// 不传 -fill：画面比例 0.6015 ≠ 流比例 0.5625，letterbox 左右各 ~15px 黑边。
		// 曾传 -fill 拉伸填满换取无黑边，但用户拖动窗口边缘改形状后拉伸越来越失真
		//（2026-10-28 用户拍板「画面别变形」优先），player 侧也已忽略该参数。
		positions[0] = screencast.ComputeSinglePortraitLayout(sideW, s.cm.GetSingleCastScale())
		arrange = false // 单台居中（哨兵）
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

	// 首帧就绪错开启动：批量（>1）时开 UDP 监听，等前一个窗口首帧就绪再启动下一个，
	// 避免多窗口瞬间并发创建 D3D11 设备/硬解器打爆 GPU（日志实证 12 路并发首帧 8.8s
	// + Exception 0xc0000005 访问违例崩溃）。单台无需错开。
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

	// 获取 ffmpeg.exe 路径（用于摄像头推流侧边栏功能）
	camExePath, _ := ensureFFmpegExe()

	for i, ref := range launches {
		a := runningIdx[ref.String()]
		cfg := screencast.ProjectionConfig{
			Controlled: true,
			WindowLayout: screencast.WindowLayout{
				Scale: scale,
			},
			Landscape:  landscape,
			NoSidebar:  noSidebar,              // 复用上面读的 s.cm.GetNoSidebar()
			GpuAccel:   s.gpuAccelFor(a.Image), // GPU 硬件加速（特质版/真机镜像强制软解）
			NotifyPort: notifyPort,
			CamPushExe: camExePath, // 摄像头推流 exe 路径
		}
		// -scale 保持传入 scale（批量=1.0 / 单台走定尺寸，player 内部 matrixScale 参考）。
		// 旧「竖屏单台 -scale 覆盖」已随定尺寸方案移除。
		// 统一传尺寸（WinW>0 → go_legacy 跳过 fit 撑大，避免与排列互相拉扯）；
		// 单台竖屏不传 -fill（任何窗口形状都 letterbox 不变形，见上方分支注释）。
		pos := positions[i]
		cfg.WinW = pos.W
		cfg.WinH = pos.H
		if arrange {
			// 批量投屏传网格位置；单台留哨兵居中（不传 -winx/-winy）
			cfg.WinX = pos.X
			cfg.WinY = pos.Y
		} else {
			cfg.WinX = screencast.WinPosUnset
			cfg.WinY = screencast.WinPosUnset
		}
		if err := s.svc.StartMatrixProjectionWindow(ref, a, cfg); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", ref, err))
		}
		// 等前一台「RTSP 握手完成（首帧解码成功）」再启动下一台：go_legacy 首帧解码后
		// 发 UDP 通知，收到立即继续。避免 N 路 RTSP 连接同时冲击容器端口 backlog
		// （约 1/3 SYN 被静默丢弃 → Windows 3s 初始 RTO 重传，首帧齐刷刷延迟 3s+）。
		// 15s 超时兜底：卡死窗口（如硬解不工作→回退软解）不死等阻塞整批。
		// GPU 初始化峰值错开是历史动机（12 路并发 D3D11 创建曾崩溃），本改动顺带保持。
		// 最后一台后面没有「下一台」，不用等（否则每次批量都多耗 15s 才返回）。
		if i < len(launches)-1 {
			waitFirstFrameOrTimeout(notifyConn, ref.String())
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("部分投屏失败: %s", strings.Join(errs, "; "))
	}

	// 布局一次定型：窗口启动时已定位到网格，无需后续重排。
	// 视频比例变化（首帧竖屏→实际横屏）由 go_legacy 内部 letterbox 处理，窗口位置/尺寸不变。
	// 手动滑块（ArrangePlayerWindows manual=true）仍可运行时重排已存在窗口。
	return nil
}
