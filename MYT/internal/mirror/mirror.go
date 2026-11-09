package mirror

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/util"
)

type Service struct {
	mu        sync.RWMutex
	cache     []util.MirrorItem
	apiURL    string
	interval  time.Duration
	stopCh    chan struct{}
	wg        sync.WaitGroup
	running   bool
	onRefresh func([]util.MirrorItem)
	// tokenGetter 注入当前登录 token（V1 镜像接口需要；未登录/未注入时降级 V2 匿名接口）
	tokenGetter func() (string, error)
}

func NewService() *Service {
	return &Service{
		apiURL:   "http://api.moyunteng.com/api.php",
		interval: 10 * time.Minute,
		stopCh:   make(chan struct{}),
	}
}

// SetTokenGetter 注入登录 token 提供者（setup.go 挂 paymentSvc.GetToken）。
// 与 internal/auth.SetTokenGetter 同款注入模式（mirror 不反向依赖 payment）。
func (s *Service) SetTokenGetter(fn func() (string, error)) {
	s.mu.Lock()
	s.tokenGetter = fn
	s.mu.Unlock()
}

var mirrorClient = &http.Client{Timeout: 15 * time.Second}

func (s *Service) FetchMirrors() ([]util.MirrorItem, error) {
	// 优先 V1 接口（壳子同款，带 type/ttype/sort——创建表单分类需要）；
	// 未登录或 V1 失败时降级 V2 匿名接口（字段少但可用）
	if s.tokenGetter != nil {
		if tok, err := s.tokenGetter(); err == nil && tok != "" {
			if items, err := s.fetchV1Mirrors(tok); err == nil {
				s.mu.Lock()
				s.cache = items
				s.mu.Unlock()
				return items, nil
			} else {
				log.Printf("V1 镜像接口失败（降级 V2）: %v", err)
			}
		}
	}
	return s.fetchV2Mirrors()
}

// fetchV1Mirrors 调 get_mirror_list（V1，壳子同款请求格式）。
// toAjaxRequest 格式：外层 type + data JSON（内嵌 token/spid/lang）。
func (s *Service) fetchV1Mirrors(token string) ([]util.MirrorItem, error) {
	inner, _ := json.Marshal(map[string]any{
		"type":  "get_mirror_list",
		"token": token,
		"spid":  0,
		"lang":  "zh-cn",
	})
	params := url.Values{}
	params.Set("type", "get_mirror_list")
	params.Set("data", string(inner))
	params.Set("lang", "zh-cn")

	resp, err := mirrorClient.PostForm(s.apiURL, params)
	if err != nil {
		return nil, fmt.Errorf("获取镜像列表失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取镜像响应失败: %w", err)
	}

	var apiResp struct {
		Code string            `json:"code"`
		Data []util.MirrorItem `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("解析镜像响应失败: %w", err)
	}
	if apiResp.Code != "200" {
		return nil, fmt.Errorf("镜像API返回错误码: %s", apiResp.Code)
	}
	return apiResp.Data, nil
}

func (s *Service) fetchV2Mirrors() ([]util.MirrorItem, error) {
	params := url.Values{}
	params.Set("type", "get_mirror_list2")

	resp, err := mirrorClient.PostForm(s.apiURL, params)
	if err != nil {
		return nil, fmt.Errorf("获取镜像列表失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取镜像响应失败: %w", err)
	}

	var apiResp struct {
		Code string            `json:"code"`
		Data []util.MirrorItem `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("解析镜像响应失败: %w", err)
	}

	if apiResp.Code != "200" {
		return nil, fmt.Errorf("镜像API返回错误码: %s", apiResp.Code)
	}

	// 全量保留到缓存；前端按 _v3 过滤 + sys_ver==5 分类
	s.mu.Lock()
	s.cache = apiResp.Data
	s.mu.Unlock()

	return apiResp.Data, nil
}

