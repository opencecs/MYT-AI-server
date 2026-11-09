//go:build windows

package discover

import (
	"log"
	"syscall"
)

func setBroadcast(fd uintptr) {
	if err := syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1); err != nil {
		log.Printf("[discover] 设置SO_BROADCAST失败: %v", err)
	}
}
