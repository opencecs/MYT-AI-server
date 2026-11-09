//go:build linux

package main

// ui_linux_nav.go — 底部导航栏：三按钮 pill 的 cairo 绘制与点击命中（W3）。
// 视觉/几何/命令语义对照 ui_windows_nav.go：navPillLayout 同参数（pillW=h*1.6 上限
// w/3、pillH=0.72h、gap=0.7h 居中），色值照 initGDICache（Light 0xF0F0F0、Pill
// 0xE0E0E0、Act 0xD0D0D0、Icon 0x707070/0x404040）。三按钮 →
// SendCmd(CmdAndroidKey, KeyBack/KeyHome/KeyRecent)（ui_windows_nav.go:198-207）。

/*
#cgo pkg-config: gtk+-3.0

#define _USE_MATH_DEFINES
#include <math.h>

// glibc 不认 _USE_MATH_DEFINES（MSVC 宏）：非严格 ANSI 模式（gcc/g++ 默认）下
// math.h 已暴露 M_PI，但为覆盖 -std=c99 等严格模式，加保底定义。
#ifndef M_PI
#define M_PI 3.14159265358979323846
#endif

#include <gtk/gtk.h>

// navHit 按几何命中 pill（0-2）或 -1：与 Windows navHitTest（ui_windows_nav.go:28-37）
// 同一几何，Go 侧算好后直接传索引，C 侧只做绘制。

static void navDrawPill(cairo_t* cr, double x, double y, double w, double h, double rad,
                        double r, double g, double b) {
    cairo_set_source_rgb(cr, r, g, b);
    cairo_new_sub_path(cr);
    cairo_arc(cr, x+w-rad, y+rad, rad, -M_PI/2, 0);
    cairo_arc(cr, x+w-rad, y+h-rad, rad, 0, M_PI/2);
    cairo_arc(cr, x+rad, y+h-rad, rad, M_PI/2, M_PI);
    cairo_arc(cr, x+rad, y+rad, rad, M_PI, 3*M_PI/2);
    cairo_close_path(cr);
    cairo_fill(cr);
}

static void navDrawIconBack(cairo_t* cr, double cx, double cy, double half,
                            double r, double g, double b) {
    cairo_set_source_rgb(cr, r, g, b);
    cairo_move_to(cr, cx+half, cy-half);
    cairo_line_to(cr, cx-half, cy);
    cairo_line_to(cr, cx+half, cy+half);
    cairo_close_path(cr);
    cairo_fill(cr);
}

static void navDrawIconHome(cairo_t* cr, double cx, double cy, double half, double lw,
                            double r, double g, double b) {
    cairo_set_source_rgb(cr, r, g, b);
    cairo_set_line_width(cr, lw);
    cairo_arc(cr, cx, cy, half, 0, 2*M_PI);
    cairo_stroke(cr);
}

static void navDrawIconRecent(cairo_t* cr, double cx, double cy, double sz, double rad, double lw,
                              double r, double g, double b) {
    cairo_set_source_rgb(cr, r, g, b);
    cairo_set_line_width(cr, lw);
    cairo_new_sub_path(cr);
    cairo_arc(cr, cx+sz-rad, cy+rad, rad, -M_PI/2, 0);
    cairo_arc(cr, cx+sz-rad, cy+sz-rad, rad, 0, M_PI/2);
    cairo_arc(cr, cx+sz-rad, cy+sz-rad, rad, M_PI/2, M_PI);
    cairo_arc(cr, cx+sz-rad, cy+sz-rad, rad, M_PI, 3*M_PI/2);
    cairo_close_path(cr);
    cairo_stroke(cr);
}

// navPaint 完整绘制导航栏（draw 信号回调，GTK 主线程）。
// 参数从 Go 侧传（几何按 navPillLayout 数学在 Go 算，单一来源好对照）。
static void navPaint(cairo_t* cr, int w, int h,
                     double pillW, double pillH, double pillY, double gap,
                     double startX, double iconSz, double pillRad,
                     int pressedBtn) {
    // 底色 Light 0xF0F0F0（对照 hBrushLight）
    cairo_set_source_rgb(cr, 0xF0/255.0, 0xF0/255.0, 0xF0/255.0);
    cairo_paint(cr);
    double cy = h / 2.0;
    for (int i = 0; i < 3; i++) {
        double px = startX + i*(pillW+gap);
        double cx = px + pillW/2;
        if (i == pressedBtn) {
            navDrawPill(cr, px, pillY, pillW, pillH, pillRad, 0xD0/255.0, 0xD0/255.0, 0xD0/255.0);
        } else {
            navDrawPill(cr, px, pillY, pillW, pillH, pillRad, 0xE0/255.0, 0xE0/255.0, 0xE0/255.0);
        }
        int active = (i == pressedBtn);
        double ir = active ? 0x40/255.0 : 0x70/255.0;
        double ig = ir, ib = ir;
        double half = iconSz;
        double lw = 3.0;
        switch (i) {
        case 0: navDrawIconBack(cr, cx, cy, half, ir, ig, ib); break;      // ◀ filled
        case 1: navDrawIconHome(cr, cx, cy, half, lw, ir, ig, ib); break;  // ○ outline
        case 2: {                                                          // ▢ outline
            double rsz = iconSz * 0.9;
            navDrawIconRecent(cr, cx-rsz, cy-rsz, rsz*2, 4.0, lw, ir, ig, ib);
            break;
        }
        }
    }
}
*/
import "C"

