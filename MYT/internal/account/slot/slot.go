package slot

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/util"
)

// expireWarnSeconds 即将到期警告阈值（3天）
const expireWarnSeconds = 3 * 24 * 3600

type SlotInfo struct {
	DeviceID string `json:"device_id"`
	Slots    []Slot `json:"slots"`
}

type Slot struct {
	Index      int    `json:"index"`
	ExpireTime string `json:"expire_time"`
	Status     string `json:"status"`
	State      int    `json:"state"` // 0=正常, 1=即将到期(3天内), 2=已过期
}

type SlotQueryResult struct {
	DeviceID string `json:"device_id"`
	Slots    []Slot `json:"slots"`
	Error    string `json:"error,omitempty"`
}

type slotRequest struct {
	Host string `json:"host"`
	Ts   int64  `json:"_ts"`
	Sign string `json:"_sign"`
}

type slotAPIDevice struct {
	Rabbet string            `json:"rabbet"`
	SNum   json.Number      `json:"snum"`
	Child  map[string]string `json:"child"`
}

type slotAPIResponse struct {
	Code any             `json:"code"`
	Data []slotAPIDevice `json:"data"`
}

type Service struct {
	mu            sync.RWMutex
	configManager *config.Manager
	apiURL        string
	salt          string
	stopCh        chan struct{}
	wg            sync.WaitGroup
	running       bool
	client        *http.Client
}

func NewService() *Service {
	return &Service{
		configManager: nil,
		apiURL:        "https://www.moyunteng.com/api/api.php",
		// 此盐含前导 '#'（双 #），与壳子权威配置 API_SIGN_KEY 不同，但该接口实测可用，勿改
		salt:          "#@#1234A98413G=--..234",
		stopCh:        make(chan struct{}),
		client:        util.NewHTTPClient(),
	}
}

func (s *Service) SetConfigManager(cm *config.Manager) {
	s.configManager = cm
}

func (s *Service) QuerySlotInfo(deviceIDs []string) []SlotQueryResult {
	log.Printf("[slot] 开始查询坑位: %d 台设备", len(deviceIDs))
	results := make([]SlotQueryResult, len(deviceIDs))
	allExpiry := make(map[string][]config.SlotExpiry)

	type queryOutcome struct {
		idx      int
		deviceID string
		slots    []Slot
		err      error
	}

	// 授权 API 查询并发上限：固定小并发（用户决策 2026-09-03——
	// 旧实现 ParallelWorkers("io") = CPU/2（8+），大量设备时瞬间打满外网 API）
	const maxQueryWorkers = 4

	taskCh := make(chan int, len(deviceIDs))
	outCh := make(chan queryOutcome, len(deviceIDs))

	for i := range deviceIDs {
		taskCh <- i
	}
	close(taskCh)

	var wg sync.WaitGroup
	for i := 0; i < maxQueryWorkers && i < len(deviceIDs); i++ {
		wg.Add(1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[PANIC] slot worker recovered: %v", r)
				}
			}()
			defer wg.Done()
			for idx := range taskCh {
				slots, err := s.querySingle(deviceIDs[idx])
				outCh <- queryOutcome{idx: idx, deviceID: deviceIDs[idx], slots: slots, err: err}
			}
		}()
	}
	wg.Wait()
	close(outCh)

	for o := range outCh {
		if o.err != nil {
			results[o.idx] = SlotQueryResult{DeviceID: o.deviceID, Error: o.err.Error()}
			continue
		}
		slotExpiryList := make([]config.SlotExpiry, len(o.slots))
		for i, sl := range o.slots {
			slotExpiryList[i] = config.SlotExpiry{
				Index:      sl.Index,
				ExpireTime: sl.ExpireTime,
				State:      sl.State,
			}
		}
		allExpiry[o.deviceID] = slotExpiryList
		results[o.idx] = SlotQueryResult{DeviceID: o.deviceID, Slots: o.slots}
	}

	if s.configManager != nil && len(allExpiry) > 0 {
		s.configManager.MergeSlotExpiry(allExpiry)
	}

	return results
}

func computeExpireState(expireTime string) int {
	if expireTime == "0" || expireTime == "" {
		return 2
	}
	ts, err := strconv.ParseInt(expireTime, 10, 64)
	if err != nil || ts == 0 {
		return 2
	}
	now := time.Now().Unix()
	if ts < now {
		return 2
	}
	if ts-now < int64(expireWarnSeconds) {
		return 1
	}
	return 0
}

