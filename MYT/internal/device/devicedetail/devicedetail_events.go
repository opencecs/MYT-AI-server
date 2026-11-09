// devicedetail 的事件推送集成：system/stats 事件（设备级指标）替代 30s /info/device 轮询。
//
// 数据流：eventws → HandleSystemStats（20s 周期推送，与 /info/device 动态字段同构）
//
//	→ 反序列化 config.DeviceDetail（json tag 对齐，FlexInt 兼容数字/字符串）
//	→ updateDeviceDisk（iSCSI 覆盖逻辑保留）+ UpdateDeviceMem → onUpdated 通知前端
//
// hello 身份字段（机型/固件版本）：一次性下发。memtotal 等静态字段由设备上线时的
// 一次性 QueryDevice 建立（/info/device 全量），system/stats 只刷动态指标。
package devicedetail

import (
	"encoding/json"
	"log"
	"strconv"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/eventws"
	"github.com/moyunteng/myt/internal/sdkapi"
)

// notifyDebounce 前端通知节流：全量设备 × 20s 心跳事件的合并窗口——窗口内
// 多台设备的事件只触发一次 devicestats:updated（全量列表 payload，O(N) 序列化
// + 前端全列表合并）。500ms：指标刷新延迟无感知，而千台规模下 100ms 会造成
// 持续 10 条/s 的全量广播（每条几百 KB IPC + 前端合并），渲染进程被打满。
const notifyDebounce = 500 * time.Millisecond

// HandleSystemStats system/stats 事件 → 设备面板指标刷新（eventws.SetSystemStatsHandler 注入）。
// 双帧形态分流（ParseSystemStatsFrame）：
//
//   - disk 帧（新 SDK，字节）：只更新磁盘三项（iSCSI 覆盖逻辑保留），不触碰 panel 字段；
//     弹窗已改主动拉取（RefreshDeviceInfo），不再喂 devicedetail:updated
//   - panel 帧（老 SDK /info/device 动态字段）：主体照旧（静态回填/UpdateDeviceMem/
//     SetDeviceDetail/onDeviceUpdated/notifySoon）；磁盘更新加守卫——新 SDK 已发过
//     disk 帧后 panel 里的 mmctotal/mmcuse 是冻结值，跳过不覆盖
func (s *Service) HandleSystemStats(deviceID string, raw json.RawMessage) {
	panel, disk, err := eventws.ParseSystemStatsFrame(raw)
	if err != nil {
		log.Printf("[devicedetail] system/stats 解析失败 device=%s: %v", deviceID, err)
		return
	}
	if disk != nil {
		s.handleSystemStatsDisk(deviceID, disk)
		return
	}
	s.handleSystemStatsPanel(deviceID, panel)
}

// handleSystemStatsDisk disk 帧（新 SDK）：只更新磁盘三项。ip 从设备详情缓存取
//（disk 帧不带 ip），iSCSI 覆盖逻辑与 panel 帧共用 updateDeviceDisk。
func (s *Service) handleSystemStatsDisk(deviceID string, disk *eventws.SystemDiskStats) {
	// 新 SDK 已发过 disk 帧：panel 帧的磁盘字段从此视为冻结值
	s.diskMu.Lock()
	s.diskFrameSeen[deviceID] = true
	s.diskMu.Unlock()

	var ip string
	s.mu.Lock()
	if existing := s.configManager.GetDeviceDetail(deviceID); existing != nil {
		ip = existing.IP
	}
	s.mu.Unlock()
	s.updateDeviceDisk(deviceID, ip, disk.DiskUsed, disk.DiskTotal)
	s.notifySoon()
}

