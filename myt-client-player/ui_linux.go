//go:build linux

package main

/*
#cgo pkg-config: x11 gtk+-3.0 webkit2gtk-4.1

#include <X11/Xlib.h>
#include <X11/Xutil.h>
#include <X11/Xatom.h>
#include <X11/keysym.h>
#include <gtk/gtk.h>
#include <gdk/gdkx.h>
#include <webkit2/webkit2.h>
#include <JavaScriptCore/JavaScript.h>
#include <string.h>
#include <stdlib.h>

// ─── GTK 骨架 helper（W1：方案 a 整体 GTK 化）──────────────────────────────

// myGtkInit gtk_init_check 包装（C 宏/函数混合，统一 static 包装风格）。
static int myGtkInit(void) { return gtk_init_check(NULL, NULL) ? 1 : 0; }

// myForceX11Backend 限制 GDK 只允许 X11 后端（GTK ≥3.24）。须在 gtk_init 前调用。
// 与 Go 侧 os.Setenv("GDK_BACKEND","x11") 双保险：env 因继承被覆盖时此接口仍生效。
static void myForceX11Backend(void) { gdk_set_allowed_backends("x11"); }

// gtkWindowNew 建主窗口（TOPLEVEL）+ GtkFixed 布局容器 + 底部导航区 GtkDrawingArea。
// 布局：video GtkDrawingArea 占 (0,0,w,h-navH)，nav 区占 (0,h-navH,w,navH)。
// 返回主窗 widget；videoArea/navArea 经出参回传。
// gtkWindowNew 建主窗口（TOPLEVEL）+ GtkFixed 布局容器。
// 布局（对照 ui_windows_create.go:132-161 的 win 版结构）：
//   video 区 (0,0,videoW,h-navH)；nav 区 (0,h-navH,videoW,navH)；
//   侧栏 (videoW,0,sideW,h)（sideW>0 时创建 WebKitWebView，MVP2 默认展开 300）。
// videoArea/navArea/sideArea 经出参回传；sideW<=0 表示无侧栏。
static GtkWidget* gtkWindowNew(int w, int h, int navH, int sideW,
                               GtkWidget** videoArea, GtkWidget** navArea, GtkWidget** sideArea) {
    GtkWidget* win = gtk_window_new(GTK_WINDOW_TOPLEVEL);
    gtk_window_set_default_size(GTK_WINDOW(win), w, h);
    gtk_window_set_resizable(GTK_WINDOW(win), TRUE);
    GtkWidget* fixed = gtk_fixed_new();
    gtk_container_add(GTK_CONTAINER(win), fixed);
    GtkWidget* video = gtk_drawing_area_new();
    gtk_widget_set_size_request(video, w - sideW, h - navH);
    gtk_fixed_put(GTK_FIXED(fixed), video, 0, 0);
    GtkWidget* nav = gtk_drawing_area_new();
    gtk_widget_set_size_request(nav, w - sideW, navH);
    gtk_fixed_put(GTK_FIXED(fixed), nav, 0, h - navH);
    GtkWidget* side = NULL;
    if (sideW > 0) {
        // 容器占位；WebKitWebView 由 Go 侧替换内容。
        gtk_widget_set_size_request(side, sideW, h);
        gtk_fixed_put(GTK_FIXED(fixed), side, w - sideW, 0);
    }
    *videoArea = video;
    *navArea = nav;
    *sideArea = side;
    return win;
}

static void myShowAll(GtkWidget* w) { gtk_widget_show_all(w); }
static void mySetTitle(GtkWidget* w, const char* t) { gtk_window_set_title(GTK_WINDOW(w), t); }
static void myGtkMain(void) { gtk_main(); }
static void myGtkQuit(void) { gtk_main_quit(); }

// myWidgetXid 取 widget 的原生 X11 Window id（渲染器/截图需要 X drawable）。
static unsigned long myWidgetXid(GtkWidget* w) {
    GdkWindow* gw = gtk_widget_get_window(w);
    if (!gw) return 0;
    return gdk_x11_window_get_xid(gw);
}

// myConnectDelete connect delete-event → 关窗退出（替代 WM_DELETE_WINDOW 轮询）。
static void myConnectDelete(GtkWidget* win) {
    g_signal_connect(win, "delete-event", G_CALLBACK(gtk_main_quit), NULL);
}

// myVideoEvents 给视频区 widget 设置事件接收前提：has_window（GtkDrawingArea 默认
// no-window，事件归有窗 ancestor，本 widget 的 button/motion/key 信号永不触发）+
// 事件 mask + can_focus。nav 区用 myNavEvents 同理。
static void myVideoEvents(GtkWidget* video) {
    gtk_widget_set_has_window(video, TRUE);
    gtk_widget_add_events(video,
        GDK_BUTTON_PRESS_MASK | GDK_BUTTON_RELEASE_MASK | GDK_POINTER_MOTION_MASK |
        GDK_BUTTON1_MOTION_MASK | GDK_BUTTON_MOTION_MASK | GDK_SCROLL_MASK |
        GDK_KEY_PRESS_MASK | GDK_KEY_RELEASE_MASK);
    gtk_widget_set_can_focus(video, TRUE);
}

// myNavEvents 导航栏事件接收（has_window + button mask；对照 myVideoEvents）。
static void myNavEvents(GtkWidget* nav) {
    gtk_widget_set_has_window(nav, TRUE);
    gtk_widget_add_events(nav,
        GDK_BUTTON_PRESS_MASK | GDK_BUTTON_RELEASE_MASK);
}

static void myMove(GtkWidget* win, int x, int y) { gtk_window_move(GTK_WINDOW(win), x, y); }

// myVideoAllocated video widget 的当前分配尺寸（configure-event 等价：size-allocate）。
static void myVideoSize(GtkWidget* video, int* w, int* h) {
    GtkAllocation a;
    gtk_widget_get_allocation(video, &a);
    *w = a.width;
    *h = a.height;
}

// ─── X11 helper（渲染器仍需：独立 Display 连接直绘 GdkWindow XID）──────────

// disableResizeHints 关闭 WM_NORMAL_HINTS 的 PResizeInc/PMinSize/PMaxSize 语义干扰
// （go_legacy 自行管理窗口尺寸）。
static void setWMName(Display* dpy, Window w, const char* name) {
    XStoreName(dpy, w, name);
    Atom wmName = XInternAtom(dpy, "_NET_WM_NAME", False);
    Atom utf8 = XInternAtom(dpy, "UTF8_STRING", False);
    if (wmName != None && utf8 != None) {
        XChangeProperty(dpy, w, wmName, utf8, 8, PropModeReplace,
                        (unsigned char*)name, (int)strlen(name));
    }
}

// myDefaultScreen 同为 Xlib 宏（DefaultScreen 在 Xlib.h 是 #define），包装展开。
static int myDefaultScreen(Display* dpy) { return DefaultScreen(dpy); }

// ─── WebKitGTK 侧栏（W4/W5；实装在 ui_linux_webview.go 的 Go 侧）────────────

// Go 导出函数前向声明（定义在文件尾部 //export；声明置于调用方之前防 implicit）。
extern void linuxGoWebMessage(long long uiId, char* msg);

// sideOnLoadTramp load-changed 的 static trampoline（preamble 独立编译单元，
// 多副本各单元自洽无符号冲突；句柄经 user_data 传回，与 myResize 系列同模式）。
static gboolean sideOnLoadTramp(WebKitWebView* wv, WebKitLoadEvent ev, gpointer data) {
    if (ev == WEBKIT_LOAD_FINISHED) linuxGoSideLoaded((long long)(long)data);
    return FALSE;
}

// sideOnMessage 前向声明（sideCreate 的 G_CALLBACK 引用；定义在下方）。
static gboolean sideOnMessage(WebKitUserContentManager* mgr, WebKitJavascriptResult* res, long long uiId);

// sideCreate 创建 WebKitWebView + user content manager：
//  - 注册 script message handler "external"（JS→Go 通道）
//  - 注入 bridge user script（window.external/chrome.webview → messageHandlers，
//    且优先 window.hostSend 由 Go 直接覆盖；对照 ui_darwin.go:446-447 的 shim）
//  - 返回 WebView widget；消息经 linuxGoWebMessage 回 Go 分发。
static GtkWidget* sideCreate(long long uiId) {
    WebKitWebView* wv = WEBKIT_WEB_VIEW(webkit_web_view_new());
    WebKitUserContentManager* mgr = webkit_web_view_get_user_content_manager(wv);
    WebKitUserScript* bridge = webkit_user_script_new(
        "window.hostSend=function(m){window.webkit.messageHandlers.external.postMessage(m);};"
        "if(!window.external){window.external={invoke:function(m){window.hostSend(m);}};}"
        "if(!window.chrome){window.chrome={webview:{postMessage:function(m){window.hostSend(m);}}};}",
        WEBKIT_USER_CONTENT_INJECT_ALL_FRAMES, WEBKIT_USER_SCRIPT_INJECT_AT_DOCUMENT_START, NULL, NULL);
    webkit_user_content_manager_add_script(mgr, bridge);
    webkit_user_script_unref(bridge);
    webkit_user_content_manager_register_script_message_handler(mgr, "external");
    g_signal_connect(mgr, "script-message-received", G_CALLBACK(sideOnMessage), (gpointer)(long)uiId);
    g_signal_connect(wv, "load-changed", G_CALLBACK(sideOnLoadTramp), (gpointer)(long)uiId);
    return GTK_WIDGET(wv);
}

// sideOnMessage JS→Go 消息中转（GTK 主线程）。
static gboolean sideOnMessage(WebKitUserContentManager* mgr, WebKitJavascriptResult* res, long long uiId) {
    (void)mgr;
    JSCValue* v = webkit_javascript_result_get_js_value(res);
    char* s = jsc_value_to_string(v);
    linuxGoWebMessage(uiId, s);
    free(s);
    return TRUE;
}

// myFixedReplace GtkFixed 内替换子 widget（占位 → WebKitWebView），绝对定位不变。
static void myFixedReplace(GtkWidget* fixed, GtkWidget* old, GtkWidget* nw,
                           int x, int y, int w, int h) {
    gtk_container_remove(GTK_CONTAINER(fixed), old);
    gtk_fixed_put(GTK_FIXED(fixed), nw, x, y);
    gtk_widget_set_size_request(nw, w, h);
}

// sideLoadHTML 加载面板单页（composeSidePanelHTML 产物）。
static void sideLoadHTML(GtkWidget* wv, const char* html) {
    webkit_web_view_load_html(WEBKIT_WEB_VIEW(wv), html, NULL);
}

// ─── Go 导出回调（ui_linux.go 尾部 //export 实现）───────────────────────────
extern int linuxGoRenderTick(long long uiId);
extern void linuxGoKeyPress(long long uiId, unsigned int keyval, unsigned int state,
                            unsigned int unicodeChar, int pressed);
extern void linuxGoButton(long long uiId, double x, double y, unsigned int button, int pressed);
extern void linuxGoMotion(long long uiId, double x, double y, unsigned int state);
extern void linuxGoScroll(long long uiId, double x, double y, int dir);
extern void linuxGoVideoResized(long long uiId, int w, int h);
extern void linuxGoVideoExposed(long long uiId);
extern void linuxGoNavDraw(long long uiId, cairo_t* cr, int w, int h);
extern void linuxGoNavClicked(long long uiId, int x, int y, int w, int h);

// onRenderTick g_timeout 回调：16ms 渲染节拍 → Go presentTick。
static gboolean onRenderTick(long long uiId) { return linuxGoRenderTick(uiId); }

// goAddRenderTick 注册渲染节拍 source（GTK 主循环驱动）。
static void goAddRenderTick(long long uiId) {
    g_timeout_add(16, onRenderTick, (gpointer)(long)uiId);
}

// requestQuit 线程安全的退出请求：g_idle_add 投递到主循环执行 gtk_main_quit
// （gtk_main_quit 非线程安全，信号 goroutine 不能直接调）。
static gboolean requestQuitIdle(void* unused) {
    (void)unused;
    gtk_main_quit();
    return G_SOURCE_REMOVE;
}
static void requestQuit(void) { g_idle_add(requestQuitIdle, NULL); }

// goWireSignals 连接 video widget 的 GDK 事件信号 → Go 导出回调。
// keyval/state 与 X keysym/KeyButMask 同值域（GDK 直用 keysym），映射表可复用。
static gboolean onKeyPress(GtkWidget* w, GdkEventKey* e, long long uiId) {
    (void)w;
    GdkKeymap* km = gdk_keymap_get_default();
    guint uc = gdk_keyval_to_unicode(e->keyval);
    // 无 Unicode 映射（功能键）时取小写 keysym 保持可读字符语义。
    if (uc == 0) {
        guint lv = gdk_keymap_translate_keyboard_state(km, e->hardware_keycode,
                     (GdkModifierType)e->state, e->group, NULL, NULL, NULL, NULL);
        if (lv) { guint low = gdk_keyval_to_lower(lv); uc = gdk_keyval_to_unicode(low); }
    }
    linuxGoKeyPress(uiId, e->keyval, e->state, uc, 1);
    return FALSE; // 继续默认处理
}
static gboolean onKeyRelease(GtkWidget* w, GdkEventKey* e, long long uiId) {
    (void)w;
    linuxGoKeyPress(uiId, e->keyval, e->state, 0, 0);
    return FALSE;
}
static gboolean onButtonPress(GtkWidget* w, GdkEventButton* e, long long uiId) {
    (void)w;
    linuxGoButton(uiId, e->x, e->y, e->button, 1);
    return TRUE; // 已处理，防焦点链抢走
}
static gboolean onButtonRelease(GtkWidget* w, GdkEventButton* e, long long uiId) {
    (void)w;
    linuxGoButton(uiId, e->x, e->y, e->button, 0);
    return TRUE;
}
static gboolean onMotion(GtkWidget* w, GdkEventMotion* e, long long uiId) {
    (void)w;
    linuxGoMotion(uiId, e->x, e->y, e->state);
    return FALSE;
}
static gboolean onScroll(GtkWidget* w, GdkEventScroll* e, long long uiId) {
    (void)w;
    // GDK_SCROLL_UP=0 / GDK_SCROLL_DOWN=1；斜向滚动按垂直分量处理。
    if (e->direction == GDK_SCROLL_UP || e->direction == GDK_SCROLL_DOWN)
        linuxGoScroll(uiId, e->x, e->y, (int)e->direction);
    return TRUE;
}
static gboolean onExpose(GtkWidget* w, GdkEventExpose* e, long long uiId) {
    (void)w; (void)e;
    linuxGoVideoExposed(uiId);
    return FALSE;
}

// onSizeAllocate video widget 分配尺寸变化 → Go UpdateSize（ConfigureNotify 等价）。
static void onSizeAllocate(GtkWidget* w, GdkRectangle* alloc, long long uiId) {
    linuxGoVideoResized(uiId, alloc->width, alloc->height);
}

// navOnDraw 导航栏 draw 信号（GTK 主线程，cairo 绘制三 pill）。
static gboolean navOnDraw(GtkWidget* w, cairo_t* cr, long long uiId) {
    (void)w;
    GtkAllocation a;
    gtk_widget_get_allocation(w, &a);
    linuxGoNavDraw(uiId, cr, a.width, a.height);
    return FALSE;
}

// navOnPress 导航栏点击 → Go 命中判定（W3）。
static gboolean navOnPress(GtkWidget* w, GdkEventButton* e, long long uiId) {
    (void)w;
    GtkAllocation a;
    gtk_widget_get_allocation(w, &a);
    linuxGoNavClicked(uiId, (int)e->x, (int)e->y, a.width, a.height);
    gtk_widget_queue_draw(w); // 刷新按下态
    return TRUE;
}

static void goWireSignals(long long uiId, GtkWidget* video, GtkWidget* nav) {
    g_signal_connect(video, "key-press-event", G_CALLBACK(onKeyPress), (gpointer)(long)uiId);
    g_signal_connect(video, "key-release-event", G_CALLBACK(onKeyRelease), (gpointer)(long)uiId);
    g_signal_connect(video, "button-press-event", G_CALLBACK(onButtonPress), (gpointer)(long)uiId);
    g_signal_connect(video, "button-release-event", G_CALLBACK(onButtonRelease), (gpointer)(long)uiId);
    g_signal_connect(video, "motion-notify-event", G_CALLBACK(onMotion), (gpointer)(long)uiId);
    g_signal_connect(video, "scroll-event", G_CALLBACK(onScroll), (gpointer)(long)uiId);
    g_signal_connect(video, "expose-event", G_CALLBACK(onExpose), (gpointer)(long)uiId);
    g_signal_connect_after(video, "size-allocate", G_CALLBACK(onSizeAllocate), (gpointer)(long)uiId);
    g_signal_connect(nav, "draw", G_CALLBACK(navOnDraw), (gpointer)(long)uiId);
    g_signal_connect(nav, "button-press-event", G_CALLBACK(navOnPress), (gpointer)(long)uiId);
}

*/
import "C"

