package screenshot

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// v4Targets 构造带 InstanceID 的 V4 目标（走自定义抓图通道，不碰 9082）
func v4Targets(count int) []ContainerTarget {
	targets := make([]ContainerTarget, count)
	for i := range targets {
		targets[i] = ContainerTarget{
			DeviceID:   "10.10.2.142",
			DeviceIP:   "10.10.2.142",
			IndexNum:   i + 1,
			InstanceID: fmt.Sprintf("coloros-%d", i+1),
			Backend:    "coloros",
		}
	}
	return targets
}

var pngPayload = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x01}

// TestV4FetchUsesTargetFetcher 带 InstanceID 的目标必须走注入的抓图回调，
// 且缓存 key 仍是 "IP_indexNum"（前端按此 key 取图）。
func TestV4FetchUsesTargetFetcher(t *testing.T) {
	targets := v4Targets(2)
	var mu sync.Mutex
	var got []ContainerTarget
	svc := newTestService(t, targets, nil)
	svc.SetTargetFetcher(func(ctx context.Context, tg ContainerTarget) ([]byte, string, error) {
		mu.Lock()
		got = append(got, tg)
		mu.Unlock()
		return pngPayload, "image/png", nil
	})

	svc.RefreshNow()

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("应抓取 2 个目标，实际 %d 个", len(got))
	}
	for _, tg := range got {
		if tg.InstanceID == "" || tg.Backend != "coloros" {
			t.Errorf("回调收到不完整的实例标识: %+v", tg)
		}
	}
	for i := 1; i <= 2; i++ {
		key := fmt.Sprintf("10.10.2.142_%d", i)
		item := svc.cache[key]
		if item == nil || item.Status != "ok" || item.DataURL == "" {
			t.Fatalf("key=%s 应有成功截图，实际 %+v", key, item)
		}
	}
}

// TestV4FetchWithoutFetcherNoFallback 未注入抓图回调时，V4 目标不得回落到 9082
// （V4 主机没有该端口，回落只会超时刷日志）。此处用「端口不可达的 9082 目标」验证
// 不会被尝试：回调缺失 → 直接记错误状态。
func TestV4FetchWithoutFetcherNoFallback(t *testing.T) {
	targets := v4Targets(1)
	svc := newTestService(t, targets, nil)

	done := make(chan struct{})
	go func() {
		svc.RefreshNow()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("缺少抓图回调时应快速失败，不应等待 9082 超时")
	}
	if item := svc.cache["10.10.2.142_1"]; item == nil || item.Status == "ok" {
		t.Fatalf("缺少回调时不应产生成功截图，实际 %+v", item)
	}
}

// TestV4FetchFailureKeepsOldData 抓图失败（如 503 未 running）时保留旧图，只更新状态
func TestV4FetchFailureKeepsOldData(t *testing.T) {
	targets := v4Targets(1)
	svc := newTestService(t, targets, nil)
	var fail atomic.Bool
	svc.SetTargetFetcher(func(ctx context.Context, tg ContainerTarget) ([]byte, string, error) {
		if fail.Load() {
			return nil, "", errors.New("实例画面不可用（HTTP 503）")
		}
		return pngPayload, "image/png", nil
	})

	svc.RefreshNow()
	first := svc.cache["10.10.2.142_1"]
	if first == nil || first.DataURL == "" {
		t.Fatal("首轮应抓到图")
	}
	dataURL := first.DataURL

	fail.Store(true)
	svc.ResetBackoff() // 绕过未变化退避，强制再抓一轮
	svc.RefreshNow()

	item := svc.cache["10.10.2.142_1"]
	if item.DataURL != dataURL {
		t.Error("抓图失败不应清空旧截图（卡片应保留最后一帧）")
	}
	if item.Status == "ok" {
		t.Errorf("连续失败后状态不应仍是 ok，实际 %q", item.Status)
	}
}

// TestV4FetchContentUnchangedNoNewVersion 服务端画面未变（静态桌面）时不得产生新版本，
// 否则前端每轮都重取同图（实测连续 3 次抓取字节完全一致）。
func TestV4FetchContentUnchangedNoNewVersion(t *testing.T) {
	targets := v4Targets(1)
	var calls int32
	svc := newTestService(t, targets, nil)
	svc.SetTargetFetcher(func(ctx context.Context, tg ContainerTarget) ([]byte, string, error) {
		atomic.AddInt32(&calls, 1)
		return pngPayload, "image/png", nil
	})

	svc.RefreshNow()
	v1 := svc.cache["10.10.2.142_1"].Version
	time.Sleep(5 * time.Millisecond)
	svc.RefreshNow()
	v2 := svc.cache["10.10.2.142_1"].Version

	if atomic.LoadInt32(&calls) < 2 {
		t.Fatalf("应抓取 2 轮，实际 %d 次", calls)
	}
	if v1 != v2 {
		t.Errorf("内容未变化不应产生新版本: %d → %d", v1, v2)
	}
}

// 通道优先级：公网（DeviceIP 是 device_id）必须仍走 publicSnap，
// 即便同时带 InstanceID（V4 目标绝不会带 device_id，这里只是保证优先级明确）。
func TestV4PublicChannelStillTakesPriority(t *testing.T) {
	targets := []ContainerTarget{{
		DeviceID: "dev-1", DeviceIP: "p948574a41c277dcea536ea328e8a7e4", IndexNum: 1,
		InstanceID: "coloros-1", Backend: "coloros",
	}}
	svc := newTestService(t, targets, nil)
	var publicCalled, v4Called atomic.Int32
	svc.SetPublicSnap(func(deviceID string, indexNum int) ([]byte, error) {
		publicCalled.Add(1)
		return pngPayload, nil
	})
	svc.SetTargetFetcher(func(_ context.Context, tg ContainerTarget) ([]byte, string, error) {
		v4Called.Add(1)
		return pngPayload, "image/png", nil
	})

	svc.RefreshNow()

	if publicCalled.Load() != 1 || v4Called.Load() != 0 {
		t.Fatalf("公网目标应走 publicSnap（public=%d v4=%d）", publicCalled.Load(), v4Called.Load())
	}
}
