// Package mytos 定义模拟器/容器安卓实例 HTTP 客户端的公共接口与 DTO。
//
// 注意：模拟器与容器是两套独立实现，互不共享代码：
//   - 模拟器：internal/mytos/emulator
//   - 容器：  internal/mytos/container
//
// 即使 URL 当前一致，参数与返回字段也可能随版本演进出现差异，不能假设通用。
// 本包仅提供 Client 接口与跨端共享的数据结构（CloudFileEntry），不含任何实现。
package mytos

// CloudFileEntry 云机文件条目（/files?list= API 返回）
//
// 此结构为纯数据 DTO，模拟器与容器共用。若未来两端字段出现差异，
// 可在对应子包内定义独立类型并在 Client 接口中拆分返回类型。
type CloudFileEntry struct {
	File   string `json:"file"`   // 完整路径
	Flag   bool   `json:"flag"`   // true=文件夹
	Length int64  `json:"length"` // 文件大小(字节)
	Name   string `json:"name"`   // 文件名
}

// Client 模拟器/容器安卓实例 HTTP 客户端统一接口。
//
// 由 emulator.Client 与 container.Client 两个实现独立满足，二者不共享实现代码。
// 调用方通过本接口持有客户端实例，由工厂函数按镜像类型分派具体实现。
type Client interface {
	// --- P0 ---
	Screenshot(quality int) ([]byte, error)
	AutoClick(action string, id, x, y int, code ...int) error

	// --- P1 ---
	GetClipboard() (string, error)
	SetClipboard(text string) error
	QueryProxy() (map[string]interface{}, error)
	StartProxy(ip string, port int, usr, pwd string, proxyType int) error
	StopProxy() error
	Download(filePath string) ([]byte, error)
	DownloadToFile(filePath, savePath string) error
	DeviceSnap(level int) ([]byte, error)
	ListFiles(dirPath string) ([]CloudFileEntry, error)
	UploadFile(filePath string) (string, error)
	UploadFileWithProgress(filePath string, onProgress func(uploaded, total int64)) (string, error)

	// --- P2 ---
	GetInfo() (map[string]interface{}, error)
	GetVersion() (string, error)
	SetADBRoot(cmd int) error
	SetLanguage(language, country string) error
	SetIPLocate(ip, language string) error
	SetBackgroundApp(cmd int, pkg string) (map[string]interface{}, error)
	SetDisableKey(value int) error
	SetCamera(cmd, camType, path string) error
	ReceiveSMS(address, body string) error
	AddContact(contactsJSON string) error
	AddCallLog(number string, callType, duration int) error
	SetADID(cmd int, adid string) error
	UpdateFingerprint(dataJSON string) error
	SetShake(enabled bool) error
	SetAppPermission(pkg string) error
	GetRootList() (map[string]interface{}, error)
	GrantRoot(pkg string) error
	SetCameraSource(camType, path string, resolution int) error
	GetBootStartList() (map[string]interface{}, error)
	SetBootStart(packages []string) error
	SetProxyFilter(domains []string) error
	BackupApp(pkg, saveTo string) error
	RestoreApp(backupPath string) error
	RefreshLocation() error
	UploadFromURL(fileURL string) (string, error)
	InstallApk(apkPath string) error
	// InstallApks 批量安装 APK/XAPK；onProgress 为上传字节进度回调（安装阶段设备端同步处理无进度），可为 nil
	InstallApks(zipPath string, onProgress func(uploaded, total int64)) error

	// --- P3 ---
	UploadKeybox(certPath string) error
	ModuleMgr(cmd, module string) (map[string]interface{}, error)

	// --- P4（高级操作）---
	BackupDeviceInfo() (map[string]interface{}, error)
	RestoreDeviceInfo(data string) error
	HideApp(packages []string) error
	DisguiseApp(dataJSON string) error
	RotateCamera(rotation int, face bool) error
	SetAudio(audioType, source, state string) error
	SetGlobalDomain(host string) error
	SetLocation(lat, lng float64) error
	SetSensor(orientation, tilt, shake, shakeDelay int, scale float64) error
}