import (
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	linuxNavBarH = 48 // MVP1 无侧边栏，仅保留导航栏高度常量供布局对齐（无 UI）
)

// matrixScale 矩阵投屏窗口缩放因子（0.6~1.0），main.go 由 -scale 赋值
// （与 ui_darwin_arrange.go / ui_windows_msg.go 的同名变量互斥，build tag 隔离）。
var matrixScale float64

var _ = linuxNavBarH // W3 导航栏绘制前占位引用（W1 先建 nav 区域布局）

// LinuxUI Linux 平台 UI：GTK 窗口（MVP2 W1，方案 a 整体 GTK 化）+ X11 渲染器直绘。
// 结构：GtkWindow(TOPLEVEL) → GtkFixed → [video GtkDrawingArea (0,0,w,h-navH),
// nav GtkDrawingArea (0,h-navH,w,navH)]。渲染层继续用 Xlib 画视频区 XID（MVP1 管线
// 零改动）；事件经 GDK 信号（key/button/motion/scroll/size）→ handle* 函数。
type LinuxUI struct {
	app *App
	mu  sync.Mutex

	dpy          uintptr // 渲染器独立 X11 连接（XOpenDisplay）
	win          uintptr // 视频 X drawable（video GtkDrawingArea 的 XID）
	gtkWin       *C.GtkWidget
	videoArea    *C.GtkWidget
	navArea      *C.GtkWidget
	sideArea     *C.GtkWidget
	webView      *C.GtkWidget
	ren          atomicPtrRenderer
	handle       C.longlong // C 侧回调持有的 UI 句柄（注册表反查）
	baseW, baseH int
	sideW        int // 侧栏宽度（0=无侧栏；52=折叠/300=展开）

	wheelDir   int
	wheelCount int
	wheelLast  time.Time
	wheelTimer *time.Timer

	// navPressedBtn 导航栏按下态 pill 索引（-1=无；MVP2 简单版无 hover）。
	navPressedBtn int
}

