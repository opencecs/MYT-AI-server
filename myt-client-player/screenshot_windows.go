//go:build windows

package main

import (
	"os"
	"path/filepath"
)

func screenshotDir() string {
	exe, err := os.Executable()
	if err != nil {
		return filepath.Join(os.Getenv("USERPROFILE"), "img")
	}
	dir := filepath.Join(filepath.Dir(exe), "img")
	os.MkdirAll(dir, 0o755)
	return dir
}
