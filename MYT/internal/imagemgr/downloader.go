package imagemgr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ProgressReader 带进度回调的 io.Reader 包装器
type ProgressReader struct {
	Reader    io.Reader
	Size      int64
	ReadBytes int64
	Callback  func(float64)

	// 节流：避免高频回调导致事件风暴
	lastPct    float64 // 上次回调时的百分比
	lastBytes  int64   // 上次回调时的字节数（未知大小时用）
	lastCall   int64   // 上次回调时间（Unix毫秒）
}

const progressMinIntervalMs = 500 // 最少间隔 500ms 发一次进度
const progressMinDelta    = 0.5   // 百分比最少变化 0.5% 才发

func (pr *ProgressReader) Read(p []byte) (n int, err error) {
	n, err = pr.Reader.Read(p)
	if n > 0 {
		pr.ReadBytes += int64(n)
		if pr.Callback != nil {
			now := time.Now().UnixMilli()
			if pr.Size > 0 {
				pct := float64(pr.ReadBytes) / float64(pr.Size) * 100
				// 百分比变化 <0.5% 且间隔 <500ms → 跳过
				if pct-pr.lastPct < progressMinDelta && now-pr.lastCall < progressMinIntervalMs {
					return n, err
				}
				pr.lastPct = pct
				pr.lastCall = now
				pr.Callback(pct)
			} else {
				mb := float64(pr.ReadBytes) / 1024 / 1024
				mbLast := float64(pr.lastBytes) / 1024 / 1024
				// 变化 <1MB 且间隔 <500ms → 跳过
				if mb-mbLast < 1.0 && now-pr.lastCall < progressMinIntervalMs {
					return n, err
				}
				pr.lastBytes = pr.ReadBytes
				pr.lastCall = now
				pr.Callback(-mb)
			}
		}
	}
	return n, err
}

// DownloadManager 管理下载任务
type DownloadManager struct {
	mu      sync.Mutex
	cancels map[string]func() // mirrorID -> cancel
}

func NewDownloadManager() *DownloadManager {
	return &DownloadManager{
		cancels: make(map[string]func()),
	}
}

func (dm *DownloadManager) SetCancel(mirrorID string, cancel func()) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.cancels[mirrorID] = cancel
}

func (dm *DownloadManager) Cancel(mirrorID string) bool {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	if cancel, ok := dm.cancels[mirrorID]; ok {
		cancel()
		delete(dm.cancels, mirrorID)
		return true
	}
	return false
}

// DownloadImage 下载镜像到本地，通过 onProgress 回调报告进度
// 下载到 .tar.downloading 后缀文件，元数据写入伴生 .meta.json，完成后由调用者重命名为 .tar
func DownloadImage(imageURL, imagesDir string, meta *SavedImage, onProgress func(stage string, progress float64), cancelCh <-chan struct{}) error {
	safeName := SanitizeFileName(imageURL)
	downloadingPath := filepath.Join(imagesDir, safeName+".tar.downloading")

	// 写入伴生 .meta.json（下载开始时就写，用于标识下载意图）
	metaPath := filepath.Join(imagesDir, safeName+".meta.json")
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化元数据失败: %w", err)
	}
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		return fmt.Errorf("写入元数据文件失败: %w", err)
	}

	if !strings.HasPrefix(imageURL, "http://") && !strings.HasPrefix(imageURL, "https://") {
		return downloadDockerImage(imageURL, downloadingPath, onProgress, cancelCh)
	}
	return downloadHTTPImage(imageURL, downloadingPath, onProgress, cancelCh)
}

