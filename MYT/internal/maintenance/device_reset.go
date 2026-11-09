package maintenance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// devicePost 向设备 SDK API 发送 POST 请求
func devicePost(deviceIP, path string, payload any) ([]byte, error) {
	ip := extractPureIP(deviceIP)
	url := fmt.Sprintf("http://%s:8000%s", ip, path)

	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("序列化请求失败: %w", err)
		}
		body = bytes.NewReader(b)
	} else {
		body = bytes.NewReader([]byte("{}"))
	}

	client := &http.Client{Timeout: 180 * time.Second}
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 %s 失败: %w", path, err)
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// ResetDevice 清空设备磁盘（调用 /server/device/reset）
func ResetDevice(deviceIP string) map[string]interface{} {
	ip := extractPureIP(deviceIP)
	log.Printf("[维护] 开始清空设备磁盘: %s", ip)

	body, err := devicePost(ip, "/server/device/reset", nil)
	if err != nil {
		// 接口可能超时（设备重启中），也算指令已发送
		log.Printf("[维护] 清空磁盘请求: %v（设备可能正在重启）", err)
		return map[string]interface{}{
			"success": true,
			"message": "清空磁盘指令已发送，设备将自动重启",
		}
	}

	var resp struct {
		CodeID int    `json:"code_id"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		log.Printf("[维护] 清空磁盘响应: %s", string(body))
		return map[string]interface{}{
			"success": true,
			"message": "清空磁盘指令已发送",
		}
	}

	if resp.CodeID != 0 && resp.CodeID != 200 {
		return errResult("清空磁盘失败: %s", resp.Msg)
	}

	return map[string]interface{}{
		"success": true,
		"message": "清空磁盘完成",
	}
}

// RebootDevice 重启设备（调用 /server/device/reboot）
func RebootDevice(deviceIP string) map[string]interface{} {
	ip := extractPureIP(deviceIP)
	log.Printf("[维护] 开始重启设备: %s", ip)

	body, err := devicePost(ip, "/server/device/reboot", nil)
	if err != nil {
		// 接口可能超时（设备重启中），也算指令已发送
		log.Printf("[维护] 重启设备请求: %v（设备可能正在重启）", err)
		return map[string]interface{}{
			"success": true,
			"message": "重启指令已发送，设备将在约2分钟后恢复",
		}
	}

	var resp struct {
		CodeID int    `json:"code_id"`
		Msg    string `json:"msg"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		log.Printf("[维护] 重启设备响应: %s", string(body))
		return map[string]interface{}{
			"success": true,
			"message": "重启指令已发送",
		}
	}

	if resp.CodeID != 0 && resp.CodeID != 200 {
		return errResult("重启设备失败: %s", resp.Msg)
	}

	return map[string]interface{}{
		"success": true,
		"message": "重启指令已发送，设备将在约2分钟后恢复",
	}
}

// PruneDeviceImages 清理设备未使用镜像（调用 /android/pruneImages）
func PruneDeviceImages(deviceIP string) map[string]interface{} {
	ip := extractPureIP(deviceIP)
	log.Printf("[维护] 开始清理设备未使用镜像: %s", ip)

	body, err := devicePost(ip, "/android/pruneImages", nil)
	if err != nil {
		return errResult("清理镜像请求失败: %v", err)
	}

	var resp struct {
		CodeID int `json:"code_id"`
		Data   struct {
			PruneCount   int    `json:"pruneCount"`
			ReleaseSpace string `json:"releaseSpace"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return errResult("清理镜像响应解析失败: %v", err)
	}

	return map[string]interface{}{
		"success":      true,
		"message":      fmt.Sprintf("清理完成：%d 个镜像，释放 %s", resp.Data.PruneCount, resp.Data.ReleaseSpace),
		"pruneCount":   resp.Data.PruneCount,
		"releaseSpace": resp.Data.ReleaseSpace,
	}
}
