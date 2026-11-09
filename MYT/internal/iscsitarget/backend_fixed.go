package iscsitarget

import (
	"fmt"
	"os"
)

// FixedBackend 固定大小的磁盘映像文件
type FixedBackend struct {
	file *os.File
	size int64
}

func NewFixedBackend(path string, sizeGB int64) (*FixedBackend, error) {
	size := sizeGB * 1024 * 1024 * 1024
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("create image file: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("stat image file: %w", err)
	}
	if info.Size() < size {
		if err := f.Truncate(size); err != nil {
			f.Close()
			return nil, fmt.Errorf("truncate image file: %w", err)
		}
	}
	return &FixedBackend{file: f, size: size}, nil
}

func OpenFixedBackend(path string) (*FixedBackend, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open image file: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("stat image file: %w", err)
	}
	return &FixedBackend{file: f, size: info.Size()}, nil
}

func (b *FixedBackend) Read(at int64, buf []byte) error {
	_, err := b.file.ReadAt(buf, at)
	return err
}

func (b *FixedBackend) Write(at int64, buf []byte) error {
	_, err := b.file.WriteAt(buf, at)
	return err
}

func (b *FixedBackend) Size() int64  { return b.size }
func (b *FixedBackend) Sync() error { return b.file.Sync() }
func (b *FixedBackend) Close() error { return b.file.Close() }

// Truncate 扩大 FixedBackend 的大小（只能扩不能缩）
func (b *FixedBackend) Truncate(newSize int64) error {
	if newSize <= b.size {
		return fmt.Errorf("new size %d must be larger than current %d", newSize, b.size)
	}
	if err := b.file.Truncate(newSize); err != nil {
		return fmt.Errorf("truncate image file: %w", err)
	}
	b.size = newSize
	return nil
}