// atomicPtrRenderer 简化 atomic.Pointer 包装（Renderer 递归引用避免循环）。
type atomicPtrRenderer struct {
	p atomic.Pointer[Renderer]
}

func (a *atomicPtrRenderer) Store(r *Renderer)          { a.p.Store(r) }
func (a *atomicPtrRenderer) Load() *Renderer            { return a.p.Load() }
func (a *atomicPtrRenderer) Swap(n *Renderer) *Renderer { return a.p.Swap(n) }

func init() {
	_newPlatformUIImpl = func(app *App) PlatformUI {
		return NewLinuxUI(app)
	}
}

func NewLinuxUI(app *App) *LinuxUI {
	ui := &LinuxUI{app: app, navPressedBtn: -1}
	// 初始窗口尺寸（对照 DarwinUI.NewDarwinUI 的默认值；-winw/-winh 优先）
	ui.baseW, ui.baseH = 412, 780
	if app.Landscape {
		ui.baseW, ui.baseH = 900, 600
	}
	if app.Controlled {
		if matrixScale > 0 && matrixScale < 1.0 {
			ui.baseW = int(float64(ui.baseW) * matrixScale)
			ui.baseH = int(float64(ui.baseH) * matrixScale)
		}
	}
	if *flagWinW > 0 {
		ui.baseW = *flagWinW
	}
	if *flagWinH > 0 {
		ui.baseH = *flagWinH
	}
	return ui
}

