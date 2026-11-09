// layout_constants.go — 投屏窗口尺寸统一标准（单一来源，Windows 平台）
//
// 所有尺寸/比例/边框常量集中于此。父仓库 MYT/internal/screencast/ports.go
// 有同名常量（跨 Go module 不能直接引用），两处值必须一致，修改时同步改。
//
// 设计原则：
//   - 视频源比例统一用 720:1280（横屏主流），删除旧 464:780
//   - NoSidebar 批量投屏仍保留 navBar（用户需要返回/主页/最近按钮）
//   - min/max 只在用户手动拖拽缩放时生效，排列/fit 定尺寸时不用（避免与排版冲突）

package main

// ─── 视频源比例 ───────────────────────────────────────────────
// 横屏源 1280x720 转竖后 eff=720x1280。排列用此比例匹配横屏主流，
// 竖屏源(480:800)的 ~6px 偏差由 letterbox 填满阈值（6px）处理。
const (
	videoAspectW = 720.0
	videoAspectH = 1280.0
	videoAspect  = videoAspectW / videoAspectH // 0.5625
)

// ─── 边框/栏（占空间）──────────────────────────────────────────
const (
	navBarHeightDefault int32 = 48 // 导航栏高（返回/主页/最近），NoSidebar 也保留
	sideCollapsedDefault int32 = 52
	sideExpandedDefault   int32 = 300
	sideWidthNoSide        int32 = 0 // NoSidebar 时侧边栏宽

	// scale 调整后栏的最小值（防止缩太小）。展开宽不缩放（WebView CSS 固定像素），
	// 无展开下限；折叠宽仍随窗口缩小（纯图标列，缩了不挤内容）。
	navBarMinHeight  int32 = 18
	sideCollapsedMin int32 = 18
)

// ─── 用户拖拽最小尺寸（仅手动拖边框 WM_SIZING 时生效，排列/fit 不用）─────────
// 单台投屏防止用户拖太小看不见；NoSidebar 批量投屏完全跳过，由排版定尺寸。
const (
	minSdlPortraitW  int32 = 270
	minSdlPortraitH  int32 = 480
	minSdlLandscapeW int32 = 480
	minSdlLandscapeH int32 = 270
)

// ─── 旋转转横的舒适下限 ─────────────────────────────────────────
// ToggleRotate 互换视频区保持面积，原竖窗偏小时转出的横窗过小看不清——
// 转横时视频区宽不足此值则等比放大到该宽度（转竖方向保持互换，自然收缩）。
const comfortLandscapeVideoW int32 = 720

// ─── 排列兜底（防 0/负）──────────────────────────────────────
const minBaseW = 40.0 // 排列客户区净宽兜底
