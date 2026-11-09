//go:build !production

package main

import (
	"fmt"
	"os"
	"sync"
	"time"
)

var (
	logMu   sync.Mutex
	logFile *os.File
	logID   string
)

func logInit(id, dir string) {
	logID = id
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	path := dir + "/go_legacy_" + id + ".log"
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	logFile = f
}

func logf(format string, args ...any) {
	msg := time.Now().Format("15:04:05.000") + " " + fmt.Sprintf(format, args...)
	if logID != "" {
		msg = "[" + logID + "] " + msg
	}
	logMu.Lock()
	defer logMu.Unlock()
	if logFile != nil {
		logFile.WriteString(msg)
	}
	fmt.Fprint(os.Stderr, msg)
}

func logln(args ...any) {
	msg := time.Now().Format("15:04:05.000") + " " + fmt.Sprintln(args...)
	if logID != "" {
		msg = "[" + logID + "] " + msg
	}
	logMu.Lock()
	defer logMu.Unlock()
	if logFile != nil {
		logFile.WriteString(msg)
	}
	fmt.Fprint(os.Stderr, msg)
}
