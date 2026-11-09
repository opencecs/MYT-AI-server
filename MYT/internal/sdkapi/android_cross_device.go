package sdkapi

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"
)

// CrossDeviceMove 跨设备移动云机：从源设备导出→下载→流式上传到目标设备
// 整个过程在 Go 侧以流式管道完成，不经过前端内存。
// srcClient/dstClient 由调用方通过 newClientWithTimeout 创建（自动适配 local/public 模式）。
// onStage 回调用于推送阶段进度（stage: preparing/exporting/transferring/cleanup）
func CrossDeviceMove(ctx context.Context, srcClient, dstClient *Client, androidName, imageURL string, targetSlot int, onStage func(stage, message string)) error {
	srcClient.SetContext(ctx)
	dstClient.SetContext(ctx)

	// 0. 清理目标设备上的同名残留容器（上次失败可能留下）
	if err := dstClient.DeleteAndroid(androidName); err != nil {
		log.Printf("[CrossDeviceMove] 清理目标设备残留容器（可忽略）: %v", err)
	}

	// 1. 检查目标设备镜像，不存在则拉取
	if onStage != nil {
		onStage("preparing", "准备目标设备")
	}
	imageExists, err := dstClient.checkImageExists(imageURL)
	if err != nil {
		return fmt.Errorf("检查目标设备镜像失败: %w", err)
	}
	if !imageExists {
		if err := dstClient.PullImage(imageURL); err != nil {
			return fmt.Errorf("拉取镜像到目标设备失败: %w", err)
		}
	}

	// 2. 在源设备导出
	if onStage != nil {
		onStage("exporting", "正在导出实例")
	}
	exportName, err := srcClient.ExportAndroidWithProgress(androidName, "", nil)
	if err != nil {
		return fmt.Errorf("导出失败: %w", err)
	}
	log.Printf("[CrossDeviceMove] 导出完成: %s", exportName)

	// 3. 从源设备下载，流式管道到目标设备上传
	if onStage != nil {
		onStage("transferring", "正在传输数据")
	}
	// 从源设备下载备份文件（buildURL/setAuth 自动适配 local/public 模式）
	downloadURL := srcClient.buildURL("/backup/download?name=" + url.QueryEscape(exportName))
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return fmt.Errorf("创建下载请求失败: %w", err)
	}
	srcClient.setAuth(req)

	dlClient := &http.Client{Timeout: 30 * time.Minute, Transport: DeviceTransport}
	dlResp, err := dlClient.Do(req)
	if err != nil {
		return fmt.Errorf("下载备份失败: %w", err)
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(dlResp.Body, 4096))
		return fmt.Errorf("下载备份失败: HTTP %d, %s", dlResp.StatusCode, string(body))
	}

	// 4. 流式上传到目标设备（octet-stream 裸流 + URL query 传参 + SSE 响应）
	query := url.Values{}
	query.Set("filename", exportName)
	query.Set("name", androidName)
	if targetSlot > 0 {
		query.Set("indexNum", fmt.Sprintf("%d", targetSlot))
	}

	uploadURL := dstClient.buildURL("/android/import?" + query.Encode())
	uploadReq, err := http.NewRequestWithContext(ctx, "POST", uploadURL, dlResp.Body)
	if err != nil {
		return fmt.Errorf("创建上传请求失败: %w", err)
	}
	uploadReq.Header.Set("Content-Type", "application/octet-stream")
	if dlResp.ContentLength > 0 {
		uploadReq.ContentLength = dlResp.ContentLength
	}
	dstClient.setAuth(uploadReq)

	ulClient := &http.Client{Timeout: 30 * time.Minute, Transport: DeviceTransport}
	uploadResp, err := ulClient.Do(uploadReq)
	if err != nil {
		return fmt.Errorf("上传到目标设备失败: %w", err)
	}
	defer uploadResp.Body.Close()

	if uploadResp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("认证失败，请检查设备密码")
	}
	if uploadResp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(uploadResp.Body, 4096))
		return fmt.Errorf("导入失败: HTTP %d, %s", uploadResp.StatusCode, string(respBody))
	}

	// SSE 响应：逐事件推进度（upload/verify/extract/restore/start/complete），末事件 complete/error
	result, err := readSSEResponse(uploadResp, func(ev SSEResult) {
		if onStage != nil && ev.Stage != "" {
			onStage("importing", fmt.Sprintf("%s %d%% %s", ev.Stage, ev.Progress, ev.Msg))
		}
	})
	if err != nil {
		return fmt.Errorf("导入失败: %w", err)
	}
	log.Printf("[CrossDeviceMove] 导入完成: name=%q", result.Name)

	log.Printf("[CrossDeviceMove] 跨设备移动完成")

	// 5. 删除源设备上的原容器（移动=复制后删除源）
	if onStage != nil {
		onStage("cleanup", "正在清理源设备")
	}
	if err := srcClient.DeleteAndroid(androidName); err != nil {
		log.Printf("[CrossDeviceMove] 删除源设备容器失败（移动已完成，需手动清理）: %v", err)
	}

	return nil
}

// checkImageExists 检查设备上是否存在指定镜像
func (c *Client) checkImageExists(imageURL string) (bool, error) {
	images, err := c.ListImages()
	if err != nil {
		return false, err
	}
	for _, img := range images {
		if img.ImageURL == imageURL {
			return true, nil
		}
	}
	return false, nil
}
