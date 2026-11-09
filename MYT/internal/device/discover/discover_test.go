package discover

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"
)

const testPort = 37678

// fakeDevice 回环假设备：收到 "lgcloud" 后回复 nReplies 个不同 DeviceID 的合法 v3 回复。
// 模拟真实设备形态（type:id:name_v3），可指定重复回复次数（同一设备多包）。
type fakeDevice struct {
	conn     net.PacketConn
	nDevices int
	perDev   int
}

func startFakeDevice(t *testing.T, nDevices, perDev int) *fakeDevice {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:"+strconv.Itoa(testPort))
	if err != nil {
		t.Skipf("测试端口 %d 被占用: %v", testPort, err)
	}
	fd := &fakeDevice{conn: conn, nDevices: nDevices, perDev: perDev}
	go fd.serve()
	t.Cleanup(func() { conn.Close() })
	return fd
}

func (f *fakeDevice) serve() {
	buf := make([]byte, 1024)
	for {
		n, remote, err := f.conn.ReadFrom(buf)
		if err != nil {
			return
		}
		if string(buf[:n]) != "lgcloud" {
			continue
		}
		for i := 0; i < f.nDevices; i++ {
			for j := 0; j < f.perDev; j++ {
				msg := fmt.Sprintf("t1:device%03d:q1_v3", i)
				f.conn.WriteTo([]byte(msg), remote)
			}
		}
	}
}

// TestScanHighVolumeNoDeadlock 回归死锁（2026-09-05 实测事故）：
// 回复数超过 resultCh 容量(512) 时，旧实现的 recvWg.Wait() 在 drain 之前
// 等 worker，worker 阻塞在通道发送 → 永久挂死。330 台设备 × 2 包 = 660 即触发。
func TestScanHighVolumeNoDeadlock(t *testing.T) {
	startFakeDevice(t, 700, 1) // 700 > 512

	d := &Discoverer{dialPort: testPort}
	type result struct {
		found []Device
		fails []string
	}
	done := make(chan result, 1)
	go func() {
		found, fails := d.DiscoverManually(context.Background(), []string{"127.0.0.1"})
		done <- result{found, fails}
	}()

	select {
	case r := <-done:
		if len(r.found) != 700 {
			t.Errorf("found=%d, want=700", len(r.found))
		}
		if len(r.fails) != 0 {
			t.Errorf("failedIPs=%v, want empty", r.fails)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("扫描死锁：回复数超过通道容量(512)未收敛（recvWg.Wait 先于 drain 的旧缺陷回归）")
	}
}

// TestScanIdleEarlyExit 静默提前退出：设备回复后沉默，不应等满 2s 硬窗口。
func TestScanIdleEarlyExit(t *testing.T) {
	startFakeDevice(t, 3, 1)

	d := &Discoverer{dialPort: testPort}
	start := time.Now()
	found, _ := d.DiscoverManually(context.Background(), []string{"127.0.0.1"})
	elapsed := time.Since(start)

	if len(found) != 3 {
		t.Errorf("found=%d, want=3", len(found))
	}
	// 假设备瞬时回复完毕 → 静默 600ms + tick 粒度 100ms + 收尾 ≈ 0.7~0.9s；
	// 旧实现恒为 2s+200ms。放宽到 1.5s 防止 CI 抖动误报。
	if elapsed > 1500*time.Millisecond {
		t.Errorf("耗时=%v, 静默提前退出未生效（应 <1.5s 而非等满 2s 窗口）", elapsed)
	}
	if elapsed < 300*time.Millisecond {
		t.Errorf("耗时=%v 过短：静默窗口(600ms)未生效就退出了", elapsed)
	}
}

// TestScanDedup 同一 DeviceID 多包回复只计一台（真实场景：两条广播各回一包）
func TestScanDedup(t *testing.T) {
	startFakeDevice(t, 5, 4) // 5 台设备 × 每台回 4 包

	d := &Discoverer{dialPort: testPort}
	found, _ := d.DiscoverManually(context.Background(), []string{"127.0.0.1"})

	if len(found) != 5 {
		t.Errorf("found=%d, want=5（去重失效）", len(found))
	}
	ids := map[string]bool{}
	for _, dev := range found {
		if ids[dev.DeviceID] {
			t.Errorf("DeviceID=%s 重复出现", dev.DeviceID)
		}
		ids[dev.DeviceID] = true
	}
}

// TestScanFailedIPs 无响应 IP 正确进 failedIPs（且因静默提前退出 ~0.7s 返回）
func TestScanFailedIPs(t *testing.T) {
	// 本端口无监听 → 127.0.0.1 无回复；用未占用端口模拟
	d := &Discoverer{dialPort: testPort + 1}
	start := time.Now()
	found, fails := d.DiscoverManually(context.Background(), []string{"127.0.0.1"})
	elapsed := time.Since(start)

	if len(found) != 0 {
		t.Errorf("found=%d, want=0", len(found))
	}
	if len(fails) != 1 || fails[0] != "127.0.0.1" {
		t.Errorf("failedIPs=%v, want=[127.0.0.1]", fails)
	}
	// 发送完成记基线 → 静默 600ms 提前退出，而非 2s 空等
	if elapsed > 1500*time.Millisecond {
		t.Errorf("耗时=%v, 无回复场景也应静默提前退出", elapsed)
	}
}
