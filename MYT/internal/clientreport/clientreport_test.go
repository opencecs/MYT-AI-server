package clientreport

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moyunteng/myt/internal/config"
)

// memStore 内存版 StateStore：单测不依赖真实配置文件。
type memStore struct {
	mu    sync.Mutex
	state config.ReportState
	saves int
}

func (m *memStore) GetClientReportState() config.ReportState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

func (m *memStore) SaveClientReportState(s config.ReportState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = s
	m.saves++
	return nil
}

// fakeServer 假后端：记录收到的上报请求。
type fakeServer struct {
	mu       sync.Mutex
	requests []payload
	codeID   int
	recorded bool
	failHTTP bool
}

func (f *fakeServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		codeID, recorded, fail := f.codeID, f.recorded, f.failHTTP
		f.mu.Unlock()

		body, _ := io.ReadAll(r.Body)
		var p payload
		_ = json.Unmarshal(body, &p)

		f.mu.Lock()
		f.requests = append(f.requests, p)
		f.mu.Unlock()

		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if codeID == 0 {
			codeID = 200
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code_id": codeID,
			"msg":     "success",
			"data":    map[string]any{"recorded": recorded},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeServer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeServer) last() payload {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return payload{}
	}
	return f.requests[len(f.requests)-1]
}

func fixedNow(at time.Time) func() time.Time {
	return func() time.Time { return at }
}

var testDay = time.Date(2026, 9, 24, 12, 0, 0, 0, cst)

func newTestReporter(srv *httptest.Server, store *memStore, uid func() string, now func() time.Time) *Reporter {
	return New(Options{
		Store:    store,
		Version:  "1.5.2",
		OSName:   "Windows 11 Pro 23H2",
		UID:      uid,
		Enabled:  true,
		Endpoint: srv.URL + "/report",
		Now:      now,
	})
}

func TestNewClientIDIsTwelveUpperHex(t *testing.T) {
	id, err := newClientID()
	if err != nil {
		t.Fatalf("newClientID 出错: %v", err)
	}
	if len(id) != 12 {
		t.Fatalf("client_id 长度 = %d (%q)，期望 12", len(id), id)
	}
	for _, c := range id {
		if !strings.ContainsRune("0123456789ABCDEF", c) {
			t.Fatalf("client_id %q 含非大写十六进制字符 %q", id, c)
		}
	}
}

// 上报请求体必须字段齐全、语义正确（验收 A2）
func TestReportSendsFullPayload(t *testing.T) {
	f := &fakeServer{}
	srv := f.start(t)
	store := &memStore{}
	r := newTestReporter(srv, store, func() string { return "" }, fixedNow(testDay))

	r.report("startup")

	if f.count() != 1 {
		t.Fatalf("请求次数 = %d，期望 1", f.count())
	}
	got := f.last()
	if len(got.DeviceID) != 12 {
		t.Errorf("device_id = %q，期望 12 位 hex", got.DeviceID)
	}
	if got.UID != "" {
		t.Errorf("未登录时 uid = %q，期望空串", got.UID)
	}
	if got.IP != "" {
		t.Errorf("ip = %q，期望空串（不传 ip，由服务端按请求来源解析）", got.IP)
	}
	if got.OS != "Windows 11 Pro 23H2" {
		t.Errorf("os = %q", got.OS)
	}
	if got.ClientVersion != "1.5.2" {
		t.Errorf("client_version = %q", got.ClientVersion)
	}
	if got.ClientType != 2 {
		t.Errorf("client_type = %d，期望 2（AI 客户端）", got.ClientType)
	}
	if got.StartupTime != testDay.Unix() {
		t.Errorf("startup_time = %d，期望 %d", got.StartupTime, testDay.Unix())
	}
	if store.state.LastDate != "2026-09-24" {
		t.Errorf("本地落档日期 = %q，期望 2026-09-24", store.state.LastDate)
	}
}

