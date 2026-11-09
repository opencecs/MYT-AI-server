package util

import "log"

// Debugf 仅在开发模式下输出日志，生产构建中静默
func Debugf(format string, args ...any) {
	if DebugEnabled {
		log.Printf(format, args...)
	}
}