// Run 主流程：GTK 初始化 → 建窗口/布局 → 渲染器 → Connect → GTK 主循环。
//
// Display 连接策略：渲染器继续用独立 XOpenDisplay 连接（不用 GDK 的 XDisplay）。
// 理由：GTK/GDK 持有自己的 Display 连接且不保证 XInitThreads；渲染器在其自有线程
// 语义下（所有 Xlib 调用固定在 GTK 主线程的 g_timeout 回调里）用独立连接直绘
// video widget 的 XID，两连接操作同一窗口是 X11 允许的标准模式（各自 flush 各自
// 队列），避免与 GDK 内部事件队列交叉。Expose 竞态由 GTK expose-event 触发
// RenderFrame 重绘兜底。
func (ui *LinuxUI) Run() {
	app := ui.app
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// 注册句柄：C 侧只持有 int64 句柄（cgo 不允许含 Go 指针对象传 C，Pin 不递归）。
	ui.handle = registerUI(ui)
	defer unregisterUI(ui.handle)

	// 强制 GDK 走 X11 后端：COSMIC 等会话默认 GDK_BACKEND=wayland,x11，GTK 选
	// Wayland 后端时 GdkWindow 非 X11 → gdk_x11_window_get_xid 断言失败 → 渲染
	// 器拿不到 XID。双保险：os.Setenv（必须在任何 GDK 调用前，本进程无 cgo 构造
	// 函数抢先初始化 GDK）+ C 侧 gdk_set_allowed_backends（GTK ≥3.24，正规接口，
	// 文档要求在 gtk_init() 之前调用）。
	os.Setenv("GDK_BACKEND", "x11")
	C.myForceX11Backend()

	if C.myGtkInit() == 0 {
		logf("[ui] gtk_init_check failed (DISPLAY=%q)\n", os.Getenv("DISPLAY"))
		return
	}

	navH := int(linuxNavBarH)
	// 侧栏宽度（对照 windows：NoSidebar=0；MVP2 默认展开 300px，W5 TogglePanel 切换）
	sideW := int(sideExpandedDefault)
	if app.NoSidebar {
		sideW = int(sideWidthNoSide)
	}
	ui.sideW = sideW
	var videoArea, navArea, sideArea *C.GtkWidget
	gtkWin := C.gtkWindowNew(C.int(ui.baseW), C.int(ui.baseH), C.int(navH), C.int(sideW), &videoArea, &navArea, &sideArea)
	if gtkWin == nil {
		logf("[ui] gtkWindowNew failed\n")
		return
	}
	ui.gtkWin = gtkWin
	ui.videoArea = videoArea
	ui.navArea = navArea
	ui.sideArea = sideArea

	title := *flagTitle
	if title == "" {
		title = "LgPlayer"
	}
	tcs := C.CString(title)
	C.mySetTitle(gtkWin, tcs)
	C.free(unsafe.Pointer(tcs))
	C.myConnectDelete(gtkWin)
	C.myVideoEvents(videoArea)
	C.myNavEvents(navArea)

	// 窗口位置：哨兵判断（与 Windows createWindows 同语义）
	const winPosUnset = -32000
	if *flagWinX != winPosUnset || *flagWinY != winPosUnset {
		C.myMove(gtkWin, C.int(*flagWinX), C.int(*flagWinY))
	}
	C.myShowAll(gtkWin)

	// 视频区 XID（渲染目标）。GdkWindow 在 show 后才存在。
	ui.win = uintptr(C.myWidgetXid(videoArea))
	if ui.win == 0 {
		logf("[ui] video XID unavailable\n")
		return
	}

	// 渲染器独立 Display 连接（理由见函数头注释）；视频区有效高度扣 navH。
	dpy := C.XOpenDisplay(nil)
	if dpy == nil {
		logf("[ui] XOpenDisplay failed (DISPLAY=%q)\n", os.Getenv("DISPLAY"))
		return
	}
	ui.dpy = uintptr(unsafe.Pointer(dpy))

	// WM 名称（GDK 设过窗口标题；_NET_WM_NAME 增强，保持 MVP1 行为）
	wcs := C.CString(title)
	C.setWMName(dpy, C.Window(ui.win), wcs)
	C.free(unsafe.Pointer(wcs))

	app.OnSignalShutdown = func() { ui.shutdown() }
	app.RequestClose = func() { ui.shutdown() }

	ren, err := NewRenderer(ui.win, ui.dpy, ui.baseW, ui.baseH-navH, app.Rotated.Load(), *flagFill)
	if err != nil {
		logf("[ui] renderer init failed: %v\n", err)
		return
	}
	ren.SetInputHandler(app)
	ui.ren.Store(ren)
	app.X11Ren.Store(ren) // 注册到 App 渲染器槽：帧投递/首帧确认/鼠标缩放/截图都经此取渲染器
	defer ren.Close()

	app.ClipboardWriter = func(text string) { linuxWriteClipboard(text) }
	app.OnConnStateChange = func(state, retry int) {
		if r := ui.ren.Load(); r != nil {
			r.SetOverlay(connStateText(state, retry))
		}
	}

	// GDK 事件接线（对照 MVP1 XEvent 分支语义）：
	// key-press/release → handleKey；button-press/release → handleButton；
	// motion-notify-event → handleMotion；scroll-event → 滚轮手势；
	// size-allocate → UpdateSize（尺寸变化 + RenderFrame）；
	// expose-event → RenderFrame（X11 Expose 等价）。
	C.goWireSignals(ui.handle, videoArea, navArea)

	// W4：侧栏 WebKitWebView（NoSidebar 时不创建，对齐 windows :157）
	if sideArea != nil {
		ui.webView = C.sideCreate(ui.handle)
		if ui.webView != nil {
			// GtkDrawingArea 占位 → 换成 WebKitWebView 填充同区域（GtkFixed 内）
			C.myFixedReplace(C.gtk_widget_get_parent(ui.sideArea), ui.sideArea, ui.webView,
				C.int(ui.baseW-sideW), 0, C.int(sideW), C.int(ui.baseH))
			html := composeSidePanelHTML()
			if list := ctrlListJSON(app.CtrlItems); list != "[]" {
				html = strings.Replace(html, "ctrlList:[]", "ctrlList:"+list, 1)
			}
			// 注入初始状态（windows 在 loadHTML 后 Eval；这里 load 完成信号后推，
			// 简化：loadHTML 后延迟一次 updateState——见 syncStateDelayed）
			hcs := C.CString(html)
			C.sideLoadHTML(ui.webView, hcs)
			C.free(unsafe.Pointer(hcs))
			C.myShowAll(ui.webView)
			ui.syncStateToPanel()
			logf("[ui] sidebar webkit created (%dpx)\n", sideW)
		}
	}

	if app.RTSPURL != "" || app.CastMode == CastModeTebox {
		go func() {
			if err := app.Connect(); err != nil {
				logf("[ui] connect failed: %v\n", err)
			}
		}()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		ui.shutdown()
	}()

	// 16ms 渲染节拍（替代 MVP1 的 poll 超时路径；g_timeout 回调在 GTK 主线程，
	// 满足渲染器 X11 调用线程约束）。
	C.goAddRenderTick(ui.handle)

	C.myGtkMain() // 阻塞到 gtk_main_quit（delete-event / shutdown）

	app.Disconnect()
}

