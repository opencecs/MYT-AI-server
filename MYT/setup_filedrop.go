package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// registerFileDropHandler 注册窗口文件拖拽事件：仅落点带 data-file-drop-target 属性的元素才触发
// （Wails 运行时自动过滤，未标记的元素拖拽照常走 HTML5 onDrop，如 AgentInput 的图片附件）。
//
// 落点命中后，WebView2 原生 API ICoreWebView2File.GetPath 拿到真实文件路径，BuildUploadEntries
// stat 构建 UploadFileEntry，再通过 "files-dropped" 事件推给前端；前端按 targetId 决定是否接收
// （如 BatchUploadModal 的 dropzone），实现"拖拽到指定区域上传"。
func registerFileDropHandler(window *application.WebviewWindow, app *application.App, fileSvc *FileService) {
	if window == nil || app == nil || fileSvc == nil {
		return
	}
	window.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		ctx := e.Context()
		paths := ctx.DroppedFiles()
		if len(paths) == 0 {
			return
		}
		entries := fileSvc.BuildUploadEntries(paths)
		if len(entries) == 0 {
			return
		}
		targetID := ""
		if t := ctx.DropTargetDetails(); t != nil {
			targetID = t.ElementID
		}
		app.Event.Emit("files-dropped", map[string]any{
			"targetId": targetID,
			"entries":  entries,
		})
	})
}
