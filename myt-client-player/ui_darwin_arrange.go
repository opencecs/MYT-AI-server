//go:build darwin

package main

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"net"
)

// 本地排列协调：每个 go_legacy 监听一个本地 UDP 端口（基于 vport 派生），
// 收到 arrange 消息时自己移动自己的窗口。

const arrangeUdpBase = 49000

// matrixScale 矩阵投屏窗口缩放因子（0.6~1.0），由主程序通过 -scale 参数设置
var matrixScale float64

var (
	arrangeConn     *net.UDPConn
	arrangeOnce     sync.Once
	arrangeNotify   func()
	primaryCallback func(string)
)

func arrangeLocalPort() int {
	return arrangeUdpBase + (*flagVPort) % 1000
}

// startArrangeListener 启动本地 UDP 监听，接收其他 go_legacy 进程的排列通知。
func startArrangeListener(notify func(), onPrimary func(string)) {
	arrangeOnce.Do(func() {
		arrangeNotify = notify
		primaryCallback = onPrimary
		addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:"+strconv.Itoa(arrangeLocalPort()))
		conn, err := net.ListenUDP("udp", addr)
		if err != nil {
			logf("[arrange] listen udp %d failed: %v\n", arrangeLocalPort(), err)
			return
		}
		arrangeConn = conn
		go func() {
			buf := make([]byte, 128)
			for {
				n, _, err := conn.ReadFrom(buf)
				if err != nil {
					return
				}
				msg := string(buf[:n])
				if msg == "arrange\n" {
					if arrangeNotify != nil {
						arrangeNotify()
					}
				} else if strings.HasPrefix(msg, "primary:") {
					addr := strings.TrimSpace(strings.TrimPrefix(msg, "primary:"))
					if primaryCallback != nil {
						primaryCallback(addr)
					}
				}
			}
		}()
	})
}

// broadcastArrange 用 ps 找到所有 go_legacy 进程的 vport，通过 UDP 通知它们排列。
func broadcastArrange() {
	// ps 获取所有 go_legacy 进程的命令行，提取 -vport 参数
	out, err := exec.Command("sh", "-c", "ps -eo pid,args | grep go_legacy | grep -v grep").Output()
	if err != nil {
		return
	}
	vportRe := regexp.MustCompile(`-vport\s+(\d+)`)
	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		m := vportRe.FindStringSubmatch(line)
		if len(m) < 2 {
			continue
		}
		vport, err := strconv.Atoi(m[1])
		if err != nil || vport <= 0 {
			continue
		}
		port := arrangeUdpBase + vport%1000
		if port == arrangeLocalPort() {
			continue
		}
		addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			continue
		}
		conn, err := net.DialUDP("udp", nil, addr)
		if err != nil {
			continue
		}
		conn.Write([]byte("arrange\n"))
		conn.Close()
		count++
	}
	logf("[arrange] broadcasted to %d peers\n", count)
}

// broadcastPrimary 通过 UDP 通知其他 go_legacy 进程谁是主控窗口。
func broadcastPrimary(addr string) {
	if addr == "" {
		return
	}
	out, err := exec.Command("sh", "-c", "ps -eo pid,args | grep go_legacy | grep -v grep").Output()
	if err != nil {
		return
	}
	vportRe := regexp.MustCompile(`-vport\s+(\d+)`)
	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		m := vportRe.FindStringSubmatch(line)
		if len(m) < 2 {
			continue
		}
		vport, err := strconv.Atoi(m[1])
		if err != nil || vport <= 0 {
			continue
		}
		port := arrangeUdpBase + vport%1000
		if port == arrangeLocalPort() {
			continue
		}
		udpAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			continue
		}
		conn, err := net.DialUDP("udp", nil, udpAddr)
		if err != nil {
			continue
		}
		conn.Write([]byte("primary:" + addr + "\n"))
		conn.Close()
		count++
	}
	logf("[primary] broadcasted to %d peers\n", count)
}
