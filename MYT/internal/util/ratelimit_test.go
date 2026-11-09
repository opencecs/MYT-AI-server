package util

import (
	"sync"
	"testing"
	"time"
)

func TestAcquireReleaseDeviceHTTP(t *testing.T) {
	// 先清空信号量，确保从干净状态开始
	for len(deviceHTTPSem) > 0 {
		<-deviceHTTPSem
	}

	AcquireDeviceHTTP()
	if len(deviceHTTPSem) != 1 {
		t.Fatalf("AcquireDeviceHTTP 后应占用 1 个槽位，实际 %d", len(deviceHTTPSem))
	}
	ReleaseDeviceHTTP()
	if len(deviceHTTPSem) != 0 {
		t.Fatalf("ReleaseDeviceHTTP 后应释放槽位，实际剩余 %d", len(deviceHTTPSem))
	}
}

func TestMaxDeviceHTTPConcurrency(t *testing.T) {
	if MaxDeviceHTTPConcurrency != 16 {
		t.Errorf("MaxDeviceHTTPConcurrency = %d, want 16", MaxDeviceHTTPConcurrency)
	}
}

func TestMinHTTPInterval(t *testing.T) {
	if minHTTPInterval != 20*time.Millisecond {
		t.Errorf("minHTTPInterval = %v, want %v", minHTTPInterval, 20*time.Millisecond)
	}
}

func TestConcurrentAcquireRelease(t *testing.T) {
	// 先清空信号量
	for len(deviceHTTPSem) > 0 {
		<-deviceHTTPSem
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			AcquireDeviceHTTP()
			time.Sleep(10 * time.Millisecond)
			ReleaseDeviceHTTP()
		}()
	}
	wg.Wait()
	// 不 panic 即通过

	// 清理
	for len(deviceHTTPSem) > 0 {
		<-deviceHTTPSem
	}
}
