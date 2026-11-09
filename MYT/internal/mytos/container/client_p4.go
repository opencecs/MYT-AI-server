package container

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
)

// BackupDeviceInfo 备份设备信息（/modifydev?cmd=8）
func (c *Client) BackupDeviceInfo() (map[string]interface{}, error) {
	return c.get("/modifydev?cmd=8")
}

// RestoreDeviceInfo 还原设备信息（/modifydev?cmd=9&data=encoded）
func (c *Client) RestoreDeviceInfo(data string) error {
	path := fmt.Sprintf("/modifydev?cmd=9&data=%s", url.QueryEscape(data))
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("还原设备信息失败: %v", result)
	}
	return nil
}

// HideApp 隐藏应用（/modifydev?cmd=3&data=base64）
// packages 为需要隐藏的应用包名列表
func (c *Client) HideApp(packages []string) error {
	arr, _ := json.Marshal(packages)
	encoded := base64.URLEncoding.EncodeToString(arr)
	path := fmt.Sprintf("/modifydev?cmd=3&data=%s", encoded)
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("隐藏应用失败: %v", result)
	}
	return nil
}

// DisguiseApp 伪装应用（V3: POST /modifydev cmd=24, fakeapps=JSON）
// dataJSON 为伪装信息 JSON 数组字符串，V3 使用 fakeapps 参数 + POST 表单方式
func (c *Client) DisguiseApp(dataJSON string) error {
	if dataJSON == "" {
		dataJSON = "[]"
	}
	var arr []map[string]interface{}
	if err := json.Unmarshal([]byte(dataJSON), &arr); err != nil {
		return fmt.Errorf("伪装应用参数格式错误: %w", err)
	}
	// 补全 action 字段并清理 null 值，避免服务端 String.length() NPE
	for i := range arr {
		if _, ok := arr[i]["action"]; !ok {
			arr[i]["action"] = "append"
		}
		for k, v := range arr[i] {
			if v == nil {
				delete(arr[i], k)
			}
		}
	}
	cleaned, _ := json.Marshal(arr)
	dataJSON = string(cleaned)

	log.Printf("[container] DisguiseApp cmd=24 fakeapps=%s", dataJSON)

	// 优先 POST fakeapps（V3 新接口），失败时 fallback 到 GET data（兼容旧固件）
	form := url.Values{}
	form.Set("cmd", "24")
	form.Set("fakeapps", dataJSON)
	result, err := c.postForm("/modifydev", form)
	if err == nil && isSuccess(result) {
		return nil
	}

	// POST 失败，尝试 GET data 方式（兼容不支持 fakeapps 的固件）
	log.Printf("[container] DisguiseApp POST failed, falling back to GET data: %v", err)
	encoded := url.QueryEscape(dataJSON)
	path := fmt.Sprintf("/modifydev?cmd=24&data=%s", encoded)
	result2, err2 := c.get(path)
	if err2 != nil {
		return err2
	}
	if !isSuccess(result2) {
		return fmt.Errorf("伪装应用失败: %v", result2)
	}
	return nil
}

// RotateCamera 旋转摄像头（/modifydev?cmd=22&rot=N&face=B）
// rotation: 0/1/2/3（对应 0/90/180/270 度）
// face: true=前置摄像头 false=后置摄像头
func (c *Client) RotateCamera(rotation int, face bool) error {
	faceVal := 0
	if face {
		faceVal = 1
	}
	path := fmt.Sprintf("/modifydev?cmd=22&rot=%d&face=%d", rotation, faceVal)
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("旋转摄像头失败: %v", result)
	}
	return nil
}

// SetAudio 设置音频资源（/modifydev?cmd=14&type=T&source=S&state=A）
// audioType: media/webrtc/rtmp/camera
// state: play/stop
func (c *Client) SetAudio(audioType, source, state string) error {
	path := fmt.Sprintf("/modifydev?cmd=14&type=%s&source=%s&state=%s",
		audioType, url.QueryEscape(source), state)
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("设置音频失败: %v", result)
	}
	return nil
}

// SetGlobalDomain 设置全球域名加速（/modifydev?sethost=H）
func (c *Client) SetGlobalDomain(host string) error {
	path := fmt.Sprintf("/modifydev?sethost=%s", url.QueryEscape(host))
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("设置域名加速失败: %v", result)
	}
	return nil
}

// SetLocation 设置经纬度（/modifydev?cmd=12&lat=L&lng=G）
func (c *Client) SetLocation(lat, lng float64) error {
	path := fmt.Sprintf("/modifydev?cmd=12&lat=%f&lng=%f", lat, lng)
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("设置经纬度失败: %v", result)
	}
	return nil
}

// SetSensor 设置运动传感器灵敏度（/modifydev?cmd=17 扩展参数）
// orientation: 0/90/180/270/-1（-1=不设置）
// tilt: 0-90
// shake: 0=关闭 1=开启 ≥2=灵敏度等级
// shakeDelay: 摇一摇延迟（毫秒）
// scale: 灵敏度系数（≥1.0）
func (c *Client) SetSensor(orientation, tilt, shake, shakeDelay int, scale float64) error {
	path := fmt.Sprintf("/modifydev?cmd=17&orientation=%d&tilt=%d&shake=%d&shake_delay=%d&scale=%f",
		orientation, tilt, shake, shakeDelay, scale)
	result, err := c.get(path)
	if err != nil {
		return err
	}
	if !isSuccess(result) {
		return fmt.Errorf("设置传感器失败: %v", result)
	}
	return nil
}
