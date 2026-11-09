//go:build windows

package screencast

// ArrangePlayerWindows 枚举所有投屏窗口（LgPlayerLegacy 独立投屏 + LgPlayerMatrix 矩阵投屏）并排列成网格。
// 每个窗口按各自内容比例去黑边、窗口更小；设备多时自动缩小适配屏幕。
// scale: 窗口缩放因子（0.3~1.0）。设备数量较多或网格超屏时自动缩小。
//
// manual=true（滑块拖动）：只排一次，重排所有已存在窗口。
// manual=false（自动调用）：同样只排一次。批量投屏窗口启动时已带位置（-winx 等），
// 不依赖此函数定位；这里仅用于少量遗留场景（如单台投屏居中、运行后手动调整）。
// 不再开 7.8 秒轮询重排——避免窗口反复抖动换位（布局层和渲染层解耦）。
func ArrangePlayerWindows(scale float64, manual bool) {
	doArrangePlayerWindows(scale, manual)
}