// shutdown 关窗退出（信号兜底路径；delete-event 直接触发 gtk_main_quit）。
func (ui *LinuxUI) shutdown() {
	C.requestQuit() // g_idle_add 投递主循环（gtk_main_quit 非线程安全）
}

// presentTick 16ms 渲染节拍（g_timeout 回调，GTK 主线程）。
// RenderFrame = 排空 frameCh → 有帧 renderFrame / 无帧重绘 repaintX / 无源涂黑 renderIdle。
// （MVP1 教训：不能只调 Present——frameCh 满后帧全部被丢，必须每拍排空队列。）
func (ui *LinuxUI) presentTick() {
	if r := ui.ren.Load(); r != nil {
		r.RenderFrame()
	}
}

// ─── GDK 事件回调（cgo export；GTK 主线程调用）────────────────────────────

//export linuxGoRenderTick
func linuxGoRenderTick(p C.longlong) C.gboolean {
	ui := lookupUI(p)
	if ui == nil {
		return 0 // G_SOURCE_REMOVE：句柄已注销（延迟回调）
	}
	ui.presentTick()
	return 1 // G_SOURCE_CONTINUE
}

//export linuxGoKeyPress
func linuxGoKeyPress(p C.longlong, keyval, state, unicodeChar C.uint, pressed C.int) {
	if ui := lookupUI(p); ui != nil {
		ui.handleKeyGTK(pressed != 0, uint32(keyval), uint32(state), uint32(unicodeChar))
	}
}