// handleSystemStatsPanel panel 帧（老 SDK /info/device 动态字段）：主体照旧。
func (s *Service) handleSystemStatsPanel(deviceID string, raw json.RawMessage) {
	var d config.DeviceDetail
	if err := json.Unmarshal(raw, &d); err != nil {
		log.Printf("[devicedetail] system/stats 解析失败 device=%s: %v", deviceID, err)
		return
	}

	// 事件只带动态指标——保留缓存中的静态字段（model/version/ip 及 memtotal 基准，
	// 后者由设备上线时的一次性 QueryDevice 建立；mmcmodel/speed 同为 REST 独有的
	// 静态字段，stats 不带，若不保留会被空值洗掉）
	s.mu.Lock()
	if existing := s.configManager.GetDeviceDetail(deviceID); existing != nil {
		d.Model = existing.Model
		d.Version = existing.Version
		d.SDKVersion = existing.SDKVersion
		d.IP = existing.IP
		d.DeviceID = existing.DeviceID
		d.MMCModel = existing.MMCModel
		d.Speed = existing.Speed
		if parseNum(d.MemTotal) == 0 {
			d.MemTotal = existing.MemTotal
		}
	}
	s.mu.Unlock()
	d.DeviceID = deviceID

	// 磁盘更新守卫：新 SDK 的 disk 帧才是磁盘权威源（panel 帧的 mmctotal/mmcuse
	// 是设备端冻结值，20min 才刷新）。diskFrameSeen=false 说明老 SDK 从未发过
	// disk 帧，panel 字段是唯一磁盘数据——照旧更新；true 则跳过。
	s.diskMu.Lock()
	seen := s.diskFrameSeen[deviceID]
	s.diskMu.Unlock()
	if !seen {
		s.updateDeviceDisk(deviceID, d.IP, int64(d.MMCUse)*mb, int64(d.MMCTotal)*mb)
	}
	s.configManager.UpdateDeviceMem(deviceID, parseNum(d.MemUse)*mb, parseNum(d.MemTotal)*mb)
	s.configManager.SetDeviceDetail(deviceID, &d)

	// 单设备实时通知（panel 帧主体照旧；唯一消费者 device_service.go 的弹窗
	// 门控 emit 已随弹窗改主动拉取而下线，nil 时跳过）
	s.mu.Lock()
	deviceFn := s.onDeviceUpdated
	s.mu.Unlock()
	if deviceFn != nil {
		deviceFn(deviceID, &d)
	}

	// 指标更新通知（onUpdated → 前端 devicestats:updated，只刷磁盘/内存条）。
	// 注意：不再复用 devicelist:updated——stats 是 20s/台的心跳流（93 台 ≈ 每秒 5 条），
	// 若混入 devicelist:updated，下游（授权页等）会对每次心跳全量重查授权，形成风暴。
	// 磁盘内存条照常实时刷新，授权查询只在真正的列表结构变化时发生（用户决策 2026-09-04）。
	s.notifySoon()
}

// notifySoon 前端通知节流：100ms 合并窗口（多设备 system/stats 事件汇成一次 emit）。
func (s *Service) notifySoon() {
	s.notifyMu.Lock()
	defer s.notifyMu.Unlock()
	if s.notifyTimer != nil {
		return // 已有待触发的通知
	}
	s.notifyTimer = time.AfterFunc(notifyDebounce, func() {
		s.notifyMu.Lock()
		s.notifyTimer = nil
		s.mu.Lock()
		fn := s.onUpdated
		s.mu.Unlock()
		s.notifyMu.Unlock()
		if fn != nil {
			fn()
		}
	})
}

// HandleHello hello 身份字段：机型/固件版本/SDK 版本写入设备详情缓存（可缺省，容忍不存在）。
// SDK 版本是升级轮询的免探测数据源（设备重启必经 WS 重连 hello——版本最权威）。
func (s *Service) HandleHello(deviceID string, h *eventws.HelloFrame) {
	if h == nil || (h.Model == "" && h.DeviceVersion == "" && h.SDKVersionNum() == 0) {
		return
	}
	// model 兜底：hello 缺省机型字段（v206+ 可缺省）但带固件版本时，从固件版本
	// 解析（"QL-r1q-...-v3" → "r1q_v3"），与 /info/device 的兜底同源
	model := h.Model
	if model == "" {
		model = sdkapi.ParseModelFromFirmware(h.DeviceVersion)
	}
	existing := s.configManager.GetDeviceDetail(deviceID)
	if existing == nil {
		existing = &config.DeviceDetail{DeviceID: deviceID}
	}
	if model != "" && existing.Model != model {
		existing.Model = model
	}
	if h.DeviceVersion != "" && existing.Version != h.DeviceVersion {
		existing.Version = h.DeviceVersion
	}
	if v := h.SDKVersionNum(); v > 0 {
		existing.SDKVersion = v
	}
	s.configManager.SetDeviceDetail(deviceID, existing)
}

// parseNum 字符串数字兼容解析（system/stats 的 memuse 是 "13144" 这类字符串）。
func parseNum(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
