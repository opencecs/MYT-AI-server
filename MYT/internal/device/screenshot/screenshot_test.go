package screenshot

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moyunteng/myt/internal/config"
)

// newTestService 构造不启动后台轮询的 Service（手动初始化 ctx），
// 用 RefreshNow 同步驱动单轮，保证断言确定性。
func newTestService(t *testing.T, targets []ContainerTarget, fn func(changedKeys []string)) *Service {
	t.Helper()
	svc := NewService()
	svc.ctx, svc.cancel = context.WithCancel(context.Background())
	t.Cleanup(svc.cancel)
	svc.SetContainerGetter(func() []ContainerTarget { return targets })
	ids := make([]string, len(targets))
	for i, tg := range targets {
		ids[i] = tg.DeviceID
	}
	svc.SetSelectedDevices(ids)
	svc.SetOnUpdated(fn)
	return svc
}

// snapTargets 构造指向本地测试服务器的截图目标（端口映射模式）
func snapTargets(port int, count int) []ContainerTarget {
	targets := make([]ContainerTarget, count)
	bindings := map[string][]config.PortBinding{
		"9082/tcp": {{HostPort: strconv.Itoa(port)}},
	}
	for i := range targets {
		targets[i] = ContainerTarget{
			DeviceID:     fmt.Sprintf("dev%d", i+1),
			DeviceIP:     "127.0.0.1",
			IndexNum:     i + 1,
			PortBindings: bindings,
		}
	}
	return targets
}

func TestInvalidateDevice(t *testing.T) {
	svc := NewService()
	seed := func(keys ...string) {
		for _, k := range keys {
			svc.cache[k] = &CacheItem{DataURL: "data:image/jpeg;base64,x", Version: 1}
		}
	}
	seed("192.0.2.2_1", "192.0.2.2_2", "192.0.2.20_1", "192.0.2.21_3")

	svc.InvalidateDevice("192.0.2.2")
	if _, ok := svc.cache["192.0.2.2_1"]; ok {
		t.Fatal("192.0.2.2_1 应被清除")
	}
	if _, ok := svc.cache["192.0.2.2_2"]; ok {
		t.Fatal("192.0.2.2_2 应被清除")
	}
	// 前缀误匹配边界：192.0.2.2_ 不得命中 192.0.2.20_* / 192.0.2.21_*
	if _, ok := svc.cache["192.0.2.20_1"]; !ok {
		t.Fatal("192.0.2.20_1 不应被误删")
	}
	if _, ok := svc.cache["192.0.2.21_3"]; !ok {
		t.Fatal("192.0.2.21_3 不应被误删")
	}

	// 空前缀：no-op（public 未解析出标识时不误伤所有缓存）
	seed("192.0.2.2_1")
	svc.InvalidateDevice("")
	if _, ok := svc.cache["192.0.2.2_1"]; !ok {
		t.Fatal("空前缀不应清除任何缓存")
	}

	// 清不存在的设备：no-op
	svc.InvalidateDevice("10.10.9.9")
	if _, ok := svc.cache["192.0.2.20_1"]; !ok {
		t.Fatal("无关设备清除应 no-op")
	}
}

func startSnapServer(t *testing.T, payload func() []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(payload())
	}))
	t.Cleanup(srv.Close)
	return srv
}

func portOf(srv *httptest.Server) int {
	return srv.Listener.Addr().(*net.TCPAddr).Port
}

// TestRefreshNowBatchedNotification 一轮 N 张新图必须合并为一次通知（携带全部 key）。
// 旧实现逐 key 通知 N 次，是前端 O(N²) 取图风暴的源头。
func TestRefreshNowBatchedNotification(t *testing.T) {
	srv := startSnapServer(t, func() []byte { return []byte("jpeg-static") })
	targets := snapTargets(portOf(srv), 3)

	var mu sync.Mutex
	var calls [][]string
	svc := newTestService(t, targets, func(keys []string) {
		mu.Lock()
		calls = append(calls, append([]string(nil), keys...))
		mu.Unlock()
	})

	svc.RefreshNow()

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 1 {
		t.Fatalf("3 张新图应恰好合批 1 次通知，实际 %d 次", len(calls))
	}
	if len(calls[0]) != 3 {
		t.Fatalf("合批通知应携带全部 3 个 key，实际 %d 个: %v", len(calls[0]), calls[0])
	}
}

