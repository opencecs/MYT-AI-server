package main

// --- MYTOS 高级操作（P4 接口）---
// 这些方法经 getMytosClientForAndroid 分派到容器/模拟器客户端实现。

// BackupAndroidDeviceInfo 备份设备信息（/modifydev?cmd=8）
func (s *SdkApiServiceWrapper) BackupAndroidDeviceInfo(deviceIP, androidName string) (map[string]interface{}, error) {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return nil, err
	}
	return client.BackupDeviceInfo()
}

// RestoreAndroidDeviceInfo 还原设备信息（/modifydev?cmd=9&data=encoded）
func (s *SdkApiServiceWrapper) RestoreAndroidDeviceInfo(deviceIP, androidName, data string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.RestoreDeviceInfo(data)
}

// HideAndroidApp 隐藏应用（/modifydev?cmd=3&data=base64）
// packages 为需要隐藏的应用包名列表
func (s *SdkApiServiceWrapper) HideAndroidApp(deviceIP, androidName string, packages []string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.HideApp(packages)
}

// DisguiseAndroidApp 伪装应用（/modifydev?cmd=24&data=encoded）
// dataJSON 为伪装信息 JSON 数组字符串
func (s *SdkApiServiceWrapper) DisguiseAndroidApp(deviceIP, androidName, dataJSON string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.DisguiseApp(dataJSON)
}

// RotateAndroidCamera 旋转摄像头（/modifydev?cmd=22&rot=N&face=B）
// rotation: 0/1/2/3（0/90/180/270度）；face: true=前置 false=后置
func (s *SdkApiServiceWrapper) RotateAndroidCamera(deviceIP, androidName string, rotation int, face bool) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.RotateCamera(rotation, face)
}

// SetAndroidAudio 设置音频资源（/modifydev?cmd=14&type=T&source=S&state=A）
// audioType: media/webrtc/rtmp/camera；state: play/stop
func (s *SdkApiServiceWrapper) SetAndroidAudio(deviceIP, androidName, audioType, source, state string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.SetAudio(audioType, source, state)
}

// SetAndroidGlobalDomain 设置全球域名加速（/modifydev?sethost=H）
func (s *SdkApiServiceWrapper) SetAndroidGlobalDomain(deviceIP, androidName, host string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.SetGlobalDomain(host)
}

// SetAndroidLocation 设置经纬度（/modifydev?cmd=12&lat=L&lng=G）
func (s *SdkApiServiceWrapper) SetAndroidLocation(deviceIP, androidName string, lat, lng float64) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.SetLocation(lat, lng)
}

// SetAndroidSensor 设置运动传感器灵敏度（/modifydev?cmd=17 扩展）
func (s *SdkApiServiceWrapper) SetAndroidSensor(deviceIP, androidName string, orientation, tilt, shake, shakeDelay int, scale float64) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.SetSensor(orientation, tilt, shake, shakeDelay, scale)
}
