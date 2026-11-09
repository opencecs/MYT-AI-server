//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/jchv/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

func (ui *WindowsUI) runWebViewMode(app *App) {
	ui.mu.Lock()
	ui.overlayText = ""
	ui.mu.Unlock()
	ui.layoutChildren()
	if !app.Hidden {
		procShowWindow.Call(uintptr(ui.hParent), SW_SHOW)
	}

	// 侧边栏：保留原有 WebView2 侧边栏（含底部控制按钮）
	if !app.NoSidebar {
		ui.chromium = edge.NewChromium()
		ui.chromium.DataPath = filepath.Join(os.TempDir(), "lgplayer_legacy_wv2")
		ui.chromium.MessageCallback = func(raw string) {
			logf("[sidebar] MessageCallback: %s\n", raw)
			app.dispatchMessage(raw, ui)
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					logf("[ui] sidebar WebView2 init failed (recovered): %v", r)
					ui.chromium = nil
				}
			}()
			if ui.chromium.Embed(uintptr(ui.hSidePanel)) {
				// 文件拖放穿透给宿主窗口（WM_DROPFILES）：dropzone 拖拽与视频区拖拽
				// 同路径走 Win32 文件名 + 流式上传，无 base64 大小限制。
				// 需 WebView2 Runtime ≥100.0.1185.39；旧运行时 no-op（JS drop 兜底）。
				// 实现见 webview_dropext.go（依赖官方包无此方法，自带 QueryInterface）
				setAllowExternalDrop(ui.chromium, false)
				ui.chromium.Resize()
				ui.chromium.Show()
				html := composeSidePanelHTML()
				if list := ctrlListJSON(app.CtrlItems); list != "[]" {
					html = strings.Replace(html, "ctrlList:[]", "ctrlList:"+list, 1)
				}
				ui.chromium.NavigateToString(html)
			}
		}()
	}

	// 公网矩阵投屏：在 webChromium 之前创建 MatrixManager（回调需引用它）
	if app.MatrixListenPort > 0 {
		// 被控才需要 inject 配置（主控本地操控走页面原生 handler）
		var inject *MatrixInjectConfig
		if !app.MatrixPrimary && app.WebURL != "" {
			inject = parseMatrixInjectConfig(app.WebURL, app.Landscape)
		}
		app.MatrixMgr = NewMatrixManager(app.MatrixListenPort, app.MatrixPrimary, app.MatrixTargets, inject)
	}

	// SDL 区域：用 WebView2 加载 player URL（替代 D3D11/GDI 渲染）
	ui.webChromium = edge.NewChromium()
	ui.webChromium.DataPath = filepath.Join(os.TempDir(), "lgplayer_legacy_webview")
	// 页面消息回调：旋转探查 + 矩阵事件（mx）+ fanout 探针（fanout）
	ui.webChromium.MessageCallback = func(raw string) {
		if strings.Contains(raw, "__rotate_probe") {
			logf("[rotate-probe] %s\n", raw)
			return
		}
		if strings.Contains(raw, `"__fit"`) {
			logf("[fitprobe] %s\n", raw)
			ui.updateWebVideoSize(raw)
			return
		}
		if app.MatrixMgr != nil && strings.Contains(raw, `"mx"`) {
			app.MatrixMgr.HandlePageEvent(raw)
		}
		if strings.Contains(raw, `"fanout"`) {
			handleFanoutMessage(raw)
		}
		if strings.Contains(raw, `"gi"`) {
			handleGroupInputMessage(raw)
		}
	}
	// 侧边栏按钮命令转发给 player 页面（通过 postMessage，与 ScreenCastPanel 一致）
	app.WebCmdForward = func(cmd string) {
		logf("[web] WebCmdForward: cmd=%s webChromium=%v\n", cmd, ui.webChromium != nil)
		// 公网群控方案 C：主控时优先经 UDP nav 事件扇出（被控调 __mytControl）
		if app.MatrixMgr != nil && app.MatrixMgr.IsPrimary() {
			if action := navAction(cmd); action != "" {
				app.MatrixMgr.SendNav(action)
			}
		}
		// 兜底：myt-agent 注入接口未就绪时走 batch-rpa HTTP 扇出
		if groupRPA != nil {
			HandleGroupKey(cmd)
		}
		if ui.webChromium != nil {
			ui.webChromium.Eval(fmt.Sprintf("window.postMessage({type:'myt-nav',cmd:'%s'},'*')", cmd))
		}
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				logf("[ui] player WebView2 init failed (recovered): %v", r)
				ui.webChromium = nil
			}
		}()
		if ui.webChromium.Embed(uintptr(ui.hSdlArea)) {
			ui.webChromium.Resize()
			ui.webChromium.Show()
			// 页面加载前 WebView2 默认白底会先闪一下白才显示远端(深色)加载动画。
			// 把控制器默认背景设为黑，消除"先白"闪烁，直接进入深色等待加载动画。
			if c2 := ui.webChromium.GetController().GetICoreWebView2Controller2(); c2 != nil {
				c2.PutDefaultBackgroundColor(edge.COREWEBVIEW2_COLOR{A: 255, R: 0, G: 0, B: 0})
			}
			if settings, err := ui.webChromium.GetSettings(); err == nil {
				settings.PutAreDevToolsEnabled(true)
				settings.PutAreDefaultContextMenusEnabled(true)
			}
		ui.webChromium.NavigationCompletedCallback = func(sender *edge.ICoreWebView2, args *edge.ICoreWebView2NavigationCompletedEventArgs) {
			logf("[ui] WebView2 navigation completed\n")
			// 远端 play 页面（h5_lgair）对视频元素不做等比例收敛，窗口格比例与视频不一时
			// 画面按宽度拉伸、高度超出视频区，顶部/底部被外框遮挡。注入 JS：把视频按各自
			// 宽高比 contain 缩放到视口内，并让所有祖先容器隐藏溢出。视频元素可能晚于
			// navigation completed 才出现，故用 MutationObserver + 轮询持续重排。
			ui.webChromium.Eval(`(function(){
				return; // contain 注入已禁用——曾导致画面缩小 50%、四周黑边
				if (window.__mytFitInstalled) return;
				window.__mytFitInstalled = true;
				// 无条件上报一次：确认注入已执行 + 页面里是否存在 video 元素
				try { window.chrome.webview.postMessage(JSON.stringify({
					"__fit":1, probe:"inject-ran",
					hasVideo: !!document.querySelector('video'),
					videos: document.querySelectorAll('video').length,
					iframes: document.querySelectorAll('iframe').length,
					iw: window.innerWidth, ih: window.innerHeight
				})); } catch(e){}
				var lastVideo = null;
				function clipAncestors(v){
					var el = v.parentElement;
					while (el && el !== document.documentElement) { el.style.overflow = 'hidden'; el = el.parentElement; }
					document.documentElement.style.overflow = 'hidden';
					document.body.style.overflow = 'hidden';
				}
				function contain(v, vw, vh){
					var vr = (v.videoWidth && v.videoHeight) ? v.videoWidth / v.videoHeight : 0;
					var w = vw, h = vh;
					if (vr > 0) {
						var wr = vw / vh;
						if (vr > wr) { w = vw; h = vw / vr; } else { h = vh; w = vh * vr; }
					}
					function sp(c, val){ v.style.setProperty(c, val, 'important'); }
					sp('width', w + 'px'); sp('height', h + 'px');
					sp('max-width', '100%'); sp('max-height', '100%');
					sp('margin', 'auto'); sp('display', 'block'); sp('object-fit', 'contain');
				}
				function watch(v){
					if (!v || v.__mytWatched) return;
					v.__mytWatched = true;
					// 视频元数据/首帧/尺寸变化后重排：元数据加载前只有 fill 尺寸（vr=0）会被裁；
					// readyState>=2 后 videoWidth 才可用，此刻必须重 fit 到真实宽高比。
					v.addEventListener('loadedmetadata', fit);
					v.addEventListener('loadeddata', fit);
					v.addEventListener('canplay', fit);
					if (v.videoWidth && v.videoHeight) fit();
				}
				function fit(){
					var v = document.querySelector('video');
					if (!v) return;
					if (v !== lastVideo) { clipAncestors(v); lastVideo = v; }
					watch(v);
					var vw = window.innerWidth, vh = window.innerHeight;
					if (vw <= 0 || vh <= 0) return;
					contain(v, vw, vh);
					var r = v.getBoundingClientRect();
					try { window.chrome.webview.postMessage(JSON.stringify({
						"__fit":1, iw:vw, ih:vh, vw:r.width, vh:r.height,
						rt:Math.round(r.top), rb:Math.round(r.bottom), rl:Math.round(r.left), vr:(v.videoWidth||0)+'x'+(v.videoHeight||0)
					})); } catch(e) {}
				}
				fit();
				window.addEventListener('resize', fit);
				try {
					var mo = new MutationObserver(function(){ fit(); });
					mo.observe(document.documentElement, { childList: true, subtree: true });
				} catch(e) {}
				// 轮询持续到视频真实分辨率就绪（vr 可知）：即便 video 晚于注入出现、
				// 或首帧 fit 时元数据未加载，也会在分辨率确定后重新 contain，消除残留裁切。
				var n = 0;
				var iv = setInterval(function(){
					var v = document.querySelector('video');
					var known = v && v.videoWidth && v.videoHeight;
					fit();
					if ((known && ++n >= 6) || n >= 120) clearInterval(iv);
				}, 500);
			})()`)
			// 只读探针：拿远端页面 video 的真实宽高与显示占位，不改任何样式（contain 已禁用）。
			// 用于确定画面真实比例与视频区(外框)是否匹配，从而决定修法。
			ui.webChromium.Eval(`(function(){
				var n = 0; function report(){
					var v = document.querySelector('video'); var d = v ? v.getBoundingClientRect() : null;
					try { window.chrome.webview.postMessage(JSON.stringify({
						"__fit":1, probe:"readonly", hasVideo: !!v,
						vr: v && v.videoWidth ? v.videoWidth + 'x' + v.videoHeight : '0x0',
						vw: v ? v.videoWidth : 0, vh: v ? v.videoHeight : 0,
						dw: d ? Math.round(d.width) : 0, dh: d ? Math.round(d.height) : 0,
						rt: d ? Math.round(d.top) : 0, rb: d ? Math.round(d.bottom) : 0,
						rl: d ? Math.round(d.left) : 0, rr: d ? Math.round(d.right) : 0,
						iw: window.innerWidth, ih: window.innerHeight,
						tf: v ? (v.style.transform || 'none') : 'none'
					})); } catch(e){}
					if ((v && v.videoWidth) || ++n > 40) clearInterval(iv);
				} report(); var iv = setInterval(report, 500);
			})()`)
			// 显示 h5_lgair 自带的底部导航按钮（play.html 默认隐藏），
			// 并劫持其点击事件：库自带 handler 走的 ajax 通道已被 myt-agent 注入脚本劫持为假成功，
			// 真正可用的是 WebSocket 上的 h5_lgair.sendCmdEvent（与 myt-nav postMessage 同链路）。
			ui.webChromium.Eval(`(function(){
				var s = document.createElement('style');
				s.textContent = '#ctr_goHome,#ctr_goBack,#ctr_goClean{display:flex !important;}';
				document.head.appendChild(s);
				if (window.__mytNavHooked) return;
				window.__mytNavHooked = true;
				var cmdMap = { ctr_goHome: 'goHome', ctr_goBack: 'goBack', ctr_goClean: 'goClean' };
				document.addEventListener('click', function(e) {
					var t = e.target && e.target.closest ? e.target.closest('#ctr_goHome,#ctr_goBack,#ctr_goClean') : null;
					if (!t) return;
					var cmd = cmdMap[t.id];
					if (!cmd) return;
					e.preventDefault();
					e.stopPropagation();
					if (window.h5_lgair && window.h5_lgair.sendCmdEvent) {
						console.log('[myt-nav] sendCmdEvent: ' + cmd);
						window.h5_lgair.sendCmdEvent(cmd);
					} else {
						console.log('[myt-nav] h5_lgair 未就绪: ' + cmd);
					}
				}, true);
				console.log('[myt-nav] 导航按钮劫持已安装');
			})()`)
			// 矩阵 JS 注入（所有矩阵实例统一注入，由 __mxSetPrimary flag 区分是否生效）
			if app.MatrixMgr != nil {
				ui.webChromium.Eval(matrixInjectJS)
				ui.webChromium.Eval(fmt.Sprintf("window.__mxSetPrimary(%v)", app.MatrixMgr.IsPrimary()))
			}
		// 公网矩阵 v2 fanout：主控页面建 WS → WebRTC DataChannel 扇出
		if len(app.FanoutURLs) > 0 {
			ui.webChromium.Eval(buildFanoutInjectJS(app.FanoutURLs))
		}
		// 公网矩阵 v4：群控输入 HTTP 扇出（触摸/按键事件 → group-input API）
		if groupInputCfg != nil {
			logf("[groupinput] injecting JS: baseURL=%s targets=%s\n", groupInputCfg.BaseURL, groupInputCfg.Targets)
			ui.webChromium.Eval(buildGroupInputInjectJS(groupInputCfg))
		}
		}
			ui.webChromium.Init(`(function(){
				// 文档创建即置深色背景：远端播放页自身 body 起步为白，深色主题/加载动画
				// 要等其 JS 启动才出现，导致"先白"闪烁。Init 在 document 创建、任何脚本
				// 运行前注入，先铺黑底，让加载动画直接出现在深色上、无白色闪现。
				var s=document.createElement('style');
				s.textContent='html,body{background:#000 !important;}';
				document.documentElement.appendChild(s);
			})()`)
			ui.webChromium.Navigate(app.WebURL)
			logf("[ui] WebView2 navigating to: %s\n", app.WebURL)
		}
	}()

	// 启动矩阵管理器（webChromium 已就绪，设置 evalFn 后启动 UDP 监听）
	if app.MatrixMgr != nil {
		app.MatrixMgr.evalFn = func(script string) {
			// WebView2 的 ExecuteScript 必须在 UI 线程调用——Chromium.Eval 是
			// 裸 vtable 调用、无线程封送，从 UDP goroutine 直接调会静默失败。
			// 因此入队后 PostMessage 到 UI 线程，由 WM_APP_MATRIX_EVAL 统一执行。
			ui.mu.Lock()
			ui.matrixEvalQ = append(ui.matrixEvalQ, script)
			ui.mu.Unlock()
			procPostMessageW.Call(uintptr(ui.hParent), WM_APP_MATRIX_EVAL, 0, 0)
		}
		if err := app.MatrixMgr.Start(); err != nil {
			logf("[matrix] start failed: %v\n", err)
			app.MatrixMgr = nil
		}
		defer func() {
			if app.MatrixMgr != nil {
				app.MatrixMgr.Stop()
			}
		}()
	}

	app.OnSignalShutdown = func() {
		procPostMessageW.Call(uintptr(ui.hParent), WM_DESTROY, 0, 0)
	}

	// 消息循环
	var m struct {
		Hwnd    windows.HWND
		Message uint32
		WParam  uintptr
		LParam  uintptr
		Time    uint32
		Pt      struct{ X, Y int32 }
	}
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}