func expireStateToStatus(state int) string {
	switch state {
	case 0:
		return "正常"
	case 1:
		return "即将到期"
	case 2:
		return "已过期"
	default:
		return "未知"
	}
}

func (s *Service) querySingle(deviceID string) ([]Slot, error) {
	ts := time.Now().Unix()

	hostJSON, err := json.Marshal([]string{deviceID})
	if err != nil {
		return nil, fmt.Errorf("序列化设备ID失败: %w", err)
	}
	signStr := fmt.Sprintf("%d#%s%s", ts, string(hostJSON), s.salt)
	sign := util.MD5Hash(signStr)

	reqData := slotRequest{
		Host: string(hostJSON),
		Ts:   ts,
		Sign: sign,
	}
	jsonData, err := json.Marshal(reqData)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	params := url.Values{}
	params.Set("type", "term_info_nologn")
	params.Set("data", string(jsonData))

	body, err := util.MoyuntengPost(s.client, s.apiURL, params)
	if err != nil {
		return nil, fmt.Errorf("查询坑位失败: %s", err)
	}

	var apiResp slotAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("解析坑位响应失败: %w", err)
	}

	if !util.IsSuccessCode(apiResp.Code) {
		return nil, fmt.Errorf("坑位API错误: code=%v", apiResp.Code)
	}

	// API 返回的是设备维度，每个设备有 child map
	if len(apiResp.Data) == 0 {
		return nil, nil
	}

	slots := apiDeviceToSlots(apiResp.Data[0])
	log.Printf("[slot] 查询结果: device=%s slots=%d", deviceID[:12], len(slots))
	return slots, nil
}

func apiDeviceToSlots(dev slotAPIDevice) []Slot {
	// child map 的 key 是坑位序号(1-based)，value 是到期时间戳
	keys := make([]int, 0, len(dev.Child))
	for k := range dev.Child {
		idx, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		keys = append(keys, idx)
	}
	sort.Ints(keys)

	slots := make([]Slot, 0, len(keys))
	for _, idx := range keys {
		expireTime := dev.Child[strconv.Itoa(idx)]
		state := computeExpireState(expireTime)
		slots = append(slots, Slot{
			Index:      idx, // 保持 1-based，与 AndroidInfo.IndexNum 一致
			ExpireTime: expireTime,
			Status:     expireStateToStatus(state),
			State:      state,
		})
	}
	return slots
}

func (s *Service) GetCachedSlots(deviceID string) []Slot {
	if s.configManager != nil {
		expiryList := s.configManager.GetDeviceSlotExpiry(deviceID)
		if len(expiryList) > 0 {
			slots := make([]Slot, len(expiryList))
			for i, e := range expiryList {
				slots[i] = Slot{
					Index:      e.Index,
					ExpireTime: e.ExpireTime,
					Status:     expireStateToStatus(e.State),
					State:      e.State,
				}
			}
			return slots
		}
	}
	return nil
}

// StartAutoQuery 低频兜底刷新（30 分钟全量一轮，内部并发限 4）：
// 坑位到期是天级变化数据，30 分钟粒度足够新鲜。添加设备时的增量查询
// （queryAuthForNewDevices）负责新设备立即生效，本定时任务负责存量设备
// 的到期状态随时间推进（"即将到期/已过期"标签翻转）。
func (s *Service) StartAutoQuery(getAllDeviceIDs func() []string) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.stopCh = make(chan struct{})
	s.running = true
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[PANIC] goroutine recovered: %v", r)
			}
		}()
		defer s.wg.Done()

		// 启动延迟 2 分钟再查首轮：错开启动高峰（eventws 建连/截图首轮），
		// 且避免与「添加设备时的增量查询」在启动阶段重复
		select {
		case <-s.stopCh:
			return
		case <-time.After(2 * time.Minute):
		}
		if ids := getAllDeviceIDs(); len(ids) > 0 {
			s.QuerySlotInfo(ids)
		}

		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				if ids := getAllDeviceIDs(); len(ids) > 0 {
					s.QuerySlotInfo(ids)
				}
			}
		}
	}()
}

func (s *Service) StopAutoQuery() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}
	close(s.stopCh)
	s.running = false
	// 等待 goroutine 退出
	s.wg.Wait()
}

// GetSlotCount 根据设备ID前缀返回坑位数量：P系列=24，其他=12
func (s *Service) GetSlotCount(deviceID string) int {
	if strings.HasPrefix(strings.ToUpper(deviceID), "P") {
		return 24
	}
	return 12
}
