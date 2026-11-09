package main

import (
	"fmt"
	"sync"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/maintenance"
	"github.com/moyunteng/myt/internal/sdkapi"
	"github.com/moyunteng/myt/internal/taskqueue"
)

type MaintenanceServiceWrapper struct {
	cm        *config.Manager
	sdkApiSvc *SdkApiServiceWrapper
	taskSvc   *taskqueue.Service
}

func NewMaintenanceServiceWrapper(cm *config.Manager) *MaintenanceServiceWrapper {
	return &MaintenanceServiceWrapper{cm: cm}
}

// SetSdkApiService 注入 SDK API 服务（用于公网模式客户端创建）
func (m *MaintenanceServiceWrapper) SetSdkApiService(svc *SdkApiServiceWrapper) {
	m.sdkApiSvc = svc
}

// SetTaskQueueService 注入任务队列服务（SubmitTeboxInstallTask 提交任务用）
func (m *MaintenanceServiceWrapper) SetTaskQueueService(svc *taskqueue.Service) {
	m.taskSvc = svc
}

// newClient 创建带密码的 SDK 客户端（公网模式走网关透传）
func (m *MaintenanceServiceWrapper) newClient(deviceIP string) *sdkapi.Client {
	if m.sdkApiSvc != nil {
		return m.sdkApiSvc.newClient(deviceIP)
	}
	// 降级：sdkApiSvc 未注入时走本地直连
	client := sdkapi.NewClient(deviceIP)
	if m.cm != nil {
		if pwd := m.cm.GetDevicePassword(deviceIP); pwd != "" {
			client.SetPassword(pwd)
		}
	}
	return client
}

// isV4Mode v4 模式判定。
//
// V4 主机（tobox_player）只提供 8088 的实例/镜像管理接口；维护类操作全部依赖
// V3 设备的 SSH 通道或 8000 SDK 接口（/server/device/*、/android/pruneImages…），
// 在 V4 下必然失败。这些入口在 UI 上照旧显示（与其他模式一致），点击后走
// v4Unsupported 返回明确原因，而不是让用户等几十秒超时或看到"指令已发送"的假成功。
func (m *MaintenanceServiceWrapper) isV4Mode() bool {
	return m.cm != nil && m.cm.GetDeployMode() == "v4"
}

// v4Unsupported 维护类操作的 V4 拒绝返回（与 maintenance 包的 errResult 同构）
func v4Unsupported(op string) map[string]interface{} {
	return map[string]interface{}{
		"success": false,
		"message": "tebox 模式暂不支持「" + op + "」（tebox 主机只提供 8088 实例管理接口，无 SSH/8000 维护通道）",
	}
}

func (m *MaintenanceServiceWrapper) InstallMytPanel(deviceIP string) map[string]interface{} {
	if m.isV4Mode() {
		return v4Unsupported("安装魔云互联")
	}
	return maintenance.InstallMytPanel(deviceIP)
}

func (m *MaintenanceServiceWrapper) UninstallMytPanel(deviceIP string) map[string]interface{} {
	if m.isV4Mode() {
		return v4Unsupported("卸载魔云互联")
	}
	return maintenance.UninstallMytPanel(deviceIP)
}

func (m *MaintenanceServiceWrapper) CheckMytPanelInstalled(deviceIP string) map[string]interface{} {
	if m.isV4Mode() {
		return v4Unsupported("查询魔云互联状态")
	}
	return maintenance.CheckMytPanelInstalled(deviceIP)
}

// StartMytPanel 拉起设备上已安装但未运行的魔云互联面板（SSH nohup，重启后
// 进程丢失场景的手动补救；v2 与 v3 统一走 internal 的 SSH 路径）。
func (m *MaintenanceServiceWrapper) StartMytPanel(deviceIP string) map[string]interface{} {
	if m.isV4Mode() {
		return v4Unsupported("启动魔云互联")
	}
	return maintenance.StartMytPanel(deviceIP)
}

// --- 公网穿透（frp）：设备端 frpc + 用户服务器端 frps ---

func (m *MaintenanceServiceWrapper) InstallTunnel(deviceIP string, serverAddr string, serverPort int, token string) map[string]interface{} {
	if m.isV4Mode() {
		return v4Unsupported("安装公网穿透")
	}
	return maintenance.InstallTunnel(deviceIP, serverAddr, serverPort, token)
}

func (m *MaintenanceServiceWrapper) UninstallTunnel(deviceIP string) map[string]interface{} {
	if m.isV4Mode() {
		return v4Unsupported("卸载公网穿透")
	}
	return maintenance.UninstallTunnel(deviceIP)
}

// InstallTunnelServer / UninstallTunnelServer 作用于「用户自己的服务器」，与设备模式无关，不做 v4 拦截。

func (m *MaintenanceServiceWrapper) InstallTunnelServer(serverIP string, srvUser string, srvPassword string, srvPort int, bindPort int, dashboardPort int, dashboardUser string, dashboardPassword string) map[string]interface{} {
	return maintenance.InstallTunnelServer(serverIP, srvUser, srvPassword, srvPort, bindPort, dashboardPort, dashboardUser, dashboardPassword)
}

func (m *MaintenanceServiceWrapper) UninstallTunnelServer(serverIP string, srvUser string, srvPassword string, srvPort int) map[string]interface{} {
	return maintenance.UninstallTunnelServer(serverIP, srvUser, srvPassword, srvPort)
}

