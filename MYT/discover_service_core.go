package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/moyunteng/myt/internal/cloudapi"
	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/device/discover"
)

// discoverAndUpdate 单轮设备发现（local 走 UDP 广播，public 走 discoverFromCloud）。
// 防重入：polling atomic CAS 保证上一轮未完成则跳过。模式代际校验避免旧模式结果污染。
// 返回失败原因（public 网关登录/拉取失败、local UDP 发现失败），供手动刷新
// 路径回传前端 toast——自动轮询路径忽略返回值。
func (s *DiscoverServiceWrapper) discoverAndUpdate() error {
	// 防止重叠：上一轮未完成则跳过
	if !atomic.CompareAndSwapInt32(&s.polling, 0, 1) {
		// 手动刷新撞守卫：转记待补跑标志，本轮结束（defer）时补跑一轮——
		// 静默丢弃会让「刷新」按钮转完却什么都没发生（公网刷新必达）。
		if atomic.SwapInt32(&s.manualHint, 0) == 1 {
			atomic.StoreInt32(&s.manualPending, 1)
		}
		log.Printf("[discover] 上一轮发现未完成，跳过本轮")
		return nil
	}
	defer func() {
		atomic.StoreInt32(&s.polling, 0)
		// 手动刷新在上一轮进行期间到达：补跑一轮（带 10s 上限防死循环——
		// 补跑再撞守卫说明轮询系统异常，放弃等下个自动轮询周期兜底）。
		if atomic.SwapInt32(&s.manualPending, 0) != 1 {
			return
		}
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if atomic.CompareAndSwapInt32(&s.polling, 0, 1) {
				atomic.StoreInt32(&s.polling, 0)
				// 补跑结果不能丢：手动刷新路径已返回，失败只经日志/错误事件可见
				if err := s.discoverAndUpdate(); err != nil { // manualPending 已清零，不会递归补跑
					log.Printf("[discover] 补跑手动刷新失败: %v", err)
				}
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		log.Printf("[discover] 补跑手动刷新超时（10s 内未获得执行窗口），放弃本轮")
	}()

	// 记录启动时的模式代际：模式切换会 bump，本轮结束时若代际已变说明是过期结果，丢弃
	gen := atomic.LoadInt64(&s.generation)
	startMode := s.configManager.GetDeployMode()

	start := time.Now()

	// public 模式：走线上服务拉取设备列表，不走 UDP 广播。
	// discoverFromCloud 返回时阶段1（SetDeviceList）已同步完成，随即对账
	// eventws——公网设备经网关透传建连（syncer 不再区分模式，Sync 自行分发）
	if s.configManager.GetDeployMode() == "public" {
		err := s.discoverFromCloud(start, gen)
		if s.eventwsSyncer != nil {
			s.eventwsSyncer()
		}
		return err
	}

	// v2 模式：主机列表来自手动添加（Docker 2375 探测），"发现"等价于批量探测刷新在线状态
	if s.configManager.GetDeployMode() == "v2" {
		if s.v2Refresher != nil {
			s.v2Refresher()
		}
		// v2 的主机同样可能在本进程登录之后才加入列表（此前只有添加时查一次），
		// 这里做限频 + 只查未覆盖设备的异步补查，让授权页尽快拿到 bind_status。
		s.refreshBindStatusForDevices(s.deviceIDsForBind())
		return nil
	}

	// v4 模式：UDP 7678 广播发现候选 IP + 8088 /health 验证（方案 A，与 v2 的
	// 「广播 + 81 dc_api 验证」同构）。扫描与落库在 V4ServiceWrapper 内完成：
	//   - /health 通过 → 按 DeviceID 查正式列表（在 → 刷新信息 + 置在线；
	//     不在 → 进「待添加」，自动发现不自动添加）
	//   - 不做型号白名单过滤（V4 无白名单信息），也不套 v3 的 LocalDevices 过滤
	//   - 事件推送 pendinglist:updated / devicelist:updated 由扫描器负责
	// 代际校验：探测是异步的，扫完可能已切走模式——把「代际 + 模式未变」作为提交
	// 钩子传下去，扫描器在写库前校验，失效则整轮结果丢弃（不写任何数据桶）。
	if s.configManager.GetDeployMode() == "v4" {
		if s.v4Scanner == nil {
			log.Printf("[discover] v4 模式但 V4 扫描器未注入，跳过本轮")
			return nil
		}
		err := s.v4Scanner(context.Background(), func() bool {
			return atomic.LoadInt64(&s.generation) == gen && s.configManager.GetDeployMode() == startMode
		})
		if err != nil {
			log.Printf("[discover] v4 扫描失败: %v, 耗时: %v", err, time.Since(start))
			return fmt.Errorf("tebox 设备扫描失败: %w", err)
		}
		log.Printf("[discover] v4 扫描完成, 耗时: %v", time.Since(start))
		// 发现完成后对账 V4 WS 连接（新设备建连；syncer 内部按模式路由到 v4ws）
		if s.eventwsSyncer != nil {
			s.eventwsSyncer()
		}
		return nil
	}

	result, err := s.discoverer.Discover(context.Background())
	if err != nil {
		log.Printf("[discover] 发现失败: %v, 耗时: %v", err, time.Since(start))
		return fmt.Errorf("本地设备发现失败: %w", err)
	}
	// 本地模式只收 v3 设备（UDP 广播是全量回复，V2 主机 _v2 由 v2 模式处理）
	result = discover.LocalDevices(result)

	// 代际校验：发现期间若模式已切换，丢弃这轮 local UDP 结果，避免污染 public 列表
	if atomic.LoadInt64(&s.generation) != gen || s.configManager.GetDeployMode() != startMode {
		log.Printf("[discover] 模式已切换，丢弃本轮 UDP 发现结果 (%d 台)", len(result))
		return nil
	}

	log.Printf("[discover] 发现完成: %d 台设备, 耗时: %v", len(result), time.Since(start))

	var newDeviceIDs []string

	for _, dev := range result {
		cacheItem := config.DeviceCache{
			DeviceID:   dev.DeviceID,
			IP:         dev.IP,
			DeviceType: dev.DeviceType,
			DeviceName: dev.DeviceName,
			Version:    dev.Version,
			State:      1,
			Order:      0,
		}

		existing := s.configManager.GetDevice(dev.DeviceID)
		if existing != nil {
			// 已在正式设备列表 → 更新信息
			changed := s.configManager.UpsertDeviceCheck(cacheItem)
			if s.ipMappingSetter != nil {
				s.ipMappingSetter(dev.IP, dev.DeviceID)
			}
			if changed && s.onNewDevice != nil {
				log.Printf("[discover] 设备变化: id=%s ip=%s type=%s name=%s", dev.DeviceID, dev.IP, dev.DeviceType, dev.DeviceName)
				s.onNewDevice(dev.DeviceID, dev.IP)
			}
		} else {
			// 不在正式列表 → 加入待添加列表（自动发现不自动添加）
			s.configManager.UpsertPendingDevice(cacheItem)
			newDeviceIDs = append(newDeviceIDs, dev.DeviceID)
			log.Printf("[discover] 发现新设备(待添加): id=%s ip=%s type=%s name=%s", dev.DeviceID, dev.IP, dev.DeviceType, dev.DeviceName)
		}
	}
	s.configManager.SortDeviceListByIP()

	currentList := s.configManager.GetDeviceList()
	pendingCount := s.configManager.PendingDevicesCount()
	log.Printf("[discover] 设备列表: %d 台, 待添加: %d 台", len(currentList), pendingCount)

	// 推送设备列表数据给前端
	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("devicelist:updated", currentList)
		if pendingCount > 0 {
			s.app.Event.Emit("pendinglist:updated", s.configManager.GetPendingDevices())
		}
	}

	// 发现完成后立即触发 eventws 对账（新设备建连）
	if s.eventwsSyncer != nil {
		s.eventwsSyncer()
	}

	// 授权/绑定查询已从发现链路移除（用户决策 2026-09-03）：
	// 改为添加设备时查新设备（AddPendingDevices，并发限 4）。
	// 旧的同步绑定查询与 refreshBindAndSlot 全量坑位查询会让刷新按钮
	// 转 17s+（外网慢时更久）且持续打点授权 API。
	//
	// 例外：登录时那次全量绑定查询只覆盖「当时已在列表」的设备，登录后才被
	// 发现/添加的设备要等 30 分钟周期。这里做一次「限频 + 只查未覆盖设备」的
	// 异步补查（不阻塞发现、不拖慢刷新按钮），把 bind_status 尽快落进缓存。
	s.refreshBindStatusForDevices(s.deviceIDsForBind())
	return nil
}