//export linuxGoButton
func linuxGoButton(p C.longlong, x, y C.double, button C.uint, pressed C.int) {
	if ui := lookupUI(p); ui != nil {
		switch int(button) {
		case 1, 2, 3:
			ui.handleMouseClick(pressed != 0, int(button), int(x), int(y))
		}
	}
}

//export linuxGoMotion
func linuxGoMotion(p C.longlong, x, y C.double, state C.uint) {
	ui := lookupUI(p)
	if ui == nil {
		return
	}
	btn := 0
	if state&C.GDK_BUTTON1_MASK != 0 {
		btn = 1
	}
	ui.handleMotion(int(x), int(y), btn)
}

//export linuxGoScroll
func linuxGoScroll(p C.longlong, x, y C.double, dir C.int) {
	ui := lookupUI(p)
	if ui == nil {
		return
	}
	// GDK ScrollDirection: UP=0, DOWN=1（对照 X Button4=上/Button5=下）
	sdir := int32(1)
	if int(dir) == 1 { // GDK_SCROLL_DOWN
		sdir = -1
	}
	ui.queueWheelGesture(int(x), int(y), sdir)
}

//export linuxGoVideoResized
func linuxGoVideoResized(p C.longlong, w, h C.int) {
	ui := lookupUI(p)
	if ui == nil || w <= 0 || h <= 0 {
		return
	}
	ui.mu.Lock()
	ui.baseW, ui.baseH = int(w), int(h)
	ui.mu.Unlock()
	if r := ui.ren.Load(); r != nil {
		r.UpdateSize(int32(w), int32(h))
		r.RenderFrame()
	}
}

