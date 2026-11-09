package main

import "sync/atomic"

// tebox_orientation.go — tebox 投屏模式的画面方向自适应。
//
// 背景（上游机制）：tebox 横屏由 guest 内部旋转触发——Android UI 转横 →
// QEMU GPU 预旋转 dma-buf → tebox-cast 推流分辨率直接变横屏（如 1600x720），
// 线协议不变。客户端唯一可感知的信号就是解码分辨率变化（无旋转元数据）。
// 这与 V3 RTSP 流的横屏形态一致（1280x720 侧躺存储，Rotated=true 走
// D3D11 顺时针旋转管线转正 + 鼠标坐标转置映射）。
//
// 方向策略：tebox 启动时 Rotated=false（竖屏流 720x1600 大概率先到），
// 首帧/分辨率变化时按宽高比更新：
//   - 宽 > 高（横屏流）→ Rotated=true（走 V3 横屏渲染+触摸链路）
//   - 宽 < 高（竖屏流）→ Rotated=false
//   - 宽 == 高（正方形，理论上不出现）→ 保持当前状态（避免来回抖动）
//
// 翻转时机：钩子挂在 decodeLoop 出帧路径（app_connect_cb.go），在任何
// SendFrame 之前调 ApplyTeboxOrientation——Rotated 翻转先于该帧进入渲染
// 管线，帧渲染、lbGeo 重算、鼠标映射（ScaleMouseCoords 每次实时读
// Rotated）三者始终一致，不存在半帧用旧方向渲染的过渡态。
//
// D3D11 管线同步：app.Rotated 翻转后立即 setRendererRotation（现有方法，
// 内部调 D3D11Renderer.SetRotation → C 侧 D3D11R_SetRotation 旋转开关 +
// lbStale 重算 letterbox），SetRotation 对同值幂等。GDI/Metal/Linux 渲染器
// 同样有 SetRotation，共用此入口。
//
// -landscape 参数在 tebox 模式被忽略：方向由流分辨率决定，不由参数决定
// （main.go 的 tebox 分支固定 Rotated=false 起步）。

// teboxRotationLocked 防止解码线程与解码线程（唯一调用方）之外的并发翻转。
// 实际上 ApplyTeboxOrientation 只在 decodeLoop 单 goroutine 调用，CAS 仅
// 防御性保留——成本一次原子操作级。
var teboxOrientationMu atomic.Bool

// teboxOrientationFor 纯方向判定：给定解码分辨率与当前状态，返回新 Rotated 值。
// 宽>高→true（横屏流走 V3 旋转管线）；宽<高→false；宽==高→保持 current
// （正方形流无方向语义，翻转会抖动）。返回 changed 表示状态需要更新。
func teboxOrientationFor(w, h int32, current bool) (rotated, changed bool) {
	switch {
	case w > h:
		return true, !current
	case w < h:
		return false, current
	default: // w == h
		return current, false
	}
}

// ApplyTeboxOrientation tebox 方向自适应入口：按解码分辨率更新 App.Rotated
// 并同步到渲染管线。仅 tebox 模式生效（RTSP 模式的方向由 -landscape 决定，
// 不受流分辨率影响）。返回是否发生翻转（供日志）。
//
// 调用点：decodeLoop 每个有效解码帧（SendFrame 之前）——分辨率中途切换
// （竖↔横）时下一帧立即生效。不用 SPS 预判（SPS 只给编码分辨率，与解码输出
// 可能有裁剪差异，且需引入完整 SPS parser；首帧只晚几十 ms）。
func (a *App) ApplyTeboxOrientation(w, h int32) bool {
	if a.CastMode != CastModeTebox {
		return false
	}
	if w <= 0 || h <= 0 {
		return false
	}
	// 单调用方（decodeLoop），CAS 仅防御重入。
	if !teboxOrientationMu.CompareAndSwap(false, true) {
		return false
	}
	defer teboxOrientationMu.Store(false)

	cur := a.Rotated.Load()
	next, changed := teboxOrientationFor(w, h, cur)
	if !changed {
		return false
	}
	a.Rotated.Store(next)
	a.setRendererRotation(next)
	logf("[tebox] orientation: %dx%d -> rotated=%v\n", w, h, next)
	return true
}

// onTeboxOrientationChanged 横竖切换后的窗口适配：通过 OnTeboxRefit 回调
// （UI 创建时注册，Windows=重置一次性初始 fit 让泵 refit 窗口；其他平台按
// 需实现或空）请求窗口重新 fit 到新比例——否则竖窗放横屏画面只剩 letterbox
// 大黑边。UI 未注册（尚未创建）时无操作，首帧 fit 兜底。
func (a *App) onTeboxOrientationChanged() {
	if a.OnTeboxRefit != nil {
		a.OnTeboxRefit()
	}
}
