//go:build linux

package main

// ui_linux_handle.go — UI 句柄注册表：C 侧不持有 Go 对象指针，只持有 int64 句柄，
// 回调经 lookupUI 反查（cgo 指针检查：含 Go 指针的对象不能传 C，Pin 不递归不可用）。
//
// 生命周期：Run() 开始 registerUI，gtk_main 退出后 unregisterUI；其间 C 回调拿到
// 的句柄若已注销（延迟回调），lookupUI 返回 nil，回调安全跳过。

import "C"

import (
	"sync"
	"sync/atomic"
)

var uiRegistry sync.Map // int64 -> *LinuxUI
var uiHandleSeq atomic.Int64

func registerUI(ui *LinuxUI) C.longlong {
	id := uiHandleSeq.Add(1)
	uiRegistry.Store(id, ui)
	return C.longlong(id)
}

func lookupUI(id C.longlong) *LinuxUI {
	v, ok := uiRegistry.Load(int64(id))
	if !ok {
		return nil
	}
	return v.(*LinuxUI)
}

func unregisterUI(id C.longlong) { uiRegistry.Delete(int64(id)) }
