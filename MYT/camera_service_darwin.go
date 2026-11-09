//go:build darwin

package main

import "fmt"

// 以下包级函数是 macOS 桩：camera_service.go（//go:build windows）在 darwin 不
// 参与编译，但跨平台文件 screencast_service.go / screencast_service_batch.go /
// stream_service.go 引用了这些符号。darwin 上摄像头推流功能不可用，统一返回
// "不支持"或 no-op，调用方已对空/错误结果做容错处理（CamPushExe 为空 = 不启用）。

// ensureFFmpegExe 跨平台文件 screencast_service.go / screencast_service_batch.go
// 调用，返回错误让调用方跳过摄像头推流功能。
func ensureFFmpegExe() (string, error) {
	return "", fmt.Errorf("macOS 不支持摄像头推流")
}

func startWindowsPusher(args []string) map[string]interface{} {
	return map[string]interface{}{"success": false, "message": "macOS 不支持摄像头推流"}
}

func stopWindowsPusher(id int) map[string]interface{} {
	return map[string]interface{}{"success": true, "message": "会话不存在"}
}

func stopAllWindowsPushers() {}

func capturePreviewFrame(camName string, width, height int) map[string]interface{} {
	return map[string]interface{}{"success": false, "message": "macOS 不支持摄像头推流"}
}

func listCameraDevices() map[string]interface{} {
	return map[string]interface{}{"success": false, "message": "macOS 不支持摄像头推流", "cameras": []string{}}
}
