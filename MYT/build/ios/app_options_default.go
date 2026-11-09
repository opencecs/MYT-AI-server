//go:build !ios

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// modifyOptionsForIOS is a no-op on non-iOS platforms
func modifyOptionsForIOS(opts *application.Options) {
	// No modifications needed for non-iOS platforms
}

// main 空实现：保证非 iOS 平台上 `go build ./...` 能编译本 package main
// （iOS 平台的真实 main 在 main_ios.go，此文件被 //go:build !ios 排除，无冲突）
func main() {}