// TestRefreshNowUnchangedNoReemit 内容不变时不得重复通知。
// 旧实现在未变化时丢弃比较基线（rawData 置 nil），下一轮跳过比较直接按"变化"
// 处理，导致版本自增 + 重发事件：第 3 轮必现重复通知，且未变化退避永远不触发。
func TestRefreshNowUnchangedNoReemit(t *testing.T) {
	srv := startSnapServer(t, func() []byte { return []byte("jpeg-static") })
	targets := snapTargets(portOf(srv), 2)

	var mu sync.Mutex
	var calls int
	svc := newTestService(t, targets, func(keys []string) {
		mu.Lock()
		calls++
		mu.Unlock()
	})

	svc.RefreshNow() // R1: 新图，通知 1 次
	time.Sleep(10 * time.Millisecond)
	svc.RefreshNow() // R2: 未变化，不通知
	time.Sleep(10 * time.Millisecond)
	svc.RefreshNow() // R3: 未变化（旧实现基线丢失后在此重新触发）

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("内容未变化时第 2/3 轮不应再通知（R1 共 1 次），实际 %d 次", calls)
	}
}

// TestDispatchPollNotifiesBeforeSlowTargetFinishes 慢/不可达容器不得把整轮新图
// 压到轮末才推送：旧实现在 wg.Wait() 之后一次性合批，实测一轮里有不可达目标时
// 整轮耗时 500ms(DialContext)/2000ms(fetchTimeout)，期间已抓好的图全压在通道里，
// 前端拿不到任何更新（表现为截图成片迟到后一次涌出几十张）。
func TestDispatchPollNotifiesBeforeSlowTargetFinishes(t *testing.T) {
	fast := startSnapServer(t, func() []byte { return []byte("jpeg-fast") })
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(600 * time.Millisecond) // 远大于 notifyFlushInterval
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg-slow"))
	}))
	t.Cleanup(slow.Close)

	slowKey := "127.0.0.1_4"
	targets := append(snapTargets(portOf(fast), 3), ContainerTarget{
		DeviceID: "devSlow", DeviceIP: "127.0.0.1", IndexNum: 4,
		PortBindings: map[string][]config.PortBinding{"9082/tcp": {{HostPort: strconv.Itoa(portOf(slow))}}},
	})

	var mu sync.Mutex
	var batches [][]string
	svc := newTestService(t, targets, func(keys []string) {
		mu.Lock()
		batches = append(batches, append([]string(nil), keys...))
		mu.Unlock()
	})

	svc.RefreshNow()

	mu.Lock()
	defer mu.Unlock()
	if len(batches) < 2 {
		t.Fatalf("慢目标拖长整轮时应分次落批，实际只通知 %d 次（说明仍在等整轮结束）", len(batches))
	}
	for _, k := range batches[0] {
		if k == slowKey {
			t.Fatalf("慢目标尚未返回就不该出现在第一批里: %v", batches[0])
		}
	}
	seen := make(map[string]bool)
	for _, b := range batches {
		if len(b) == 0 {
			t.Fatal("落批不应为空（空批会白推一次事件）")
		}
		for _, k := range b {
			if seen[k] {
				t.Fatalf("key %s 被重复通知，合批语义被破坏", k)
			}
			seen[k] = true
		}
	}
	if len(seen) != 4 {
		t.Fatalf("4 个目标最终都应被通知到，实际 %d 个: %v", len(seen), seen)
	}
}

// TestRefreshNowChangingReemitsEachRound 内容逐轮变化时每轮仍应恰好一次合批通知，
// 验证合批与基线逻辑不会误伤真实更新。
func TestRefreshNowChangingReemitsEachRound(t *testing.T) {
	var seq int32
	srv := startSnapServer(t, func() []byte {
		return []byte(fmt.Sprintf("jpeg-%d", atomic.AddInt32(&seq, 1)))
	})
	targets := snapTargets(portOf(srv), 3)

	var mu sync.Mutex
	var calls [][]string
	svc := newTestService(t, targets, func(keys []string) {
		mu.Lock()
		calls = append(calls, append([]string(nil), keys...))
		mu.Unlock()
	})

	svc.RefreshNow()
	time.Sleep(10 * time.Millisecond)
	svc.RefreshNow()

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("两轮均变化应各通知 1 次（共 2 次），实际 %d 次", len(calls))
	}
	for i, c := range calls {
		if len(c) != 3 {
			t.Fatalf("第 %d 轮通知应携带全部 3 个 key，实际 %d 个: %v", i+1, len(c), c)
		}
	}
}