//export linuxGoVideoExposed
func linuxGoVideoExposed(p C.longlong) {
	if ui := lookupUI(p); ui != nil {
		if r := ui.ren.Load(); r != nil {
			r.RenderFrame()
		}
	}
}

// handleKeyGTK GDK 键事件 → 现有 handleKey 语义（keyval 即 X keysym 值域，
// linux_keys.go 的 keysym→SDL 映射表直接复用；state 的 GDK 掩码与 X11 KeyButMask 同值）。
func (ui *LinuxUI) handleKeyGTK(pressed bool, keyval, state, unicodeChar uint32) {
	app := ui.app
	if app.Ctrl.Load() == nil {
		return
	}
	scancode, sym := xkeysymToSDL(keyval)
	mods := xmodsToSDL(state)
	textChar := uint32(0)
	if pressed && mods&(ModCtrl|ModAlt) == 0 && unicodeChar >= 32 && unicodeChar != 127 {
		textChar = unicodeChar
	}

	// Ctrl+C/X/V → 设备 API 剪贴板（对照 Win/mac 路径同语义）
	if pressed && mods&ModCtrl != 0 {
		switch keyval {
		case 0x63, 0x43: // c / C
			go ui.ClipboardCopy()
			return
		case 0x78, 0x58: // x / X
			go ui.ClipboardCut()
			return
		case 0x76, 0x56: // v / V
			go func() {
				if text := ui.ReadClipboardText(); text != "" {
					app.PasteClipboard(text)
				}
			}()
			return
		}
	}

	// 功能键（无 Ctrl/Alt）
	if pressed && mods&(ModCtrl|ModAlt) == 0 {
		cmdID := funcKeyCmdID(xKeySymToKeyName(keyval))
		if cmdID > 0 {
			app.SendCmd(CmdFuncKey, uint32(cmdID))
			return
		}
	}
	// Esc → Android 返回键
	if pressed && keyval == 0xFF1B {
		app.SendCmd(CmdAndroidKey, KeyBack)
		return
	}

	if sym == 0 && scancode == 0 {
		return
	}
	app.SendKeyboard(pressed, scancode, int32(sym), 0, uint16(mods))

	// 可打印字符随按下发送文本（对照 DarwinUI.handleKey 尾部）
	if pressed && mods&(ModCtrl|ModAlt) == 0 && textChar >= 32 && textChar != 127 {
		app.SendText(string(rune(textChar)))
	}
}
