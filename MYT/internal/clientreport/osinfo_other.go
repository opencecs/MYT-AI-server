//go:build !windows && !darwin

package clientreport

import "runtime"

// OSName 非 Windows/macOS 平台的兜底实现（项目仅发布 windows/darwin，
// 此文件只为保证其他平台下 `go build ./...` 仍可编译）。
func OSName() string { return runtime.GOOS }