// 当天已用相同 uid 上报过 → 不再发请求（验收 A3）
func TestSkipWhenSameDaySameUID(t *testing.T) {
	f := &fakeServer{}
	srv := f.start(t)
	store := &memStore{state: config.ReportState{
		ClientID: "A1B2C3D4E5F6", LastDate: "2026-09-24", LastUID: "10086",
	}}
	r := newTestReporter(srv, store, func() string { return "10086" }, fixedNow(testDay))

	r.report("startup")

	if f.count() != 0 {
		t.Fatalf("请求次数 = %d，期望 0（当天已上报过）", f.count())
	}
}

// 当天上报时未登录（uid 空），登录成功后补报一次（验收 A5）
func TestSupplementUIDAfterLogin(t *testing.T) {
	f := &fakeServer{}
	srv := f.start(t)
	store := &memStore{state: config.ReportState{
		ClientID: "A1B2C3D4E5F6", LastDate: "2026-09-24", LastUID: "",
	}}
	r := newTestReporter(srv, store, func() string { return "10086" }, fixedNow(testDay))

	r.report("login")

	if f.count() != 1 {
		t.Fatalf("请求次数 = %d，期望 1（补 uid）", f.count())
	}
	if got := f.last().UID; got != "10086" {
		t.Errorf("补报 uid = %q，期望 10086", got)
	}
	if store.state.LastUID != "10086" {
		t.Errorf("落档 uid = %q，期望 10086", store.state.LastUID)
	}
}

// 上报失败不落日期，下一轮可重试（验收 A4）
func TestFailureKeepsDateAndRetries(t *testing.T) {
	f := &fakeServer{failHTTP: true}
	srv := f.start(t)
	store := &memStore{}
	r := newTestReporter(srv, store, func() string { return "" }, fixedNow(testDay))

	r.report("startup")

	if f.count() != 1 {
		t.Fatalf("首次请求次数 = %d，期望 1", f.count())
	}
	if store.state.LastDate != "" {
		t.Fatalf("失败后不应落日期，实际 %q", store.state.LastDate)
	}

	f.mu.Lock()
	f.failHTTP = false
	f.mu.Unlock()

	r.report("daily")

	if f.count() != 2 {
		t.Fatalf("重试后请求次数 = %d，期望 2", f.count())
	}
	if store.state.LastDate != "2026-09-24" {
		t.Errorf("重试成功后日期 = %q，期望 2026-09-24", store.state.LastDate)
	}
}

// recorded=false（服务端去重命中）仍算本次成功
func TestRecordedFalseStillCountsAsReported(t *testing.T) {
	f := &fakeServer{recorded: false}
	srv := f.start(t)
	store := &memStore{}
	r := newTestReporter(srv, store, func() string { return "10086" }, fixedNow(testDay))

	r.report("startup")
	r.report("startup")

	if f.count() != 1 {
		t.Fatalf("请求次数 = %d，期望 1（第二次应跳过）", f.count())
	}
	if store.state.LastUID != "10086" {
		t.Errorf("落档 uid = %q", store.state.LastUID)
	}
}

// 跨天触发上报，且 device_id 保持不变（验收 A1 + 日活）
func TestCrossDayReportsWithStableClientID(t *testing.T) {
	f := &fakeServer{}
	srv := f.start(t)
	store := &memStore{}

	day1 := time.Date(2026, 9, 24, 23, 59, 0, 0, cst)
	day2 := time.Date(2026, 9, 25, 0, 1, 0, 0, cst)
	current := day1
	now := func() time.Time { return current }
	r := newTestReporter(srv, store, func() string { return "10086" }, now)

	r.report("startup")
	if f.count() != 1 {
		t.Fatalf("首日请求次数 = %d，期望 1", f.count())
	}

	r.report("daily") // 同一天，应跳过
	if f.count() != 1 {
		t.Fatalf("同日重复调用后请求次数 = %d，期望仍为 1", f.count())
	}

	current = day2
	r.report("daily") // 跨天，应上报
	if f.count() != 2 {
		t.Fatalf("跨天后请求次数 = %d，期望 2", f.count())
	}

	first, second := f.requests[0], f.requests[1]
	if first.DeviceID != second.DeviceID {
		t.Errorf("device_id 跨天发生变化: %q → %q", first.DeviceID, second.DeviceID)
	}
	if store.state.LastDate != "2026-09-25" {
		t.Errorf("落档日期 = %q，期望 2026-09-25", store.state.LastDate)
	}
}

