package main

import (
	"fmt"
	"log"

	"github.com/moyunteng/myt/internal/maintenance"
	"github.com/moyunteng/myt/internal/util"
)

// --- V2 盒子 SDK（myt-sdk-go-v2）自动部署 ---

// V2SdkDeployProgress SDK 部署进度事件负载
type V2SdkDeployProgress struct {
	DeviceIP string `json:"deviceIP"`
	Stage    string `json:"stage"`
	Message  string `json:"message"`
	Success  bool   `json:"success"`
	Error    string `json:"error,omitempty"`
}

// CheckV2Sdk 探测设备 8000 是否已有 SDK 服务（前端设备详情/手动诊断用）
func (s *V2ServiceWrapper) CheckV2Sdk(deviceID string) (bool, error) {
	dev := s.configManager.GetDevice(deviceID)
	if dev == nil || dev.IP == "" {
		return false, fmt.Errorf("设备不存在: %s", deviceID)
	}
	return maintenance.CheckV2SdkAlive(dev.IP), nil
}

// DeployV2Sdk 手动触发部署（前端调用）；自动路径走 maybeDeployV2Sdk
func (s *V2ServiceWrapper) DeployV2Sdk(deviceID string) error {
	dev := s.configManager.GetDevice(deviceID)
	if dev == nil || dev.IP == "" {
		return fmt.Errorf("设备不存在: %s", deviceID)
	}
	return s.deployV2SdkAsync(dev.IP)
}

// maybeDeployV2Sdk 添加设备/上线后的自动检测+部署入口（统一走自动安装状态机）：
//   - /info 可用（已是带 /info 的新版 SDK）→ 不管；版本是否最新交给现有显示/安装链路
//   - /info 不可用（老版本 SDK / 8000 无服务）→ shouldAttempt 决策后异步安装
//
// 防重入：同一 IP 部署进行中跳过（deployV2SdkAsync 的 sdkDeployInFlight 表 +
// SDK 安装链路的 installMu，见 v2_service.go Wrapper 定义与 sdkapi_service.go）。
// 进度经 v2sdk:deploy-progress 推送；失败只落日志，不影响设备正常使用。
func (s *V2ServiceWrapper) maybeDeployV2Sdk(ip string) {
	if s.configManager.GetDeployMode() != "v2" {
		return
	}
	if maintenance.CheckV2SdkAlive(ip) && maintenance.HasV2SdkInfo(ip) {
		log.Printf("[v2sdk] %s 8000 /info 可用，跳过部署", ip)
		return
	}
	s.maybeAutoInstallV2Sdk(ip, "deploy-check")
}

// v2SdkAutoState 单设备自动安装状态机（内存态，重启客户端后重新探测，语义自洽）。
type v2SdkAutoState struct {
	failCount  int  // 连续失败探测次数（每轮安装尝试开始时清零）
	round      int  // 已进行的安装尝试轮数（>=3 彻底放弃）
	abandoned  bool // 彻底放弃标记（round>=3 置位，手动安装成功可清）
	installing bool // 安装任务进行中（决策时排除重复触发）
}

// 自动安装重试策略常量：连续 5 次失败探测触发一次重装尝试，累计 3 轮后放弃
const (
	v2AutoFailPerRound = 5 // 每轮触发安装前需累计的失败探测次数
	v2AutoMaxRounds    = 3 // 最大安装尝试轮数，超过彻底放弃
)