func (m *MaintenanceServiceWrapper) GetLogUsage(deviceIP, androidName string) map[string]interface{} {
	if m.isV4Mode() {
		return v4Unsupported("查询日志占用")
	}
	return maintenance.GetLogUsage(deviceIP, androidName, m.newClient(deviceIP))
}

func (m *MaintenanceServiceWrapper) CleanLogs(deviceIP, androidName string, categories []string) map[string]interface{} {
	if m.isV4Mode() {
		return v4Unsupported("清理日志")
	}
	return maintenance.CleanLogs(deviceIP, androidName, categories, m.newClient(deviceIP))
}

func (m *MaintenanceServiceWrapper) GetAndroidCacheUsage(deviceIP, androidName string) map[string]interface{} {
	if m.isV4Mode() {
		return v4Unsupported("查询云机缓存占用")
	}
	return maintenance.GetAndroidCacheUsage(deviceIP, androidName, m.newClient(deviceIP))
}

func (m *MaintenanceServiceWrapper) CleanAndroidCache(deviceIP, androidName string, categories []string) map[string]interface{} {
	if m.isV4Mode() {
		return v4Unsupported("清理云机缓存")
	}
	return maintenance.CleanAndroidCache(deviceIP, androidName, categories, m.newClient(deviceIP))
}

func (m *MaintenanceServiceWrapper) ResetDevice(deviceIP string) map[string]interface{} {
	if m.isV4Mode() {
		return v4Unsupported("格式化磁盘")
	}
	return maintenance.ResetDevice(deviceIP)
}

func (m *MaintenanceServiceWrapper) RebootDevice(deviceIP string) map[string]interface{} {
	if m.isV4Mode() {
		return v4Unsupported("重启设备")
	}
	return maintenance.RebootDevice(deviceIP)
}

func (m *MaintenanceServiceWrapper) PruneDeviceImages(deviceIP string) map[string]interface{} {
	if m.isV4Mode() {
		return v4Unsupported("清理设备镜像")
	}
	return maintenance.PruneDeviceImages(deviceIP)
}

// --- tebox_player SDK（tobox_player）安装 ---

// installTeboxMu 安装防重入：同一设备同时只装一次（任务队列已限并发，但
// 用户可能同时右键两台/双击重复提交——同一 IP 二次提交直接拒绝）。
var installTeboxMu sync.Mutex
var installingTeboxIPs = make(map[string]bool)

// CheckTeboxSdkInstalled 查询设备 tobox_player SDK 安装状态（8088 /health）。
func (m *MaintenanceServiceWrapper) CheckTeboxSdkInstalled(deviceIP string) map[string]interface{} {
	if m.isV4Mode() {
		return v4Unsupported("查询 tebox SDK 状态")
	}
	return maintenance.TeboxSdkStatus(deviceIP)
}

// SubmitTeboxInstallTask 提交 tebox SDK 安装任务（任务队列执行，进度在任务面板）。
// 返回任务 ID；同一设备已有安装任务时返回错误。
func (m *MaintenanceServiceWrapper) SubmitTeboxInstallTask(deviceIP string) (string, error) {
	if m.isV4Mode() {
		return "", fmt.Errorf("tebox 模式无需安装 tebox SDK（主机即 tobox_player）")
	}
	if deviceIP == "" {
		return "", fmt.Errorf("未选择目标设备")
	}
	ip := maintenance.ExtractPureIP(deviceIP)
	if !tryLockTeboxInstall(ip) {
		return "", fmt.Errorf("设备 %s 正在安装 tebox SDK，请勿重复操作", ip)
	}
	// 锁在 handler 内释放（teboxInstall handler 完成时经注入钩子 UnlockTeboxInstall）
	taskSvc := m.taskSvc
	if taskSvc == nil {
		UnlockTeboxInstall(ip)
		return "", fmt.Errorf("任务队列服务未初始化")
	}
	return taskSvc.CreateTeboxInstallTask(ip), nil
}

// tryLockTeboxInstall / unlockTeboxInstall 安装防重入（teboxInstall handler 与
// SubmitTeboxInstallTask 共用；锁的获取在提交侧、释放在执行侧，提交失败由
// 队列/直接错误路径解锁）。
func tryLockTeboxInstall(ip string) bool {
	installTeboxMu.Lock()
	defer installTeboxMu.Unlock()
	if installingTeboxIPs[ip] {
		return false
	}
	installingTeboxIPs[ip] = true
	return true
}

// UnlockTeboxInstall 导出给任务队列 handler 在任务终态释放锁（exported：
// taskqueue 包不能引用 main 的未导出符号，经 hook 函数注入）。
func UnlockTeboxInstall(ip string) {
	installTeboxMu.Lock()
	defer installTeboxMu.Unlock()
	delete(installingTeboxIPs, ip)
}

// HasTeboxInstallLock 查询某设备是否正持有安装锁（队列恢复场景：客户端重启后
// 锁丢失，handler 用它避免重复锁提示——目前仅防御性查询）。
func HasTeboxInstallLock(ip string) bool {
	installTeboxMu.Lock()
	defer installTeboxMu.Unlock()
	return installingTeboxIPs[ip]
}
