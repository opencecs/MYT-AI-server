package main

import (
	"log"
	"time"

	"github.com/moyunteng/myt/internal/util"
)

// startBackgroundServices 启动所有后台轮询/心跳/截图等服务。
// 生命周期由 main.go 通过 defer r.stopBackgroundServices() 管理。
func (r *serviceRegistry) startBackgroundServices() {
	r.authSvc.StartAutoSync()
	r.mirrorSvc.StartAutoRefresh()
	r.paymentSvc.StartAutoRefresh()
	// 客户端启动上报：立即报一次，此后每 30 分钟检查跨天（长期开机客户端的日活）
	r.clientReportSvc.Start()
	r.screencastSvc.StartPublicRefresher()
	r.detailSvc.Start()
	r.androidListSvc.Start()
	r.screenshotSvc.Start()
	// 坑位到期兜底刷新：30 分钟全量一轮（并发限 4）——存量设备的到期状态
	// 随时间推进（即将到期/已过期标签翻转）；新设备由添加时的
	// queryAuthForNewDevices 增量查询立即生效。
	// 对所有模式生效：v2 主机与 v3/local 共用云端坑位授权体系，均参与兜底查询。
	r.slotSvc.StartAutoQuery()
	// 事件推送：对全部 local 设备建 WS 连接——拨号即探测（拨通+snapshot=在线，
	// 拨号失败=离线）。缓存里 State=1 的设备也参与，避免「等心跳确认」死锁（httpbeat 已撤）。
	// v2 模式跳过：V2 主机是裸 Docker（无 eventws 端点），拨号只会空转报错、状态打架。
	// v4 模式：走独立的 V4 WS 连接池（/ws/events 协议不同，见 internal/v4ws）
	if r.appSvc.cm != nil {
		switch r.appSvc.cm.GetDeployMode() {
		case "v2":
			// V2 主机是裸 Docker（无 eventws 端点），不建连
		case "v4":
			if r.v4wsSvc != nil {
				r.v4wsSvc.Sync(v4HostsOf(r.appSvc.cm.GetDeviceList()))
			}
		default:
			r.eventwsSvc.Sync(r.appSvc.cm.GetDeviceList())
		}
	}
	// 设备发现：仅 public 模式启用自动轮询（线上设备在线状态需定时拉取）。
	// local 模式不再启动扫描（用户决策 2026-09-03）：添加进来的设备靠 WS 事件驱动——
	// 列表从缓存加载 + eventws.Sync 拨号即探测；IP 变化靠手动刷新修正；
	// 新设备发现/待添加列表靠手动扫描/刷新按钮。
	if r.discoverSvc.configManager.GetDeployMode() == "public" {
		r.discoverSvc.StartAutoPoll()
	}
	// v2 主机心跳：无条件常驻（tick 内自检部署模式，非 v2 空转）——
	// 启动即挂上，之后切到 v2 模式无需再启停联动。对齐壳子：7678 UDP 单播
	// 测活、状态真变化才广播 devicelist:updated。
	r.v2Svc.StartHeartbeat()
	if r.appSvc.cm != nil && r.appSvc.cm.GetAutoUpgradeSDK() {
		r.autoUpgradeSvc.Start()
	}
	if err := r.iscsiTargetSvc.StartTarget(); err != nil {
		log.Printf("启动 iSCSI Target 失败: %v", err)
	}

	// 启动流媒体服务器
	if r.streamSvc != nil {
		r.streamSvc.StartServer()
	}

	// 启动后检查所有在线设备的 iSCSI 挂载状态，同步磁盘显示数据
	util.GoSafe(func() {
		time.Sleep(10 * time.Second) // 等设备列表就绪
		devices := r.appSvc.cm.GetOnlineDevices()
		for _, d := range devices {
			if d.IP == "" {
				continue
			}
			status, err := r.iscsiSvc.GetIscsiStatus(d.IP)
			if err != nil {
				continue
			}
			if status.IsMounted {
				r.appSvc.cm.UpdateDeviceDisk(d.DeviceID, status.UsedMB*1024*1024, status.TotalMB*1024*1024)
				log.Printf("[iSCSI] 启动同步: 设备 %s 已挂载，磁盘 %dMB/%dMB", d.IP, status.UsedMB, status.TotalMB)
			}
		}
	})
}

// stopBackgroundServices 停止所有后台服务（与 startBackgroundServices 对称）。
func (r *serviceRegistry) stopBackgroundServices() {
	r.authSvc.StopAutoSync()
	r.mirrorSvc.StopAutoRefresh()
	r.paymentSvc.StopAutoRefresh()
	r.clientReportSvc.Stop()
	r.screencastSvc.StopPublicRefresher()
	r.discoverSvc.StopAutoPoll()
	r.v2Svc.StopHeartbeat()
	r.detailSvc.Stop()
	r.androidListSvc.Stop()
	r.eventwsSvc.Stop()
	if r.v4wsSvc != nil {
		r.v4wsSvc.Stop()
	}
	r.screenshotSvc.Stop()
	r.slotSvc.StopAutoQuery()
	r.autoUpgradeSvc.Stop()
	r.screencastSvc.StopAllProjections()
	r.iscsiTargetSvc.StopTarget()
	if r.streamSvc != nil {
		r.streamSvc.StopServer()
	}
	if r.cameraMicSvc != nil {
		r.cameraMicSvc.StopAll()
	}
}
