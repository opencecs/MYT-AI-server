package util

import (
	"log"
	"runtime"
)

// numCPU 返回逻辑 CPU 核心数
var numCPU = runtime.NumCPU()

// ParallelWorkers 根据任务类型返回合理的并发 worker 数
// IO 密集型（网络请求）：CPU / 2，至少 4
// CPU 密集型：CPU 数量
// UDP 收发：CPU / 2，至少 2
func ParallelWorkers(kind string) int {
	switch kind {
	case "io":
		n := numCPU / 2
		if n < 4 {
			n = 4
		}
		return n
	case "cpu":
		return numCPU
	case "udp":
		n := numCPU / 2
		if n < 2 {
			n = 2
		}
		return n
	default:
		return numCPU
	}
}

// GoSafe 安全启动 goroutine，发生 panic 时 recover 并打日志，避免整个进程崩溃
func GoSafe(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] goroutine recovered: %v", r)
			}
		}()
		fn()
	}()
}