// maybeAutoInstallV2Sdk 统一自动安装决策+触发入口（所有自动触发点汇聚于此，
// trigger 仅用于日志区分来源）。决策与计数递增在锁内原子完成：8 并发版本查询
// 同时失败也只会有一个触发安装，其余被 installing/窗口计数挡住。
func (s *V2ServiceWrapper) maybeAutoInstallV2Sdk(ip, trigger string) {
	s.sdkAutoMu.Lock()
	st := s.sdkAuto[ip]
	if st == nil {
		st = &v2SdkAutoState{}
		s.sdkAuto[ip] = st
	}
	action := "skip"
	switch {
	case st.abandoned:
		action = "skip(abandoned)"
	case st.installing:
		action = "skip(installing)"
	case st.failCount < v2AutoFailPerRound:
		st.failCount++ // 窗口内累计失败探测，未达阈值只计数不安装
		action = "count"
	case st.round >= v2AutoMaxRounds:
		st.abandoned = true
		action = "abandon"
	default:
		// 达到 5 次失败阈值 → 触发一轮安装：计数清零、轮数+1、占住 installing
		st.failCount = 0
		st.round++
		st.installing = true
		action = "install"
	}
	fail, round, abandoned := st.failCount, st.round, st.abandoned
	s.sdkAutoMu.Unlock()
	log.Printf("[v2sdk] 自动安装决策 ip=%s trigger=%s failCount=%d round=%d abandoned=%v action=%s",
		ip, trigger, fail, round, abandoned, action)
	if action != "install" {
		return
	}
	// 安装任务异步执行，绝不阻塞版本查询/心跳/添加主机等触发方。
	// 完成判定用 /info 实测而非 deployV2SdkAsync 的返回值：手动安装持锁时
	// 自动路径会「让位放弃」返回 nil，不能当作安装成功（误重置状态机）。
	// finishAutoInstall 经 defer 保证恰好执行一次：deployV2SdkAsync panic 时
	// 若不 recover 补救，installing 永久卡 true，该设备永远跳过自动安装。
	// stInstall 是决策时捕获的状态指针：手动安装成功会 ResetV2SdkAutoState
	// 删条目、后续触发又可能新建状态，finish 时校验身份一致才操作，
	// 避免旧任务的收尾误清/误重置新状态。
	stInstall := st
	util.GoSafe(func() {
		ok := false
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[v2sdk] %s 自动安装 goroutine panic: %v", ip, r)
			}
			s.finishAutoInstall(ip, stInstall, ok)
		}()
		_ = s.deployV2SdkAsync(ip)
		ok = maintenance.HasV2SdkInfo(ip)
	})
}

// finishAutoInstall 安装任务结束回调：成功重置整个状态机（下轮重新累计）；
// 失败释放 installing，等下一个 5 次失败窗口再试。
// stInstall 身份校验：仅当 map 里仍是决策时的那个状态才收尾（已被手动
// reset 删除或被并发触发重建时直接放弃，不碰新状态）。
func (s *V2ServiceWrapper) finishAutoInstall(ip string, stInstall *v2SdkAutoState, ok bool) {
	s.sdkAutoMu.Lock()
	defer s.sdkAutoMu.Unlock()
	st := s.sdkAuto[ip]
	if st == nil || st != stInstall {
		return
	}
	st.installing = false
	if ok {
		s.sdkAuto[ip] = &v2SdkAutoState{} // 成功重置（round/failCount/abandoned 归零）
	}
}

// ResetV2SdkAutoState 手动安装成功后重置状态机（sdkapi_service 调用）。
func (s *V2ServiceWrapper) ResetV2SdkAutoState(ip string) {
	s.sdkAutoMu.Lock()
	defer s.sdkAutoMu.Unlock()
	delete(s.sdkAuto, ip)
}

// deployV2SdkAsync 执行部署并推送进度事件（串行防重入）
func (s *V2ServiceWrapper) deployV2SdkAsync(ip string) error {
	s.sdkDeployMu.Lock()
	if s.sdkDeployInFlight[ip] {
		s.sdkDeployMu.Unlock()
		log.Printf("[v2sdk] %s 部署进行中，跳过重复触发", ip)
		return nil
	}
	s.sdkDeployInFlight[ip] = true
	s.sdkDeployMu.Unlock()
	defer func() {
		s.sdkDeployMu.Lock()
		delete(s.sdkDeployInFlight, ip)
		s.sdkDeployMu.Unlock()
	}()

	// 与 SDK 安装链路（InstallV2SdkVersion / V3 InstallSdkVersion）互斥，
	// 避免同时两路 SSH/上传操作同一设备；锁未注入或拿不到时放弃本次——设备
	// 上线/添加主机会周期性重触发（幂等），不必排队等待
	if s.installMu == nil || !s.installMu.TryLock() {
		log.Printf("[v2sdk] %s 有 SDK 安装任务执行中，放弃本次自动部署", ip)
		return nil
	}
	defer s.installMu.Unlock()

	emit := func(stage, msg string, success bool, errMsg string) {
		if s.app != nil && s.app.Event != nil {
			s.app.Event.Emit("v2sdk:deploy-progress", V2SdkDeployProgress{
				DeviceIP: ip, Stage: stage, Message: msg, Success: success, Error: errMsg,
			})
		}
	}
	log.Printf("[v2sdk] 开始部署 SDK 到 %s（版本号部署前动态查询）", ip)
	err := maintenance.EnsureV2SdkUpgraded(ip, func(stage, msg string) {
		emit(stage, msg, true, "")
	})
	if err != nil {
		log.Printf("[v2sdk] %s 部署失败: %v", ip, err)
		emit("done", "SDK 部署失败", false, err.Error())
		return err
	}
	log.Printf("[v2sdk] %s SDK 部署完成", ip)
	emit("done", "SDK 部署完成，8000 端口已就绪", true, "")
	return nil
}
