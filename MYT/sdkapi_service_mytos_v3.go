package main

// --- 容器/模拟器信息查询与配置（V3 接口） ---
// 以下方法都经 getMytosClientForAndroid 分派到对应实现（模拟器/容器）。

// GetAndroidInfo 获取云机信息（/info）
func (s *SdkApiServiceWrapper) GetAndroidInfo(deviceIP, androidName string) (map[string]interface{}, error) {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return nil, err
	}
	return client.GetInfo()
}

// GetAndroidVersion 获取云机版本（/queryversion）
func (s *SdkApiServiceWrapper) GetAndroidVersion(deviceIP, androidName string) (string, error) {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return "", err
	}
	return client.GetVersion()
}

// SetAndroidLanguage 设置语言和国家（/modifydev?cmd=13）
func (s *SdkApiServiceWrapper) SetAndroidLanguage(deviceIP, androidName, language, country string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.SetLanguage(language, country)
}

// SetAndroidADBRoot 开关 ADB root（/adb?cmd=N）
func (s *SdkApiServiceWrapper) SetAndroidADBRoot(deviceIP, androidName string, cmd int) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.SetADBRoot(cmd)
}

// SetAndroidBackgroundApp 后台保活（/background?cmd=N&package=P）
// cmd: 1=开启 0=关闭；pkg 为空时对所有应用操作
func (s *SdkApiServiceWrapper) SetAndroidBackgroundApp(deviceIP, androidName string, cmd int, pkg string) (map[string]interface{}, error) {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return nil, err
	}
	return client.SetBackgroundApp(cmd, pkg)
}

// SetAndroidDisableKey 屏蔽按键（/disablekey?value=N）
// value: 1=屏蔽返回键 2=屏蔽Home 3=屏蔽返回+Home 0=关闭
func (s *SdkApiServiceWrapper) SetAndroidDisableKey(deviceIP, androidName string, value int) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.SetDisableKey(value)
}

// ReceiveAndroidSMS 模拟接收短信（POST /sms?cmd=4）
func (s *SdkApiServiceWrapper) ReceiveAndroidSMS(deviceIP, androidName, address, body string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.ReceiveSMS(address, body)
}

// AddAndroidContact 添加联系人（/addcontact?data=JSON）
func (s *SdkApiServiceWrapper) AddAndroidContact(deviceIP, androidName, contactsJSON string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.AddContact(contactsJSON)
}

// AddAndroidCallLog 添加通话记录（/callog?number=N&type=T&duration=D）
// callType: 1=来电 2=去电 3=未接
func (s *SdkApiServiceWrapper) AddAndroidCallLog(deviceIP, androidName, number string, callType, duration int) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.AddCallLog(number, callType, duration)
}

// SetAndroidADID 设置/生成 Google ADID（/adid?cmd=N&adid=X）
// cmd: 1=生成新ADID 2=设置指定ADID
func (s *SdkApiServiceWrapper) SetAndroidADID(deviceIP, androidName string, cmd int, adid string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.SetADID(cmd, adid)
}

// UpdateAndroidFingerprint 更新设备指纹（/modifydev?cmd=7&data=JSON）
func (s *SdkApiServiceWrapper) UpdateAndroidFingerprint(deviceIP, androidName, dataJSON string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.UpdateFingerprint(dataJSON)
}

// SetAndroidAppPermission 设置应用权限（/modifydev?cmd=18&pkg=P）
func (s *SdkApiServiceWrapper) SetAndroidAppPermission(deviceIP, androidName, pkg string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.SetAppPermission(pkg)
}

// GetAndroidRootList 获取 Root 授权列表（/modifydev?cmd=10&action=list）
func (s *SdkApiServiceWrapper) GetAndroidRootList(deviceIP, androidName string) (map[string]interface{}, error) {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return nil, err
	}
	return client.GetRootList()
}

// GrantAndroidRoot 为指定包名开启 Root（/modifydev?cmd=10&pkg=P&root=true）
func (s *SdkApiServiceWrapper) GrantAndroidRoot(deviceIP, androidName, pkg string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.GrantRoot(pkg)
}

// GetAndroidBootStartList 获取开机启动列表（/appbootstart?cmd=1）
func (s *SdkApiServiceWrapper) GetAndroidBootStartList(deviceIP, androidName string) (map[string]interface{}, error) {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return nil, err
	}
	return client.GetBootStartList()
}

// SetAndroidBootStart 设置开机启动应用（POST /appbootstart?cmd=2，body 为包名数组）
func (s *SdkApiServiceWrapper) SetAndroidBootStart(deviceIP, androidName string, packages []string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.SetBootStart(packages)
}

// SetAndroidProxyFilter 设置代理域名过滤（POST /proxy?cmd=4，body 为域名数组）
func (s *SdkApiServiceWrapper) SetAndroidProxyFilter(deviceIP, androidName string, domains []string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.SetProxyFilter(domains)
}

// BackupAndroidApp 导出应用数据（/backrestore?cmd=backup&pkg=P&saveto=PATH）
func (s *SdkApiServiceWrapper) BackupAndroidApp(deviceIP, androidName, pkg, saveTo string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.BackupApp(pkg, saveTo)
}

// RestoreAndroidApp 导入应用数据（/backrestore?cmd=recovery&backuppath=PATH）
func (s *SdkApiServiceWrapper) RestoreAndroidApp(deviceIP, androidName, backupPath string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.RestoreApp(backupPath)
}

// RefreshAndroidLocation 刷新定位（/task）
func (s *SdkApiServiceWrapper) RefreshAndroidLocation(deviceIP, androidName string) error {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return err
	}
	return client.RefreshLocation()
}

// UploadAndroidFromURL 通过 URL 下载文件到云机（/?task=upload&file=URL）
func (s *SdkApiServiceWrapper) UploadAndroidFromURL(deviceIP, androidName, fileURL string) (string, error) {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return "", err
	}
	return client.UploadFromURL(fileURL)
}

// AndroidModuleMgr 模块管理（/modulemgr?cmd=C&module=M）
// cmd: install/uninstall/list 等；module: magisk/gms 等
func (s *SdkApiServiceWrapper) AndroidModuleMgr(deviceIP, androidName, cmd, module string) (map[string]interface{}, error) {
	client, err := s.getMytosClientForAndroid(deviceIP, androidName)
	if err != nil {
		return nil, err
	}
	return client.ModuleMgr(cmd, module)
}