func downloadHTTPImage(imageURL, downloadingPath string, onProgress func(string, float64), cancelCh <-chan struct{}) error {
	onProgress("downloading", 0)

	ctx, ctxCancel := context.WithCancel(context.Background())
	cancelDone := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] goroutine recovered: %v", r)
			}
		}()
		select {
		case <-cancelCh:
			ctxCancel()
		case <-ctx.Done():
		}
		close(cancelDone)
	}()
	defer func() {
		ctxCancel()
		<-cancelDone
	}()

	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("下载失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(downloadingPath), 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	out, err := os.Create(downloadingPath)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer out.Close()

	fileSize := resp.ContentLength
	pr := &ProgressReader{
		Reader: resp.Body,
		Size:   fileSize,
		Callback: func(progress float64) {
			if fileSize > 0 {
				mapped := progress * 0.95
				if mapped > 95 {
					mapped = 95
				}
				onProgress("downloading", mapped)
			} else {
				onProgress("downloading", progress)
			}
		},
	}

	buf := make([]byte, 32*1024)
	for {
		select {
		case <-cancelCh:
			out.Close()
			os.Remove(downloadingPath)
			return fmt.Errorf("下载已取消")
		default:
		}
		n, readErr := pr.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			select {
			case <-cancelCh:
				out.Close()
				os.Remove(downloadingPath)
				return fmt.Errorf("下载已取消")
			default:
				return readErr
			}
		}
	}

	onProgress("downloading", 100)
	return nil
}

func downloadDockerImage(tag, outputTar string, onProgress func(string, float64), cancelCh <-chan struct{}) error {
	imageTag := parseImageTag(tag)

	ctx, ctxCancel := context.WithCancel(context.Background())
	cancelDone := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] goroutine recovered: %v", r)
			}
		}()
		select {
		case <-cancelCh:
			ctxCancel()
		case <-ctx.Done():
		}
		close(cancelDone)
	}()
	defer func() {
		ctxCancel()
		<-cancelDone
	}()

	if !strings.Contains(imageTag.Host, "aliyuncs.com") {
		if err := fetchHash(imageTag); err != nil {
			log.Printf("fetchHash 失败(可忽略): %v", err)
		}
	}

	manifest, err := fetchManifest(ctx, imageTag)
	if err != nil {
		return fmt.Errorf("获取 manifest 失败: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(outputTar), 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	tarFile, err := os.Create(outputTar)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer tarFile.Close()

	tw := newTarWriterWithCancel(tarFile, cancelCh, outputTar)
	defer tw.Close()

	onProgress("downloading", 5)

	configFileName := strings.TrimPrefix(manifest.Config.Digest, "sha256:") + ".json"
	if err := downloadAndAddToTar(ctx, tw, imageTag, manifest.Config, configFileName, func(progress float64) {
		total := 5 + (progress/100)*10 // 5% - 15%
		onProgress("downloading", total)
	}); err != nil {
		return fmt.Errorf("下载 config 失败: %w", err)
	}

	layerCount := len(manifest.Layers)
	layers := make([]string, 0, layerCount)
	for i, layer := range manifest.Layers {
		select {
		case <-cancelCh:
			os.Remove(outputTar)
			return fmt.Errorf("下载已取消")
		default:
		}

		layerFileName := strings.TrimPrefix(layer.Digest, "sha256:") + "/layer.tar"
		layers = append(layers, layerFileName)

		layerIndex := i
		if err := downloadAndAddToTar(ctx, tw, imageTag, layer, layerFileName, func(progress float64) {
			startPct := 15.0 + float64(layerIndex)*(80.0/float64(layerCount))
			layerPct := (progress / 100) * 80.0 / float64(layerCount)
			total := startPct + layerPct
			if total > 95 {
				total = 95
			}
			onProgress("downloading", total)
		}); err != nil {
			return fmt.Errorf("下载 layer %d 失败: %w", i, err)
		}
	}

	// 写入 manifest.json
	manifests := []ManifestFile{{
		Config:   configFileName,
		RepoTags: []string{tag},
		Layers:   layers,
	}}
	data, err := json.Marshal(manifests)
	if err != nil {
		return err
	}
	if err := addTarEntry(tw, "manifest.json", data); err != nil {
		return err
	}

	onProgress("downloading", 100)
	return nil
}

func SanitizeFileName(url string) string {
	name := url
	name = strings.TrimPrefix(name, "http://")
	name = strings.TrimPrefix(name, "https://")
	replacer := strings.NewReplacer(":", "_", "?", "_", "&", "_", "=", "_", "#", "_", "/", "_")
	return replacer.Replace(name)
}
