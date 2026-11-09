package discover

import (
	"context"
	"testing"
	"time"
)

// TestDiscoverRealLAN 真机局域网验证（仅本地手动跑：go test -run TestDiscoverRealLAN -v）
// 验证修复后真实环境表现：不死锁 + 静默提前退出（330 台设备应 ~0.7s 完成，旧实现挂死或 2.2s）
func TestDiscoverRealLAN(t *testing.T) {
	d := NewDiscoverer()
	start := time.Now()
	found, err := d.Discover(context.Background())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Discover 失败: %v", err)
	}
	t.Logf("真实局域网: 发现 %d 台设备, 耗时 %v（修复前：>512 回复挂死 / 恒 2.2s）", len(found), elapsed)
	if elapsed > 2*time.Second+300*time.Millisecond {
		t.Errorf("耗时=%v 超过硬窗口上限，静默提前退出疑似未生效", elapsed)
	}
}
