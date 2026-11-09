package maintenance

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

type mytPanelUpdateResponse struct {
	CodeID int    `json:"code_id"`
	Msg    string `json:"msg"`
	Data   struct {
		DownloadURL   string `json:"download_url"`
		LatestVersion string `json:"latest_version"`
		SHA256        string `json:"sha256"`
	} `json:"data"`
}

// panelDeviceType 更新 check 接口的设备型号参数（默认 r1s；setup 按
// DeployMode 注入：v2 模式 → r1s_v2）。internal 不能 import main，走包级注入
// （与 SetV2HostIPResolver 同模式）。
var panelDeviceType = "r1s"

// SetPanelDeviceType 注入更新 check 的设备型号（main 包 setup 调用）。
func SetPanelDeviceType(t string) {
	panelDeviceType = t
}

func checkMytPanelUpdate() (downloadURL, latestVersion, sha256 string, err error) {
	apiURL := fmt.Sprintf("https://newapi.moyunteng.com/api/v1/update/check?device=%s&arch=arm64&version=0", panelDeviceType)
	log.Printf("[维护] 查询最新版本: %s", apiURL)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return "", "", "", fmt.Errorf("请求更新接口失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", fmt.Errorf("读取响应失败: %w", err)
	}

	var result mytPanelUpdateResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", "", fmt.Errorf("解析响应失败: %w", err)
	}

	if result.CodeID != 200 || result.Data.DownloadURL == "" {
		return "", "", "", fmt.Errorf("未找到可用更新 (code=%d, msg=%s)", result.CodeID, result.Msg)
	}

	return result.Data.DownloadURL, result.Data.LatestVersion, result.Data.SHA256, nil
}
