package container

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/util"
)

// --- P2 方法 ---

// GetInfo 获取容器信息
func (c *Client) GetInfo() (map[string]interface{}, error) {
	return c.get("/info")
}

// GetVersion 获取版本
func (c *Client) GetVersion() (string, error) {
	result, err := c.get("/queryversion")
	if err != nil {
		return "", err
	}
	if msg, ok := result["msg"].(string); ok {
		return msg, nil
	}
	return "", nil
}

// SetADBRoot 开关 ADB root
func (c *Client) SetADBRoot(cmd int) error {
	path := "/adb"
	if cmd > 0 {
		path = fmt.Sprintf("/adb?cmd=%d", cmd)
	}
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("ADB操作失败: %v", result)
	}
	return nil
}

// SetLanguage 设置语言和国家
func (c *Client) SetLanguage(language, country string) error {
	path := fmt.Sprintf("/modifydev?cmd=13&language=%s&country=%s", language, country)
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("设置语言失败: %v", result)
	}
	return nil
}

// SetIPLocate IP定位
func (c *Client) SetIPLocate(ip, language string) error {
	path := "/modifydev?cmd=11"
	if ip != "" {
		path += "&ip=" + ip
	}
	if language != "" {
		path += "&launage=" + language
	}
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("IP定位失败: %v", result)
	}
	return nil
}

// SetBackgroundApp 后台保活
func (c *Client) SetBackgroundApp(cmd int, pkg string) (map[string]interface{}, error) {
	path := fmt.Sprintf("/background?cmd=%d", cmd)
	if pkg != "" {
		path += "&package=" + pkg
	}
	return c.get(path)
}

// SetDisableKey 屏蔽按键
func (c *Client) SetDisableKey(value int) error {
	path := fmt.Sprintf("/disablekey?value=%d", value)
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("屏蔽按键失败: %v", result)
	}
	return nil
}

// SetCamera 虚拟摄像头
func (c *Client) SetCamera(cmd, camType, path string) error {
	url := c.buildURL(fmt.Sprintf("/camera?cmd=%s", cmd))
	if path != "" {
		url += "&path=" + path
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}
	c.setAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("摄像头操作失败: %s", util.FriendlyNetError(err))
	}
	defer resp.Body.Close()
	return nil
}

// ReceiveSMS 模拟接收短信
func (c *Client) ReceiveSMS(address, body string) error {
	payload, _ := json.Marshal(map[string]string{"address": address, "body": body})
	_, err := c.post("/sms?cmd=4", bytes.NewReader(payload))
	return err
}

// AddContact 添加联系人
func (c *Client) AddContact(contactsJSON string) error {
	path := fmt.Sprintf("/addcontact?data=%s", contactsJSON)
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("添加联系人失败: %v", result)
	}
	return nil
}

// AddCallLog 添加通话记录
func (c *Client) AddCallLog(number string, callType, duration int) error {
	path := fmt.Sprintf("/callog?number=%s&type=%d&duration=%d", number, callType, duration)
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("添加通话记录失败: %v", result)
	}
	return nil
}

// SetADID 设置/生成 Google ADID
func (c *Client) SetADID(cmd int, adid string) error {
	path := fmt.Sprintf("/adid?cmd=%d", cmd)
	if adid != "" {
		path += "&adid=" + adid
	}
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("设置ADID失败: %v", result)
	}
	return nil
}

// UpdateFingerprint 更新设备指纹
func (c *Client) UpdateFingerprint(dataJSON string) error {
	path := fmt.Sprintf("/modifydev?cmd=7&data=%s", dataJSON)
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("更新指纹失败: %v", result)
	}
	return nil
}

// SetShake 设置摇一摇
func (c *Client) SetShake(enabled bool) error {
	val := 0
	if enabled {
		val = 1
	}
	path := fmt.Sprintf("/modifydev?cmd=17&shake=%d", val)
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("设置摇一摇失败: %v", result)
	}
	return nil
}

// SetAppPermission 设置应用权限
func (c *Client) SetAppPermission(pkg string) error {
	path := fmt.Sprintf("/modifydev?cmd=18&pkg=%s", pkg)
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("设置应用权限失败: %v", result)
	}
	return nil
}

// GetRootList 获取Root授权列表
func (c *Client) GetRootList() (map[string]interface{}, error) {
	return c.get("/modifydev?cmd=10&action=list")
}

// GrantRoot 为指定包名开启Root
func (c *Client) GrantRoot(pkg string) error {
	path := fmt.Sprintf("/modifydev?cmd=10&pkg=%s&root=true", pkg)
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("授权Root失败: %v", result)
	}
	return nil
}

// SetCameraSource 设置虚拟摄像头源
func (c *Client) SetCameraSource(camType, path string, resolution int) error {
	url := c.buildURL("/modifydev?cmd=4")
	if camType != "" {
		url += "&type=" + camType
	}
	if path != "" {
		url += "&path=" + path
	}
	if resolution > 0 {
		url += fmt.Sprintf("&resolution=%d", resolution)
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}
	c.setAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("设置摄像头源失败: %s", util.FriendlyNetError(err))
	}
	defer resp.Body.Close()
	return nil
}