// discoverFromCloud public 模式：两阶段加载。
// 阶段1（同步）：网关 ListDevices → Online=true 设备立即全部显示（型号先复用旧值或留空）。
// 阶段2（异步）：对每台调透传 /info/device，通=在线(State 0)+补型号，不通=离线(State 1，不删除)，逐台 emit 更新。
// 这样用户切到 public 能立即看到设备列表，再逐台刷新在线状态。
// 返回网关登录/拉取失败原因（手动刷新路径回传前端；阶段2 异步探测失败只记日志）。
func (s *DiscoverServiceWrapper) discoverFromCloud(start time.Time, gen int64) error {
	if s.sdkApiSvc == nil {
		log.Printf("[discover] public 模式但 SdkApiService 未注入，跳过")
		s.emitDiscoverError("公网服务未初始化")
		return fmt.Errorf("公网服务未初始化")
	}

	if err := s.sdkApiSvc.EnsureCloudLogin(); err != nil {
		log.Printf("[discover] public 网关登录失败: %v", err)
		s.emitDiscoverError("网关登录失败: " + err.Error())
		return fmt.Errorf("网关登录失败: %w", err)
	}
	client := s.sdkApiSvc.CloudClient()
	devices, total, err := client.ListDevicesWithTotal()
	if err != nil {
		log.Printf("[discover] public 拉取设备列表失败: %v, 耗时: %v", err, time.Since(start))
		s.emitDiscoverError("拉取公网设备列表失败: " + err.Error())
		return fmt.Errorf("拉取公网设备列表失败: %w", err)
	}

	// 阶段1 候选 = 网关返回的全部设备（含 online=false）：
	// online=false 只说明设备侧此刻断连，不等于「已下架」——它若已在本地列表中，
	// 必须保留（State 置离线），否则一次网关抖动就把设备从列表抹掉，5s 后落档写盘，
	// 用户看到设备消失。真正下架（网关列表里不再出现）仍会被 buildPublicDeviceList 移除。
	// 只有网关返回空（total==0，账号下确实没有设备）才整体清空。
	if total == 0 && len(devices) == 0 {
		if atomic.LoadInt64(&s.generation) != gen || s.configManager.GetDeployMode() != "public" {
			log.Printf("[discover] 模式已切换，丢弃本轮 public 空列表结果")
			return nil
		}
		log.Printf("[discover] public 网关返回空列表（total=0），清空公网设备列表")
		s.configManager.SetDeviceListAllowEmpty(nil)
		if s.app != nil && s.app.Event != nil {
			s.app.Event.Emit("devicelist:updated", []config.DeviceCache{})
		}
		if s.onDeviceListUpdated != nil {
			s.onDeviceListUpdated()
		}
		return nil
	}

	// 阶段2 的探测目标：仅 online=true 的设备（离线设备透传探测必然失败，白打点网关）
	candidates := make([]cloudapi.GatewayDevice, 0, len(devices))
	for _, dev := range devices {
		if dev.Online {
			candidates = append(candidates, dev)
		}
	}
	log.Printf("[discover] public 拉取完成: 共 %d 台（在线 %d 台）, 耗时: %v", len(devices), len(candidates), time.Since(start))

	newList := buildPublicDeviceList(devices, s.configManager.GetDeviceList())
	// 代际 + 模式双重校验（与 local 分支对齐）：仅比 generation 不够——
	// SetDeployMode 里 mode 先落定、bump 紧随其后，若拉取恰在这段窗口内完成，
	// generation 仍相等，而 SetDeviceList 已按新模式取指针，公网列表会被写进
	// local 的文件（切模式丢列表的根因）。mode 不等即判定为过期结果直接丢弃。
	if atomic.LoadInt64(&s.generation) != gen || s.configManager.GetDeployMode() != "public" {
		log.Printf("[discover] 模式已切换，丢弃本轮 public 拉取结果 (%d 台)", len(newList))
		return nil
	}
	s.configManager.SetDeviceList(newList)
	s.configManager.SortDeviceListByIP()
	currentList := s.configManager.GetDeviceList()
	log.Printf("[discover] public 阶段1: 立即显示 %d 台候选设备", len(currentList))
	if s.app != nil && s.app.Event != nil {
		s.app.Event.Emit("devicelist:updated", currentList)
	}
	if s.onDeviceListUpdated != nil {
		s.onDeviceListUpdated()
	}

	// 绑定状态补查：public 的 bind_status 只由 30 分钟全量轮询写入，
	// 后加入列表的设备要等一整个周期——这里限频补查（只查未覆盖的设备）。
	s.refreshBindStatusForDevices(s.deviceIDsForBind())

	// ── 阶段2：异步探测每台设备补全型号（/info/device 响应带主板型号）──
	// 在线性不在此判定——与本地逻辑同步：状态唯一来源是 eventws
	// （Sync 拨号即探测：拨通+snapshot=在线，拨号失败=离线，8s 超时）。
	// 透传探测对慢网关会 60s 挂起，若写状态会与 WS 心跳打架造成在线↔离线翻转。
	// 型号没变的设备不 emit（避免 30s 一轮 × N 台的 devicelist:updated 风暴）。
	go func(gen int64) {
		const probeWorkers = 8
		taskCh := make(chan cloudapi.GatewayDevice, len(candidates))
		for _, dev := range candidates {
			taskCh <- dev
		}
		close(taskCh)

		var wg sync.WaitGroup
		for i := 0; i < probeWorkers && i < len(candidates); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for dev := range taskCh {
					// 取任务前即做代际 + 模式双校验：切到 local 后旧轮次的 worker
					// 若只比 generation，仍会对整批公网设备逐台发起透传探测
					// （慢网关单台可挂 60s），既打点网关又拖住 goroutine 退出。
					if atomic.LoadInt64(&s.generation) != gen || s.configManager.GetDeployMode() != "public" {
						return // 模式已切换，停止探测
					}
					pwd := getDevicePasswordSafe(s, dev.DeviceID)
					deviceType := fetchDeviceTypeFromCloud(client, dev.DeviceID, pwd)
					// 探测是慢操作（慢网关可 60s）：写回型号前同样做代际 + 模式双校验，
					// 否则切到 local 后仍会把公网设备的型号补丁写进本地列表
					// （SetDeviceType 按当前模式取指针，与 SetDeviceList 同一竞态）。
					if atomic.LoadInt64(&s.generation) != gen || s.configManager.GetDeployMode() != "public" {
						return
					}
					if deviceType == "" {
						log.Printf("[discover] public 探测失败: %s（型号未补全；在线性由 WS 判定）", dev.DeviceID)
						continue
					}
					// 型号有变化才 emit（型号补全是阶段2 唯一职责）
					if s.configManager.SetDeviceType(dev.DeviceID, deviceType) {
						if s.app != nil && s.app.Event != nil {
							s.app.Event.Emit("devicelist:updated", s.configManager.GetDeviceList())
						}
					}
				}
			}()
		}
		wg.Wait()
		log.Printf("[discover] public 阶段2: 探测完成")
	}(gen)
	return nil
}