// hasV3Marker 判断 ttype2 中是否有任一项严格以 _v3 结尾（只认 _v3，其它 _v30 / _v3_cecs 不算）
func hasV3Marker(item util.MirrorItem) bool {
	for _, t := range item.TType2 {
		if strings.HasSuffix(strings.TrimSpace(t), "_v3") {
			return true
		}
	}
	return false
}

// IsEmulatorMirror 统一判定镜像是否为模拟器类型（供前端之外的后端逻辑复用）。
// 规则：ttype2 含 _v3 且（sys_ver=="5" 或 特质版）→ 模拟器；其余 → 容器。
func IsEmulatorMirror(item util.MirrorItem) bool {
	return hasV3Marker(item) && (item.SysVer == "5" || item.SysVerDes == "特质版")
}

// filterV3Mirrors 只保留 ttype2 含 _v3 后缀的镜像
func filterV3Mirrors(items []util.MirrorItem) []util.MirrorItem {
	var result []util.MirrorItem
	for _, item := range items {
		if hasV3Marker(item) {
			result = append(result, item)
		}
	}
	return result
}

// filterEmulatorMirrors 模拟器镜像：ttype2 含 _v3 且（sys_ver=="5" 或 特质版）
func filterEmulatorMirrors(items []util.MirrorItem) []util.MirrorItem {
	var result []util.MirrorItem
	for _, item := range items {
		if IsEmulatorMirror(item) {
			result = append(result, item)
		}
	}
	return result
}

// filterContainerMirrors 容器镜像：ttype2 含 _v3 且非模拟器类型
func filterContainerMirrors(items []util.MirrorItem) []util.MirrorItem {
	var result []util.MirrorItem
	for _, item := range items {
		if hasV3Marker(item) && !IsEmulatorMirror(item) {
			result = append(result, item)
		}
	}
	return result
}

// GetCachedEmulatorMirrors 返回缓存中的模拟器镜像（_v3 且 sys_ver=="5"）
func (s *Service) GetCachedEmulatorMirrors() []util.MirrorItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return filterEmulatorMirrors(s.cache)
}

// GetCachedContainerMirrors 返回缓存中的容器镜像（_v3 且 sys_ver!="5"）
func (s *Service) GetCachedContainerMirrors() []util.MirrorItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return filterContainerMirrors(s.cache)
}

// GetCachedV3Mirrors 返回缓存中所有带 _v3 标识的镜像（前端分类用）
func (s *Service) GetCachedV3Mirrors() []util.MirrorItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return filterV3Mirrors(s.cache)
}

func (s *Service) GetCachedMirrors() []util.MirrorItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cache
}

// SetCache 设置内存缓存（从 mirror_cache.json 加载）
func (s *Service) SetCache(items []util.MirrorItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = items
}

func (s *Service) StartAutoRefresh() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.stopCh = make(chan struct{})
	s.running = true
	hasCache := len(s.cache) > 0
	s.mu.Unlock()

	s.wg.Add(1)
	util.GoSafe(func() {
		defer s.wg.Done()
		// 无缓存时立即请求一次，有缓存则等定时器
		if !hasCache {
			if items, err := s.FetchMirrors(); err != nil {
				log.Printf("首次获取镜像列表失败: %v", err)
			} else {
				s.mu.RLock()
				fn := s.onRefresh
				s.mu.RUnlock()
				if fn != nil {
					fn(items)
				}
			}
		}

		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				if items, err := s.FetchMirrors(); err != nil {
					log.Printf("自动刷新镜像失败: %v", err)
				} else {
					s.mu.RLock()
					fn := s.onRefresh
					s.mu.RUnlock()
					if fn != nil {
						fn(items)
					}
				}
			}
		}
	})
}

func (s *Service) SetOnRefresh(fn func([]util.MirrorItem)) {
	s.mu.Lock()
	s.onRefresh = fn
	s.mu.Unlock()
}

func (s *Service) StopAutoRefresh() {
	s.mu.Lock()

	if !s.running {
		s.mu.Unlock()
		return
	}
	close(s.stopCh)
	s.running = false
	s.mu.Unlock()
	s.wg.Wait()
}
