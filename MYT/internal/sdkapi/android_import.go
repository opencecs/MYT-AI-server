package sdkapi

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ImportAndroid 从设备本地备份包导入安卓云机（/android/importLocal）
// backupName /backup 列表里的备份压缩包文件名
// indexNum 目标坑位（0=自动分配）
// newName 导入后云机名称（留空用备份包里的原始名）
// start 导入后是否开机
// keepMac 保持原始MAC地址（跨设备迁移不退登）
//
// 响应双兼容（doPostSSE 自带嗅探分流）：新固件返回 SSE 进度流
// （verify/extract/restore/gunzip/e2fsck/resize2fs/pull_image/start/complete/error），
// 旧固件返回同步 JSON {code,data:{name}}。onEvent 在 SSE 模式下逐事件回调。
func (c *Client) ImportAndroid(backupName string, indexNum int, newName string, start bool, keepMac bool, onEvent SSEEventFn) (string, error) {
	payload := map[string]any{"backupName": backupName}
	if indexNum > 0 {
		payload["indexNum"] = indexNum
	}
	if newName != "" {
		payload["name"] = newName
	}
	payload["start"] = start
	payload["keepMac"] = keepMac

	// 导入是长任务（30 分钟超时，避免中途断开取消设备端 ctx）
	lastBody, err := c.doPostSSE("/android/importLocal", payload, typedStageAdapter(onEvent), 30*time.Minute)
	if err != nil {
		return "", err
	}

	// 新固件：SSE 流（doPostSSE 已逐事件回放，末事件应为 complete/error）
	var lastEv SSEResult
	if json.Unmarshal(lastBody, &lastEv) == nil && lastEv.Stage != "" {
		switch lastEv.Stage {
		case "error":
			return "", fmt.Errorf("设备端操作失败: %s", lastEv.Msg)
		case "complete":
			log.Printf("[ImportAndroid] SSE 完成: backup=%s name=%q", backupName, lastEv.Name)
			if lastEv.Name != "" {
				return lastEv.Name, nil
			}
			return backupName, nil
		default:
			return "", fmt.Errorf("导入未完成（最后阶段: %s）", lastEv.Stage)
		}
	}

	// 旧固件：同步 JSON {code, data:{name}}
	log.Printf("[ImportAndroid] JSON 响应: %s", truncateStr(string(lastBody), 500))
	apiResp, err := parseAPIResponse(lastBody)
	if err != nil {
		return "", err
	}
	var result struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(apiResp.Data, &result); err != nil {
		return "", err
	}
	return result.Name, nil
}

// ImportAndroidFromFile 从本地备份文件导入安卓云机（POST /androidV2/import）
// 流式上传（application/octet-stream，绕开 multipart）+ 设备端 SSE 进度流。
//
//	filename: 备份包文件名（不能含 /，取 filePath 的 basename）
//	indexNum: 目标坑位（0=自动分配）
//	name: 导入后名称（newName 非空用 newName，否则 machineName）
//	start/keepMac/keepPorts: 开机/保持 MAC/保持端口
//
// onEvent 接收设备端 SSE 事件：
//
//	upload（每秒一条，P%+current/total/speed）→ verify(3) → extract(5) →
//	restore(10，子事件 gunzip/e2fsck/resize2fs/pull_image) → start(95) → complete(100，含 name)
//	任意阶段失败：error（含 msg）。
//
// 长任务：30 分钟超时（上传+还原可达分钟级，避免中途断开取消设备端 ctx）。
func (c *Client) ImportAndroidFromFile(filePath, machineName, newName string, indexNum int, start, keepMac, keepPorts bool, onEvent SSEEventFn) (string, error) {
	log.Printf("[sdkapi] ImportAndroidFromFile: file=%s, name=%s, indexNum=%d", filePath, machineName, indexNum)

	// 上传前本地 gzip 完整性快检（流式，不载入内存）：坏包当场报错，不浪费上传时间。
	// 在这里单独打开校验，后面 os.Open 的新 reader 才能从头开始上传。
	if strings.HasSuffix(strings.ToLower(filePath), ".gz") {
		if err := VerifyGzipFile(filePath); err != nil {
			return "", fmt.Errorf("本地文件损坏，请重新导出: %w", err)
		}
	}

	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("打开备份文件失败: %w", err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("获取文件信息失败: %w", err)
	}

	query := url.Values{}
	query.Set("filename", filepath.Base(filePath))
	query.Set("indexNum", fmt.Sprintf("%d", indexNum)) // 0 = 自动分配坑位
	if newName != "" {
		query.Set("name", newName)
	} else {
		query.Set("name", machineName)
	}
	query.Set("start", fmt.Sprintf("%v", start))
	query.Set("keepMac", fmt.Sprintf("%v", keepMac))
	query.Set("keepPorts", fmt.Sprintf("%v", keepPorts))

	req, err := http.NewRequestWithContext(c.requestContext(), "POST", c.buildURL("/androidV2/import?"+query.Encode()), f)
	if err != nil {
		return "", fmt.Errorf("创建导入请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = fi.Size()
	c.setAuth(req)

	client := &http.Client{Timeout: 30 * time.Minute, Transport: DeviceTransport}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("导入请求失败: %s", friendlyNetError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("认证失败，请检查设备密码")
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("导入失败: HTTP %d, %s", resp.StatusCode, string(respBody))
	}

	result, err := readSSEResponse(resp, onEvent)
	if err != nil {
		return "", err
	}
	log.Printf("[sdkapi] ImportAndroidFromFile 完成: name=%q", result.Name)
	if result.Name != "" {
		return result.Name, nil
	}
	return machineName, nil
}