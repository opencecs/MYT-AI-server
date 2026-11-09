package imagemgr

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
)

// cancelableTarWriter 支持取消的 tar writer
type cancelableTarWriter struct {
	tw       *tar.Writer
	cancelCh <-chan struct{}
	filePath string // 用于取消时删除
}

func newTarWriterWithCancel(w io.Writer, cancelCh <-chan struct{}, filePath string) *cancelableTarWriter {
	return &cancelableTarWriter{
		tw:       tar.NewWriter(w),
		cancelCh: cancelCh,
		filePath: filePath,
	}
}

func (c *cancelableTarWriter) WriteHeader(name string, size int64) error {
	header := &tar.Header{
		Name: name,
		Size: size,
		Mode: 0600,
	}
	return c.tw.WriteHeader(header)
}

func (c *cancelableTarWriter) Write(p []byte) (n int, err error) {
	select {
	case <-c.cancelCh:
		os.Remove(c.filePath)
		return 0, fmt.Errorf("下载已取消")
	default:
	}
	return c.tw.Write(p)
}

func (c *cancelableTarWriter) Close() error {
	return c.tw.Close()
}

func addTarEntry(tw *cancelableTarWriter, name string, data []byte) error {
	if err := tw.WriteHeader(name, int64(len(data))); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}