// GetBootStartList 获取开机启动列表
func (c *Client) GetBootStartList() (map[string]interface{}, error) {
	return c.get("/appbootstart?cmd=1")
}

// SetBootStart 设置开机启动
func (c *Client) SetBootStart(packages []string) error {
	payload, _ := json.Marshal(packages)
	_, err := c.post("/appbootstart?cmd=2", bytes.NewReader(payload))
	return err
}

// SetProxyFilter 设置代理域名过滤
func (c *Client) SetProxyFilter(domains []string) error {
	payload, _ := json.Marshal(domains)
	_, err := c.post("/proxy?cmd=4", bytes.NewReader(payload))
	return err
}

// BackupApp 导出app信息
func (c *Client) BackupApp(pkg, saveTo string) error {
	path := fmt.Sprintf("/backrestore?cmd=backup&pkg=%s&saveto=%s", pkg, saveTo)
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("备份失败: %v", result)
	}
	return nil
}

// RestoreApp 导入app信息
func (c *Client) RestoreApp(backupPath string) error {
	path := fmt.Sprintf("/backrestore?cmd=recovery&backuppath=%s", backupPath)
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("恢复失败: %v", result)
	}
	return nil
}

// RefreshLocation 刷新定位
func (c *Client) RefreshLocation() error {
	result, err := c.get("/task")
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("刷新定位失败: %v", result)
	}
	return nil
}

// UploadFromURL 通过URL下载文件到安卓设备，返回设备端文件路径
func (c *Client) UploadFromURL(fileURL string) (string, error) {
	url := c.buildURL(fmt.Sprintf("/?task=upload&file=%s", fileURL))
	log.Printf("[container] UploadFromURL: %s", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("create request failed: %w", err)
	}
	c.setAuth(req)
	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("URL上传失败: %s", util.FriendlyNetError(err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取上传响应失败: %w", err)
	}

	// 尝试解析 JSON 响应
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err == nil {
		// 如果响应中有 data.path 或 data 字段，优先使用
		if data, ok := result["data"].(map[string]interface{}); ok {
			if p, ok := data["path"].(string); ok && p != "" {
				return p, nil
			}
			if p, ok := data["file"].(string); ok && p != "" {
				return p, nil
			}
		}
		if !isSuccess(result) {
			return "", fmt.Errorf("URL上传失败: %v", result)
		}
	}

	// MYTOS 上传成功后文件通常在 /sdcard/Download/ 目录
	// 从 URL 提取文件名
	parts := strings.Split(fileURL, "/")
	fileName := parts[len(parts)-1]
	if idx := strings.Index(fileName, "?"); idx > 0 {
		fileName = fileName[:idx]
	}
	return "/sdcard/Download/" + fileName, nil
}

// InstallApk 上传单个 APK 文件并安装（POST /installapk）
func (c *Client) InstallApk(apkPath string) error {
	rawURL := c.buildURL("/installapk")
	file, err := os.Open(apkPath)
	if err != nil {
		return fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filepath.Base(apkPath))
	if err != nil {
		return fmt.Errorf("创建表单失败: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}
	writer.Close()

	longClient := &http.Client{Timeout: 7200 * time.Second}
	req, err := http.NewRequest("POST", rawURL, body)
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	c.setAuth(req)
	resp, err := longClient.Do(req)
	if err != nil {
		return fmt.Errorf("安装APK失败: %s", util.FriendlyNetError(err))
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil // 安装成功但响应非JSON
	}
	if !isSuccess(result) {
		return fmt.Errorf("安装APK失败: %v", result)
	}
	return nil
}

// InstallApks 批量安装 APK/XAPK（ZIP压缩包）
// 流式上传（io.Pipe + progressReader，避免大包整体读入内存），onProgress 透出上传字节进度
func (c *Client) InstallApks(zipPath string, onProgress func(uploaded, total int64)) error {
	rawURL := c.buildURL("/installapks")
	file, err := os.Open(zipPath)
	if err != nil {
		return fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	fi, err := file.Stat()
	if err != nil {
		return fmt.Errorf("获取文件信息失败: %w", err)
	}

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] installapks goroutine recovered: %v", r)
			}
		}()
		defer pw.Close()
		defer writer.Close()

		part, partErr := writer.CreateFormFile("file", filepath.Base(zipPath))
		if partErr != nil {
			pw.CloseWithError(partErr)
			return
		}
		progReader := &progressReader{r: file, total: fi.Size(), onProg: onProgress}
		if _, copyErr := io.Copy(part, progReader); copyErr != nil {
			pw.CloseWithError(copyErr)
			return
		}
	}()

	longClient := &http.Client{Timeout: 7200 * time.Second}
	req, err := http.NewRequest("POST", rawURL, pr)
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	c.setAuth(req)
	resp, err := longClient.Do(req)
	if err != nil {
		return fmt.Errorf("安装APK失败: %s", util.FriendlyNetError(err))
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil // 安装成功但响应非JSON
	}
	if !isSuccess(result) {
		return fmt.Errorf("安装APK失败: %v", result)
	}
	return nil
}
