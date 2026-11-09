package sdkapi

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// exportTransport 导出/下载专用独立 Transport，不共享连接池
// 避免被心跳/截图等并发请求阻塞
var exportTransport = &http.Transport{
	DialContext: (&net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	MaxIdleConns:        2,
	MaxIdleConnsPerHost: 2,
	MaxConnsPerHost:     2,
	IdleConnTimeout:     90 * time.Second,
	ReadBufferSize:      32 * 1024,
	WriteBufferSize:     32 * 1024,
	ForceAttemptHTTP2:   false,
}

// ExportAndroid 导出安卓云机（POST /androidV2/export，设备端 SSE 进度流）
// exportName: 自定义导出包名（不含 .tar.gz 后缀），空 = 设备端自动生成
func (c *Client) ExportAndroid(name, exportName string) (string, error) {
	return c.ExportAndroidWithProgress(name, exportName, nil)
}

// ExportAndroidWithProgress 导出安卓云机，onEvent 接收设备端 SSE 进度事件：
//
//	config → snapshot（子事件 pause/sync/snapshot/unpause/e2fsck/resize2fs/gzip；
//	非沙盒目录模式为 tar+gzip）→ collect → pack（字节进度子事件）→ complete（含 exportName）
//	任意阶段失败：error（含 msg）。
//
// 长任务：30 分钟独立超时客户端（不用默认 8s，避免中途断开取消设备端 ctx 中断导出）。
func (c *Client) ExportAndroidWithProgress(name, exportName string, onEvent SSEEventFn) (string, error) {
	reqBody := map[string]string{"name": name}
	if exportName != "" {
		reqBody["exportName"] = exportName
	}
	log.Printf("[ExportAndroid] POST /androidV2/export name=%s exportName=%q", name, exportName)

	// SSE 进度流（30 分钟：快照+压缩可达分钟级，避免中途断开取消设备端 ctx）
	lastBody, err := c.doPostSSE("/androidV2/export", reqBody, typedStageAdapter(onEvent), 30*time.Minute)
	if err != nil {
		return "", err
	}
	result, err := stageFromEvent(lastBody)
	if err != nil {
		return "", err
	}
	log.Printf("[ExportAndroid] 完成: name=%s exportName=%q", name, result.ExportName)
	if result.ExportName != "" {
		return result.ExportName, nil
	}

	// complete 事件未带 exportName 时兜底：从设备端备份列表中查找刚产生的备份文件
	log.Printf("[ExportAndroid] complete 事件无 exportName，回退备份列表查找")
	if foundName, err2 := c.findLatestBackupName(name); err2 == nil {
		return foundName, nil
	}
	return "", fmt.Errorf("导出完成但无法确定备份文件名")
}

// findLatestBackupName 从设备端备份列表中查找包含容器名的备份文件名
// 导出后设备端会产生一个备份文件，其文件名包含容器名
func (c *Client) findLatestBackupName(containerName string) (string, error) {
	backups, err := c.ListBackups()
	if err != nil {
		return "", err
	}
	// 优先精确匹配 "容器名.tar.gz"
	suffix := containerName + ".tar.gz"
	for _, b := range backups {
		if strings.HasSuffix(b.Name, suffix) {
			return b.Name, nil
		}
	}
	// fallback: 包含容器名即可（兼容非 .tar.gz 格式）
	for _, b := range backups {
		if strings.Contains(b.Name, containerName) {
			return b.Name, nil
		}
	}
	return "", fmt.Errorf("备份列表中未找到包含 %s 的文件", containerName)
}