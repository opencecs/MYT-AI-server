package screenshot

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// 修复 3 单测：截图抓取失败不翻新版本号。
//
// 语义：版本号只在成功抓图（storeImage）时前进。失败翻版本号 → 前端 poll 发现
// 「版本变了但 GetScreenshots 无数据」→ 清图闪断（v2 主机 mytsdk 忙时间歇失败时
// 截图成片丢失）。

// TestUpdateErrorKeepsVersion 有旧图的条目连续失败后版本号不变、旧图保留。
func TestUpdateErrorKeepsVersion(t *testing.T) {
	svc := NewService()
	seed := &CacheItem{DataURL: "data:image/jpeg;base64,old", Version: 12345, Status: "ok"}
	svc.cache["192.0.2.2_1"] = seed

	for i := 0; i < maxFailCount+2; i++ {
		svc.updateError("192.0.2.2_1", "error")
	}

	item := svc.cache["192.0.2.2_1"]
	if item.Version != 12345 {
		t.Errorf("失败不应翻新版本号: got %d want 12345", item.Version)
	}
	if item.DataURL != "data:image/jpeg;base64,old" {
		t.Error("失败不应清空旧截图")
	}
	if item.Status != "offline" {
		t.Errorf("连续失败应进入 offline 退避，实际 %q", item.Status)
	}
	if item.FailCount != maxFailCount+2 {
		t.Errorf("失败计数应累计: got %d want %d", item.FailCount, maxFailCount+2)
	}
	if item.SkipUntil.IsZero() {
		t.Error("offline 应设置退避 SkipUntil")
	}
}

// TestUpdateErrorFirstFailureNoVersion 从未成功过的条目首次失败：不产生版本号
// （版本 0 = 无图，前端按无图处理，不会误判为新版本）。
func TestUpdateErrorFirstFailureNoVersion(t *testing.T) {
	svc := NewService()
	svc.updateError("192.0.2.2_1", "error")

	item := svc.cache["192.0.2.2_1"]
	if item == nil {
		t.Fatal("首次失败应建缓存条目（记录退避状态）")
	}
	if item.Version != 0 {
		t.Errorf("从未成功的条目失败不应有版本号: got %d want 0", item.Version)
	}
	if item.DataURL != "" {
		t.Error("从未成功的条目不应有图片数据")
	}
}

// TestFetchFailureVersionUnchangedEndToEnd 端到端：成功抓图 → 连续失败 → 版本号
// 停留在最后一次成功的版本（GetScreenshotVersions 输出不变，前端不触发清图）。
func TestFetchFailureVersionUnchangedEndToEnd(t *testing.T) {
	var fail atomic.Bool
	var seq atomic.Int64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		// 每次成功返回不同内容：内容未变化时 storeImage 本就不翻版本（既有语义），
		// 测试「版本前进」必须换内容
		_, _ = w.Write([]byte("jpeg-" + strconv.FormatInt(seq.Add(1), 10)))
	}))
	defer ts.Close()

	svc := newTestService(t, snapTargets(portOf(ts), 1), nil)
	svc.RefreshNow()
	first := svc.GetScreenshotVersions()["127.0.0.1_1"]
	if first == 0 {
		t.Fatal("首轮成功应有版本号")
	}

	fail.Store(true)
	svc.ResetBackoff()
	svc.RefreshNow()
	svc.ResetBackoff()
	svc.RefreshNow()

	if got := svc.GetScreenshotVersions()["127.0.0.1_1"]; got != first {
		t.Errorf("失败轮不应翻新版本号: got %d want %d", got, first)
	}
	if got := svc.GetScreenshots([]string{"127.0.0.1_1"})["127.0.0.1_1"]; got == "" {
		t.Error("失败轮不应清空旧图")
	}

	// 恢复后版本号前进（成功仍翻版本）。版本号是 UnixMilli 时间戳，测试全程
	// 毫秒级完成，可能与首轮落在同一毫秒——先跨过毫秒边界保证确定性。
	fail.Store(false)
	time.Sleep(2 * time.Millisecond)
	svc.ResetBackoff()
	svc.RefreshNow()
	if got := svc.GetScreenshotVersions()["127.0.0.1_1"]; got == first {
		t.Error("恢复成功后版本号应前进")
	}
}