// 业务失败（code_id != 200）不落日期
func TestBusinessErrorKeepsDate(t *testing.T) {
	f := &fakeServer{codeID: 500}
	srv := f.start(t)
	store := &memStore{}
	r := newTestReporter(srv, store, func() string { return "" }, fixedNow(testDay))

	r.report("startup")

	if store.state.LastDate != "" {
		t.Errorf("业务失败后日期 = %q，期望空", store.state.LastDate)
	}
}

// Enabled=false 时不发任何请求，Start/Stop 也不 panic（能力开关分支）
func TestDisabledReporterDoesNothing(t *testing.T) {
	f := &fakeServer{}
	srv := f.start(t)
	store := &memStore{}
	r := New(Options{
		Store:    store,
		Version:  "dev",
		Enabled:  false,
		Endpoint: srv.URL + "/report",
		Now:      fixedNow(testDay),
	})

	r.Start()
	r.ReportNow("login")
	r.Stop()

	if f.count() != 0 {
		t.Errorf("禁用时请求次数 = %d，期望 0", f.count())
	}
	if store.saves != 0 {
		t.Errorf("禁用时不应写本地状态，实际写入 %d 次", store.saves)
	}
}

// 首次运行生成并落盘 client_id，之后复用（验收 A1）
func TestClientIDGeneratedOnceAndReused(t *testing.T) {
	f := &fakeServer{}
	srv := f.start(t)
	store := &memStore{}
	r := newTestReporter(srv, store, func() string { return "" }, fixedNow(testDay))

	r.report("startup")

	if len(store.state.ClientID) != 12 {
		t.Fatalf("落档 client_id = %q，期望 12 位 hex", store.state.ClientID)
	}
	saved := store.state.ClientID

	// 换一个 Reporter 实例（模拟重启）应复用同一 client_id
	r2 := newTestReporter(srv, store, func() string { return "" }, fixedNow(time.Date(2026, 9, 25, 8, 0, 0, 0, cst)))
	r2.report("startup")

	if got := store.state.ClientID; got != saved {
		t.Errorf("重启后 client_id = %q，期望复用 %q", got, saved)
	}
	if f.count() != 2 {
		t.Fatalf("请求次数 = %d，期望 2", f.count())
	}
	if f.requests[0].DeviceID != f.requests[1].DeviceID {
		t.Errorf("两次上报 device_id 不一致: %q vs %q", f.requests[0].DeviceID, f.requests[1].DeviceID)
	}
}

// Start/Stop 生命周期不阻塞、可重复调用
func TestStartStopIsIdempotent(t *testing.T) {
	f := &fakeServer{}
	srv := f.start(t)
	store := &memStore{}
	r := newTestReporter(srv, store, func() string { return "" }, fixedNow(testDay))

	r.Start()
	r.Stop()
	r.Stop() // 幂等，不应 panic

	if f.count() != 1 {
		t.Errorf("请求次数 = %d，期望 1（启动上报一次）", f.count())
	}
}

// 并发触发（启动上报与登录补报几乎同时）必须串行化，不能重复上报
func TestConcurrentReportsAreSerialized(t *testing.T) {
	f := &fakeServer{}
	srv := f.start(t)
	store := &memStore{}
	r := newTestReporter(srv, store, func() string { return "10086" }, fixedNow(testDay))

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.report("startup")
		}()
	}
	wg.Wait()

	if f.count() != 1 {
		t.Fatalf("并发上报请求次数 = %d，期望 1", f.count())
	}
}
