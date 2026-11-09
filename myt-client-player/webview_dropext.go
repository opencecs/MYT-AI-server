//go:build windows

// webview_dropext.go — WebView2「文件拖放穿透给宿主窗口」支持。
//
// 背景（为什么不用依赖自带方法）：官方 jchv/go-webview2 没有封装
// ICoreWebView2Controller4::AllowExternalDrop。此前靠手改 vendor/ 里的依赖副本
// 实现，而 vendor/ 被 .gitignore 忽略、不入库——本地编译通过、他人 clone 必然
// 报 `ui.chromium.SetAllowExternalDrop undefined`（2026-09-20 复现确认）。
// 现改为在子模块内自行经 QueryInterface 取 Controller4 调用，只依赖官方包
// 已导出的 edge.ComProc / edge.NewGUID / edge.Chromium.GetController。
//
// 作用：拖文件到播放器窗口时，若 WebView2 自行消化拖放，只能走 JS FileReader
// 读成 base64 回传（V8 字符串上限 200MB、且有 33% 膨胀）；关掉后拖放穿透到
// 宿主窗口，走 WM_DROPFILES → handleDropFiles 拿到磁盘路径 → Go 流式上传
// （io.Pipe 零内存拷贝，944MB 实测通过）。点击选择文件走系统对话框，不经此路径。
//
// 兼容性：需 WebView2 Runtime ≥ 100.0.1185.39；旧运行时 QueryInterface 返回
// 失败 → no-op（JS 拖放兜底，行为同未打补丁）。
package main

import (
	"unsafe"

	"github.com/jchv/go-webview2/pkg/edge"
)

// iidController4 ICoreWebView2Controller4 的 IID。
const iidController4 = "{97d418d5-a426-4e49-a151-e1a10f327d9e}"

// vtable 全局槽位（IUnknown 在前，之后按接口继承顺序排列）：
//
//	槽 0-2   IUnknown        QueryInterface / AddRef / Release
//	槽 3-35  Controller/2/3  GetIsVisible … PutBoundsMode
//	槽 36    Controller4     GetAllowExternalDrop
//	槽 37    Controller4     PutAllowExternalDrop
const (
	vtblQueryInterface       = 0
	vtblRelease              = 2
	vtblPutAllowExternalDrop = 37
)

// comProcAt 从 vtable 基址取第 idx 槽的函数指针。
func comProcAt(vtbl uintptr, idx uintptr) edge.ComProc {
	return edge.ComProc(*(*uintptr)(unsafe.Add(unsafe.Pointer(vtbl), idx*unsafe.Sizeof(uintptr(0)))))
}

// setAllowExternalDrop 关闭 WebView2 对文件拖放的自行消化（allow=false 时
// 拖放穿透给宿主窗口的 WM_DROPFILES）。不支持的运行时静默返回。
func setAllowExternalDrop(chromium *edge.Chromium, allow bool) {
	if chromium == nil {
		return
	}
	controller := chromium.GetController()
	if controller == nil {
		return
	}

	// controller 是 *edge.ICoreWebView2Controller（COM 接口指针），
	// 其首地址即 vtable 指针——直接当 IUnknown 用，无需访问包内私有字段。
	obj := uintptr(unsafe.Pointer(controller))
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	if vtbl == 0 {
		return
	}

	// QueryInterface 取 Controller4
	iid := edge.NewGUID(iidController4)
	if iid == nil {
		return
	}
	var c4 uintptr
	hr, _, _ := comProcAt(vtbl, vtblQueryInterface).Call(
		obj,
		uintptr(unsafe.Pointer(iid)),
		uintptr(unsafe.Pointer(&c4)),
	)
	if hr != 0 || c4 == 0 { // 非 S_OK 或空指针：运行时过旧，放弃（JS 兜底）
		return
	}

	// PutAllowExternalDrop(allow)
	v4 := *(*uintptr)(unsafe.Pointer(c4))
	if v4 == 0 {
		return
	}
	arg := uintptr(0)
	if allow {
		arg = 1
	}
	_, _, _ = comProcAt(v4, vtblPutAllowExternalDrop).Call(c4, arg)

	// 释放 QueryInterface 取得的引用（引用计数已由 QI 增加）
	_, _, _ = comProcAt(v4, vtblRelease).Call(c4)
}