// navPillGeometry Linux 版 pill 几何（与 windows navPillLayout 同一数学，ui_windows_nav.go:10-26）。
func navPillGeometry(w, h int32) (pillW, pillH, pillY, gap, startX, iconSz, pillRad int32) {
	pillW = int32(float32(h) * 1.6)
	if pillW > w/3 {
		pillW = w / 3
	}
	pillH = int32(float32(h) * 0.72)
	pillY = (h - pillH) / 2
	gap = int32(float32(h) * 0.7)
	totalW := pillW*3 + gap*2
	startX = (w - totalW) / 2
	iconSz = pillH / 3
	if iconSz < 6 {
		iconSz = 6
	}
	pillRad = pillH / 3
	if pillRad > 14 {
		pillRad = 14
	}
	return
}

// navHit 命中测试（对照 ui_windows_nav.go:28-37）：返回 0-2 或 -1。
func navHit(x, y, w, h int32) int {
	pillW, pillH, pillY, gap, startX, _, _ := navPillGeometry(w, h)
	for i := 0; i < 3; i++ {
		px := startX + int32(i)*(pillW+gap)
		if x >= px && x < px+pillW && y >= pillY && y < pillY+pillH {
			return i
		}
	}
	return -1
}

// navPaintDiag draw 信号回调（GTK 主线程，cairo 上下文即本线程）。
func (ui *LinuxUI) navPaintDiag(cr *C.cairo_t, w, h C.int) {
	pillW, pillH, pillY, gap, startX, iconSz, pillRad := navPillGeometry(int32(w), int32(h))
	ui.mu.Lock()
	pressed := ui.navPressedBtn
	ui.mu.Unlock()
	C.navPaint(cr, w, h,
		C.double(pillW), C.double(pillH), C.double(pillY), C.double(gap),
		C.double(startX), C.double(iconSz), C.double(pillRad),
		C.int(pressed))
}

// navClickDiag nav 区点击 → 三按钮命令（对照 ui_windows_nav.go:198-207）。
// WebURL 模式走 WebCmdForward（goBack/goHome/goClean，:189-197）。
func (ui *LinuxUI) navClickDiag(x, y, w, h int) {
	idx := navHit(int32(x), int32(y), int32(w), int32(h))
	if idx < 0 {
		return
	}
	app := ui.app
	if app.WebURL != "" && app.WebCmdForward != nil {
		switch idx {
		case 0:
			app.WebCmdForward("goBack")
		case 1:
			app.WebCmdForward("goHome")
		case 2:
			app.WebCmdForward("goClean")
		}
		return
	}
	switch idx {
	case 0:
		app.SendCmd(CmdAndroidKey, KeyBack)
	case 1:
		app.SendCmd(CmdAndroidKey, KeyHome)
	case 2:
		app.SendCmd(CmdAndroidKey, KeyRecent)
	}
}

//export linuxGoNavDraw
func linuxGoNavDraw(p C.longlong, cr *C.cairo_t, w, h C.int) {
	if ui := lookupUI(p); ui != nil {
		ui.navPaintDiag(cr, w, h)
	}
}

//export linuxGoNavClicked
func linuxGoNavClicked(p C.longlong, x, y, w, h C.int) {
	ui := lookupUI(p)
	if ui == nil {
		return
	}
	logf("[nav] click x=%d y=%d w=%d h=%d\n", x, y, w, h)
	ui.mu.Lock()
	idx := navHit(int32(x), int32(y), int32(w), int32(h))
	if idx >= 0 {
		ui.navPressedBtn = idx // 简单按下态（释放后由下一拍 draw 刷新回弹起）
	}
	ui.mu.Unlock()
	if idx >= 0 {
		ui.navClickDiag(int(x), int(y), int(w), int(h))
	}
}
