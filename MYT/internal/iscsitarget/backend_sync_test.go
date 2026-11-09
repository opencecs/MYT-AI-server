package iscsitarget

import (
	"bytes"
	"path/filepath"
	"testing"
)

// crashClose 模拟 target 进程被 kill -9：绕过 ThinBackend.Close()（旧实现会在
// Close 里 Flush bitmap），直接关底层文件句柄。os.File.Close 不影响 page cache，
// 已写入内容对新句柄仍可见——因此重开后读到的文件状态即进程崩溃后的真实状态
func (b *ThinBackend) crashClose(t *testing.T) {
	t.Helper()
	if err := b.file.Close(); err != nil {
		t.Fatalf("crashClose: %v", err)
	}
}

// reopenThin 模拟崩溃后重启：重新打开映像文件
func reopenThin(t *testing.T, path string) *ThinBackend {
	t.Helper()
	tb, err := OpenThinBackend(path)
	if err != nil {
		t.Fatalf("OpenThinBackend: %v", err)
	}
	t.Cleanup(func() { _ = tb.Close() })
	return tb
}

// 核心用例：Write 新块后不 Sync、走崩溃式退出，重开后数据必须完整。
// 旧实现在此必失败——bitmap 只在 Close 时 Flush，进程死亡时 bitmap 仍在内存，
// 重开后新块被误判为未分配 → 读出全零
func TestThinBitmapPersistedOnWriteWithoutSync(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crash.img")
	tb, err := NewThinBackend(path, 1)
	if err != nil {
		t.Fatalf("NewThinBackend: %v", err)
	}
	data := []byte("thin-persistence-payload-0123456789abcdef")
	off := int64(3*thinBlockSize + 512) // 块 3 中部
	if err := tb.Write(off, data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	tb.crashClose(t) // 不调用 Sync，模拟进程崩溃退出

	tb2 := reopenThin(t, path)
	got := make([]byte, len(data))
	if err := tb2.Read(off, got); err != nil {
		t.Fatalf("Read after crash-reopen: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("after crash-reopen data corrupted:\n got %q\nwant %q", got, data)
	}
}

// 同块二次写：重开后新数据生效、bitmap 标记保持（不丢块）
func TestThinWriteExistingBlockKeepsAllocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twice.img")
	tb, err := NewThinBackend(path, 1)
	if err != nil {
		t.Fatalf("NewThinBackend: %v", err)
	}
	off := int64(5*thinBlockSize + 100)
	if err := tb.Write(off, []byte("first-write")); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	tb.crashClose(t)

	tb2 := reopenThin(t, path)
	if err := tb2.Write(off, []byte("second-write")); err != nil {
		t.Fatalf("second Write: %v", err)
	}
	tb2.crashClose(t)

	tb3 := reopenThin(t, path)
	got := make([]byte, len("second-write"))
	if err := tb3.Read(off, got); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != "second-write" {
		t.Fatalf("got %q, want %q", got, "second-write")
	}
}

// Write → Sync → 崩溃式退出 → 重开：数据完整（Sync 承诺已落盘）
func TestThinSyncFlushesData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.img")
	tb, err := NewThinBackend(path, 1)
	if err != nil {
		t.Fatalf("NewThinBackend: %v", err)
	}
	data := bytes.Repeat([]byte{0xAB}, 4096)
	off := int64(10 * thinBlockSize)
	if err := tb.Write(off, data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := tb.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	tb.crashClose(t)

	tb2 := reopenThin(t, path)
	got := make([]byte, len(data))
	if err := tb2.Read(off, got); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("data mismatch after Sync + crash-reopen")
	}
}

// 未分配块读零（回归既有行为）
func TestThinUnallocatedReadZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zero.img")
	tb, err := NewThinBackend(path, 1)
	if err != nil {
		t.Fatalf("NewThinBackend: %v", err)
	}
	t.Cleanup(func() { _ = tb.Close() })
	got := make([]byte, 32)
	if err := tb.Read(2*thinBlockSize, got); err != nil {
		t.Fatalf("Read unallocated: %v", err)
	}
	if !bytes.Equal(got, make([]byte, 32)) {
		t.Fatalf("unallocated block should read zero, got %x", got)
	}
}

// 越界写报错（回归）
func TestThinWriteBeyondVirtual(t *testing.T) {
	path := filepath.Join(t.TempDir(), "beyond.img")
	tb, err := NewThinBackend(path, 1) // 1GB
	if err != nil {
		t.Fatalf("NewThinBackend: %v", err)
	}
	t.Cleanup(func() { _ = tb.Close() })
	if err := tb.Write(1024*1024*1024, []byte("x")); err == nil {
		t.Fatal("write at virtual size boundary should fail")
	}
}

// 块内偏移写 + 跨块边界写：重开后均正确
func TestThinPartialAndCrossBlockWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cross.img")
	tb, err := NewThinBackend(path, 1)
	if err != nil {
		t.Fatalf("NewThinBackend: %v", err)
	}
	// 跨块 0 尾部 → 块 1 头部
	off := int64(thinBlockSize - 512)
	data := bytes.Repeat([]byte{0xCD}, 1024)
	if err := tb.Write(off, data); err != nil {
		t.Fatalf("cross-block Write: %v", err)
	}
	tb.crashClose(t)

	tb2 := reopenThin(t, path)
	got := make([]byte, len(data))
	if err := tb2.Read(off, got); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("cross-block data mismatch after crash-reopen")
	}
}

// fixed 盘 Write → Sync → 重开读回
func TestFixedSyncAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixed.img")
	fb, err := NewFixedBackend(path, 1)
	if err != nil {
		t.Fatalf("NewFixedBackend: %v", err)
	}
	data := []byte("fixed-backend-sync-payload")
	if err := fb.Write(512, data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := fb.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if err := fb.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	fb2, err := OpenFixedBackend(path)
	if err != nil {
		t.Fatalf("OpenFixedBackend: %v", err)
	}
	t.Cleanup(func() { _ = fb2.Close() })
	got := make([]byte, len(data))
	if err := fb2.Read(512, got); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("got %q, want %q", got, data)
	}
}
