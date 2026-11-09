package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRotatingWriterAppendsWithoutRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.log")
	w := NewRotatingWriter(path, 0, 3) // maxBytes=0：不轮转
	defer w.Close()

	if _, err := w.Write([]byte("aaa")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := w.Write([]byte("bbb")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "aaabbb" {
		t.Errorf("content = %q, want %q", data, "aaabbb")
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Error("不轮转模式下不该产生 .1 备份")
	}
}

func TestRotatingWriterRotatesAtLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.log")
	w := NewRotatingWriter(path, 10, 3)
	defer w.Close()

	if _, err := w.Write([]byte("11111")); err != nil { // size=5
		t.Fatalf("Write: %v", err)
	}
	if _, err := w.Write([]byte("22222")); err != nil { // 5+5=10 未超
		t.Fatalf("Write: %v", err)
	}
	if _, err := w.Write([]byte("33333")); err != nil { // 10+5>10 → 轮转
		t.Fatalf("Write: %v", err)
	}

	cur, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(cur) != "33333" {
		t.Errorf("当前日志 = %q, want %q", cur, "33333")
	}

	b1, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("备份 .1 不存在: %v", err)
	}
	if string(b1) != "1111122222" {
		t.Errorf(".1 = %q, want %q", b1, "1111122222")
	}
}

func TestRotatingWriterKeepsAtMostBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.log")
	w := NewRotatingWriter(path, 4, 2) // 最多保留 2 份备份
	defer w.Close()

	for i := 0; i < 6; i++ {
		if _, err := w.Write([]byte("abcd")); err != nil {
			t.Fatalf("Write #%d: %v", i, err)
		}
	}

	if _, err := os.Stat(path + ".2"); err != nil {
		t.Errorf(".2 应存在: %v", err)
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Error(".3 不该存在（超出 backups 上限）")
	}
}

func TestRotatingWriterReopenAppendsNotTruncates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.log")

	w1 := NewRotatingWriter(path, 100, 1)
	if _, err := w1.Write([]byte("first\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// 模拟进程重启：新实例必须续写而不是覆盖（否则每次启动丢历史）
	w2 := NewRotatingWriter(path, 100, 1)
	if _, err := w2.Write([]byte("second\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	w2.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "first\nsecond\n" {
		t.Errorf("重开后应追加，得到 %q", data)
	}
}

func TestRotatingWriterOpenFailsOnMissingDir(t *testing.T) {
	// 目录不存在时必须报错，让调用方保留默认 stderr 输出（不能让日志静默消失）
	w := NewRotatingWriter(filepath.Join(t.TempDir(), "nodir", "t.log"), 100, 1)
	defer w.Close()

	if err := w.Open(); err == nil {
		t.Error("Open() 对不存在的目录应返回错误")
	}
}
