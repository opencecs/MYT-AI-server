//go:build darwin

package main

import (
	"os"
	"path/filepath"
)

const appSupportDir = "com.moyunteng.v3ai"

func getDarwinDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, "Library", "Application Support", appSupportDir)
	os.MkdirAll(dir, 0755)
	return dir
}
