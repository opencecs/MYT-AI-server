package main

import (
	"log"
	"time"

	"github.com/moyunteng/myt/internal/screencast"
)

// publicSession 公网投屏会话：RTSP 端口组 + 启动模板 + 过期时刻。
// 端口组 600s 过期，由刷新器主动续期；异常退出时被动重启（双保险）。
type publicSession struct {
	ref      screencast.SlotRef
	cfg      screencast.ProjectionConfig
	expireAt time.Time
	// refreshAt 最近一次刷新/重启时刻；15s 内的退出事件视为"预期退出"（进程替换），
	// 不触发注销/兜底，避免刷新杀旧进程与退出回调互相干扰。
	refreshAt   time.Time
	matrixItems []BatchProjectionItem // 矩阵主控：全部设备坑位（刷新时重拉所有端口）
	isMatrix    bool
	// refreshing 刷新进行中标记（publicMu 内读写）：刷新入口并发防护——
	// 5s tick 的 due 判断在 expireAt 更新前会反复触发，退出兜底也走同一入口，
	// 无防护时两轮并发 Stop+Start 会开出重复窗口。
	refreshing bool
}

// StartPublicRefresher 启动公网投屏端口过期刷新器（主动保险）。
// 5s tick 扫描 publicSessions，到期前 60s 重拉端口并重启窗口。
func (s *ScreencastServiceWrapper) StartPublicRefresher() {
	s.publicMu.Lock()
	defer s.publicMu.Unlock()
	if s.refresherStop != nil {
		return
	}
	s.refresherStop = make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.refreshPublicDue()
			case <-s.refresherStop:
				return
			}
		}
	}()
}

// StopPublicRefresher 停止刷新器
func (s *ScreencastServiceWrapper) StopPublicRefresher() {
	s.publicMu.Lock()
	defer s.publicMu.Unlock()
	if s.refresherStop != nil {
		close(s.refresherStop)
		s.refresherStop = nil
	}
}

// registerPublicSession 注册公网投屏会话（启动成功后调用）
func (s *ScreencastServiceWrapper) registerPublicSession(ref screencast.SlotRef, cfg screencast.ProjectionConfig, expiresIn int, matrixItems []BatchProjectionItem, isMatrix bool) {
	if expiresIn <= 0 {
		expiresIn = 600
	}
	s.publicMu.Lock()
	s.publicSessions[ref.String()] = &publicSession{
		ref:         ref,
		cfg:         cfg,
		expireAt:    time.Now().Add(time.Duration(expiresIn) * time.Second),
		matrixItems: matrixItems,
		isMatrix:    isMatrix,
	}
	s.publicMu.Unlock()
}

// refreshPublicDue 扫描到期（60s 内）的会话并刷新
func (s *ScreencastServiceWrapper) refreshPublicDue() {
	now := time.Now()
	s.publicMu.Lock()
	var due []string
	for key, sess := range s.publicSessions {
		if now.Add(60 * time.Second).After(sess.expireAt) {
			due = append(due, key)
		}
	}
	s.publicMu.Unlock()
	for _, key := range due {
		go s.refreshPublicSessionByKey(key)
	}
}

// refreshPublicSessionByKey 重拉端口组并重启投屏窗口（幂等：进程已死则只开新的）。
// 主动刷新与被动兜底共用此入口。并发防护：已在刷新中则跳过（5s tick 在
// expireAt 更新前会重复触发 + 退出兜底同入口，无防护会并发 Stop+Start 双开窗口）。
func (s *ScreencastServiceWrapper) refreshPublicSessionByKey(key string) {
	s.publicMu.Lock()
	sess, ok := s.publicSessions[key]
	if !ok || sess.refreshing {
		s.publicMu.Unlock()
		return
	}
	sess.refreshing = true
	sess.refreshAt = time.Now()
	s.publicMu.Unlock()
	defer func() {
		s.publicMu.Lock()
		if _, still := s.publicSessions[key]; still {
			s.publicSessions[key].refreshing = false
		}
		s.publicMu.Unlock()
	}()

	if sess.isMatrix {
		s.refreshMatrixSession(sess)
		return
	}

	mp, err := s.publicMgr.GetMobilePorts(sess.ref.DeviceIP, sess.ref.IndexNum)
	if err != nil {
		log.Printf("[screencast] public refresh: 拉端口失败 %s: %v（下轮重试）", key, err)
		return
	}
	s.svc.StopProjection(sess.ref) // 进程存活则等待其退出；已退出则 no-op
	cfg := sess.cfg
	cfg.RTSPParams.DeviceIP = mp.VideoHost
	cfg.RTSPParams.VideoPort = mp.VideoPort
	cfg.RTSPParams.ControlPort = mp.ControlPort
	if err := s.svc.StartProjectionWithConfig(sess.ref, cfg); err != nil {
		log.Printf("[screencast] public refresh: 重启失败 %s: %v", key, err)
		s.publicMu.Lock()
		delete(s.publicSessions, key)
		s.publicMu.Unlock()
		return
	}
	sess.cfg = cfg
	sess.expireAt = time.Now().Add(time.Duration(mp.ExpiresIn) * time.Second)
	log.Printf("[screencast] public refreshed: %s → %s:%d", key, mp.VideoHost, mp.VideoPort)
}

// refreshMatrixSession 矩阵主控刷新：重拉全部（主控+被控）端口，重启主控窗口
func (s *ScreencastServiceWrapper) refreshMatrixSession(sess *publicSession) {
	if len(sess.matrixItems) == 0 {
		return
	}
	s.svc.StopProjection(sess.ref) // 先停旧主控

	rtspParams, matrixParams, expiresIn, errs := s.fetchPublicMatrixPorts(sess.matrixItems)
	if rtspParams.VideoPort == 0 {
		log.Printf("[screencast] public matrix refresh: 拉端口失败（下轮重试）: %v", errs)
		return
	}
	cfg := sess.cfg
	cfg.RTSPParams = rtspParams
	cfg.CtrlList = matrixParams.CtrlList
	cfg.AllCtrlList = matrixParams.AllCtrlList
	cfg.SelfAddr = matrixParams.SelfAddr
	if err := s.svc.StartProjectionWithConfig(sess.ref, cfg); err != nil {
		log.Printf("[screencast] public matrix refresh: 重启失败: %v", err)
		s.publicMu.Lock()
		delete(s.publicSessions, sess.ref.String())
		s.publicMu.Unlock()
		return
	}
	sess.cfg = cfg
	sess.expireAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	log.Printf("[screencast] public matrix refreshed: primary=%s controlled=%d", sess.ref.String(), countCtrlTargets(cfg.CtrlList))
}

// stopPublicMatrixPrimary 停掉当前公网矩阵主控窗口（注销会话 + 杀进程）。
// 返回是否停掉了窗口（供调用方决定是否需要等待进程退出）。
func (s *ScreencastServiceWrapper) stopPublicMatrixPrimary() {
	s.publicMu.Lock()
	var oldRef screencast.SlotRef
	for key, sess := range s.publicSessions {
		if sess.isMatrix {
			oldRef = sess.ref
			delete(s.publicSessions, key)
		}
	}
	s.publicMu.Unlock()
	if oldRef.DeviceIP == "" {
		return
	}
	s.svc.StopProjection(oldRef) // 等待进程退出（GPU 资源释放）
	time.Sleep(300 * time.Millisecond)
}

// clearPublicSessions 清空会话注册表（StopAllProjections 用）
func (s *ScreencastServiceWrapper) clearPublicSessions() {
	s.publicMu.Lock()
	s.publicSessions = make(map[string]*publicSession)
	s.publicMu.Unlock()
}
