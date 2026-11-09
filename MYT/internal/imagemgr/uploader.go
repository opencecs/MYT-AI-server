package imagemgr

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/sdkapi"
)

// UploadManager 管理上传任务
type UploadManager struct {
	mu      sync.Mutex
	cancels map[string]map[string]func() // mirrorID -> deviceIP -> cancel
}

func NewUploadManager() *UploadManager {
	return &UploadManager{
		cancels: make(map[string]map[string]func()),
	}
}

func (um *UploadManager) setCancel(mirrorID, deviceIP string, cancel func()) {
	um.mu.Lock()
	defer um.mu.Unlock()
	if um.cancels[mirrorID] == nil {
		um.cancels[mirrorID] = make(map[string]func())
	}
	um.cancels[mirrorID][deviceIP] = cancel
}

func (um *UploadManager) removeCancel(mirrorID, deviceIP string) {
	um.mu.Lock()
	defer um.mu.Unlock()
	if devices, ok := um.cancels[mirrorID]; ok {
		delete(devices, deviceIP)
		if len(devices) == 0 {
			delete(um.cancels, mirrorID)
		}
	}
}

func (um *UploadManager) CancelDevice(mirrorID, deviceIP string) bool {
	um.mu.Lock()
	defer um.mu.Unlock()
	if devices, ok := um.cancels[mirrorID]; ok {
		if cancel, ok := devices[deviceIP]; ok {
			cancel()
			delete(devices, deviceIP)
			return true
		}
	}
	return false
}

func (um *UploadManager) CancelAll(mirrorID string) {
	um.mu.Lock()
	defer um.mu.Unlock()
	if devices, ok := um.cancels[mirrorID]; ok {
		for _, cancel := range devices {
			cancel()
		}
		delete(um.cancels, mirrorID)
	}
}

// UploadToDevices 并发上传镜像到多台设备
func UploadToDevices(
	mirrorID, localPath string, deviceIPs []string,
	onProgress func(deviceIP string, progress float64),
	onComplete func(deviceIP string, success bool, err string),
	uploadMgr *UploadManager,
	getDevicePassword func(deviceIP string) string,
) {
	var wg sync.WaitGroup

	for _, ip := range deviceIPs {
		wg.Add(1)
		go func(deviceIP string) {
			defer wg.Done()

			success, errMsg := uploadToDevice(mirrorID, localPath, deviceIP, onProgress, uploadMgr, getDevicePassword)
			onComplete(deviceIP, success, errMsg)
		}(ip)
	}

	wg.Wait()
}

func uploadToDevice(mirrorID, localPath, deviceIP string, onProgress func(string, float64), uploadMgr *UploadManager, getDevicePassword func(string) string) (bool, string) {
	ctx, ctxCancel := context.WithCancel(context.Background())
	uploadMgr.setCancel(mirrorID, deviceIP, ctxCancel)
	defer uploadMgr.removeCancel(mirrorID, deviceIP)

	file, err := os.Open(localPath)
	if err != nil {
		return false, fmt.Sprintf("打开文件失败: %v", err)
	}
	defer file.Close()

	fi, err := file.Stat()
	if err != nil {
		return false, fmt.Sprintf("获取文件信息失败: %v", err)
	}
	fileSize := fi.Size()

	pr := &ProgressReader{
		Reader: file,
		Size:   fileSize,
		Callback: func(progress float64) {
			select {
			case <-ctx.Done():
				return
			default:
			}
			onProgress(deviceIP, progress)
		},
	}

	url := fmt.Sprintf("http://%s:%d/docker/images/load", deviceIP, sdkapi.DevicePort)
	req, err := http.NewRequestWithContext(ctx, "POST", url, pr)
	if err != nil {
		return false, fmt.Sprintf("创建请求失败: %v", err)
	}
	req.SetBasicAuth(sdkapi.DeviceUser, getDevicePassword(deviceIP))
	req.Header.Set("Content-Type", "application/x-tar")

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		select {
		case <-ctx.Done():
			return false, "上传已取消"
		default:
			return false, fmt.Sprintf("上传失败: %v", err)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Sprintf("上传失败: HTTP %d, %s", resp.StatusCode, string(body))
	}

	return true, ""
}
