package v2host

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
) // newSuperSdkTestClient 组装指向 httptest 2375/81 双假服务端的 Client。
// dockerSrv 处理 /info、/images/json、/images/create、/containers/*；
// sdkSrv 处理 81 的 dc_api（sdkReady=false 时拒绝连接语义由 handler 返回错误模拟）。
func newSuperSdkTestClient(t *testing.T, dockerSrv, sdkSrv *httptest.Server) *Client {
	t.Helper()
	c := NewClient()
	c.port = portOf(dockerSrv)
	c.sdkPort = portOf(sdkSrv)
	// 单测目标环回地址由调用方以 ip=127.0.0.1 传入
	return c
}

func portOf(srv *httptest.Server) int {
	idx := strings.LastIndex(srv.URL, ":")
	if idx < 0 {
		panic("bad test server url: " + srv.URL)
	}
	n := 0
	for _, r := range srv.URL[idx+1:] {
		if r < '0' || r > '9' {
			panic("bad port in " + srv.URL)
		}
		n = n*10 + int(r-'0')
	}
	return n
}

const testImage = SuperSdkImage

// superSdkFixture 可控的假 Docker 主机 + 81 服务
type superSdkFixture struct {
	docker      *httptest.Server
	sdk         *httptest.Server
	hasImage    atomic.Bool // /images/json 是否包含目标镜像
	imgPulls    atomic.Int32
	ctrCount    atomic.Int32 // /containers/create 成功次数
	ctrState    atomic.Pointer[string]
	starts      atomic.Int32
	removes     atomic.Int32 // DELETE /containers/{name} 次数
	ctrHasPorts atomic.Bool  // inspect 返回的容器是否带 20000 端口段（默认 true）
	sdkUp       atomic.Bool                 // 81 dc_api 是否就绪
	ctr409      atomic.Pointer[atomic.Bool] // 置 true 时 /containers/create 返回 409（容错分支用）
	lastCreate  atomic.Pointer[string]      // 最近一次 /containers/create 请求体
}

func newFixture(t *testing.T) *superSdkFixture {
	f := &superSdkFixture{}
	stopped := "exited"
	f.ctrState.Store(&stopped)
	f.ctrHasPorts.Store(true)

	dockerMux := http.NewServeMux()
	dockerMux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ID": "dockertest", "Name": "host", "Containers": 2})
	})
	dockerMux.HandleFunc("/images/json", func(w http.ResponseWriter, r *http.Request) {
		if f.hasImage.Load() {
			json.NewEncoder(w).Encode([]map[string]any{{"Id": "sha256:x", "RepoTags": []string{testImage}}})
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{})
	})
	dockerMux.HandleFunc("/images/create", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fromImage") != testImage {
			http.Error(w, "wrong fromImage", http.StatusBadRequest)
			return
		}
		f.imgPulls.Add(1)
		fmt.Fprintln(w, `{"status":"Pulling"}`)
		fmt.Fprintln(w, `{"status":"Download complete"}`)
		fmt.Fprintln(w, `{"status":"Status: Downloaded"}`)
	})
	dockerMux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		if f.ctrCount.Load() == 0 {
			json.NewEncoder(w).Encode([]map[string]any{})
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{{
			"Id":    "ctr1",
			"Names": []string{"/" + superSdkContainerName},
			"Image": testImage,
			"State": *f.ctrState.Load(),
		}})
	})
	dockerMux.HandleFunc("/containers/create", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") != superSdkContainerName {
			http.Error(w, "wrong name", http.StatusBadRequest)
			return
		}
		if p := f.ctr409.Load(); p != nil && p.Load() {
			// 模拟容器已存在（并发安装/残留）：409 Conflict，不计数
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"message":"Conflict. The container name "/myt_sdk" is already in use"}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		bodyStr := string(body)
		f.lastCreate.Store(&bodyStr)
		f.ctrHasPorts.Store(true) // 新建的容器视为带端口段
		f.ctrCount.Add(1)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"Id":"ctr1"}`)
	})
	dockerMux.HandleFunc("/containers/myt_sdk/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !f.ctrHasPorts.Load() {
			// 旧版客户端创建的容器：仅 5000→81，无 20000 端口段
			fmt.Fprint(w, `{"HostConfig":{"PortBindings":{"5000/tcp":[{"HostPort":"81"}]}}}`)
			return
		}
		bindings := map[string]any{
			"5000/tcp": []map[string]string{{"HostPort": "81"}},
		}
		for i := superSdkPortRangeStart; i < superSdkPortRangeStart+superSdkPortRangeCount; i++ {
			bindings[fmt.Sprintf("%d/tcp", i)] = []map[string]string{{"HostPort": fmt.Sprintf("%d", i)}}
			bindings[fmt.Sprintf("%d/udp", i)] = []map[string]string{{"HostPort": fmt.Sprintf("%d", i)}}
		}
		json.NewEncoder(w).Encode(map[string]any{"HostConfig": map[string]any{"PortBindings": bindings}})
	})
	dockerMux.HandleFunc("/containers/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/start") {
			f.starts.Add(1)
			running := "running"
			f.ctrState.Store(&running)
			// 模拟 mytsdk 启动后 81 服务就绪（容器 start 即回验可通过）
			f.sdkUp.Store(true)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodDelete {
			// 模拟删除容器：容器消失（下次 /containers/json 列表为空）
			f.removes.Add(1)
			f.ctrCount.Store(0)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "unexpected", http.StatusNotFound)
	})
	f.docker = httptest.NewServer(dockerMux)

	sdkMux := http.NewServeMux()
	for _, ep := range []string{"get", "list"} {
		sdkMux.HandleFunc("/dc_api/v1/"+ep+"/", func(w http.ResponseWriter, r *http.Request) {
			if !f.sdkUp.Load() {
				http.Error(w, "connection refused", http.StatusServiceUnavailable)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []map[string]any{{"id": "a1", "index": 1}}})
		})
	}
	f.sdk = httptest.NewServer(sdkMux)

	t.Cleanup(func() {
		f.docker.Close()
		f.sdk.Close()
	})
	return f
}

func TestEnsureSuperSdkAlreadyReady(t *testing.T) {
	f := newFixture(t)
	c := newSuperSdkTestClient(t, f.docker, f.sdk)
	f.sdkUp.Store(true)
	res, err := c.EnsureSuperSdk(context.Background(), "127.0.0.1", nil)
	if err != nil {
		t.Fatalf("EnsureSuperSdk: %v", err)
	}
	if !res.Ready || res.PulledImage || res.Created || res.Started {
		t.Errorf("幂等分支: got %+v, want Ready=true 其余 false", res)
	}
	if f.imgPulls.Load() != 0 || f.ctrCount.Load() != 0 {
		t.Errorf("已就绪不应拉镜像/建容器")
	}
}

func TestEnsureSuperSdkFullInstall(t *testing.T) {
	f := newFixture(t)
	c := newSuperSdkTestClient(t, f.docker, f.sdk)
	var stages []string
	res, err := c.EnsureSuperSdk(context.Background(), "127.0.0.1", func(stage, msg string) {
		stages = append(stages, stage)
	})
	if err != nil {
		t.Fatalf("EnsureSuperSdk: %v", err)
	}
	if !res.Ready || !res.PulledImage || !res.Created || !res.Started {
		t.Errorf("全量安装: got %+v", res)
	}
	if f.imgPulls.Load() != 1 {
		t.Errorf("镜像应拉取 1 次, got %d", f.imgPulls.Load())
	}
	if f.ctrCount.Load() != 1 {
		t.Errorf("容器应创建 1 次, got %d", f.ctrCount.Load())
	}
	if f.starts.Load() != 1 {
		t.Errorf("容器应启动 1 次, got %d", f.starts.Load())
	}
	if f.lastCreate.Load() == nil {
		t.Fatal("应捕获到容器创建请求体")
	}
	assertCreatePayloadPortRange(t, *f.lastCreate.Load())
	if len(stages) == 0 || stages[0] != "check" || stages[len(stages)-1] != "done" {
		t.Errorf("进度阶段异常: %v", stages)
	}
}

// assertCreatePayloadPortRange 校验容器创建请求体带 5000→81 + 20000-20050 端口段
// （TCP/UDP 各 51 条同号映射）与 ExposedPorts（对齐 LG.js createMytSdkContainers）。
func assertCreatePayloadPortRange(t *testing.T, body string) {
	t.Helper()
	var payload struct {
		HostConfig struct {
			PortBindings map[string]json.RawMessage `json:"PortBindings"`
		} `json:"HostConfig"`
		Config struct {
			ExposedPorts map[string]json.RawMessage `json:"ExposedPorts"`
		} `json:"Config"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("解析创建请求体: %v", err)
	}
	for _, proto := range []string{"tcp", "udp"} {
		for i := superSdkPortRangeStart; i < superSdkPortRangeStart+superSdkPortRangeCount; i++ {
			key := fmt.Sprintf("%d/%s", i, proto)
			bindings, ok := payload.HostConfig.PortBindings[key]
			if !ok {
				t.Errorf("PortBindings 缺 %s", key)
				continue
			}
			var arr []struct {
				HostPort string `json:"HostPort"`
			}
			if err := json.Unmarshal(bindings, &arr); err != nil || len(arr) != 1 {
				t.Errorf("%s 绑定格式异常: %s", key, bindings)
				continue
			}
			if arr[0].HostPort != fmt.Sprintf("%d", i) {
				t.Errorf("%s 应同号映射 HostPort=%d, got %s", key, i, arr[0].HostPort)
			}
			if _, ok := payload.Config.ExposedPorts[key]; !ok {
				t.Errorf("ExposedPorts 缺 %s", key)
			}
		}
	}
	if _, ok := payload.HostConfig.PortBindings["5000/tcp"]; !ok {
		t.Error("PortBindings 缺 5000/tcp（81 SDK 服务口）")
	}
}

func TestEnsureSuperSdkImageOnlyMissing(t *testing.T) {
	// 容器存在但已停止：只拉镜像 + 启动，不再创建
	f := newFixture(t)
	c := newSuperSdkTestClient(t, f.docker, f.sdk)
	f.ctrCount.Store(1)
	res, err := c.EnsureSuperSdk(context.Background(), "127.0.0.1", nil)
	if err != nil {
		t.Fatalf("EnsureSuperSdk: %v", err)
	}
	if !res.PulledImage || res.Created || !res.Started {
		t.Errorf("镜像缺失+容器已停: got %+v, want Pulled+Started 不 Created", res)
	}
	if f.ctrCount.Load() != 1 {
		t.Errorf("不应重复创建容器")
	}
}

func TestEnsureSuperSdkRunningContainerNoStart(t *testing.T) {
	// 镜像在、容器 running：只等 81 回验（此处 81 未就绪会报验证超时——
	// 用短 ctx 提前取消验证该分支不会重建容器）
	f := newFixture(t)
	c := newSuperSdkTestClient(t, f.docker, f.sdk)
	f.hasImage.Store(true)
	f.ctrCount.Store(1)
	running := "running"
	f.ctrState.Store(&running)
	ctx, cancel := context.WithTimeout(context.Background(), 500*1e6)
	defer cancel()
	_, err := c.EnsureSuperSdk(ctx, "127.0.0.1", nil)
	if err == nil {
		t.Fatal("81 未就绪应报验证超时")
	}
	if f.starts.Load() != 0 || f.ctrCount.Load() != 1 {
		t.Errorf("running 容器不应重启/重建: starts=%d ctr=%d", f.starts.Load(), f.ctrCount.Load())
	}
}

func TestAdmissionProbe(t *testing.T) {
	// 2375 可达 + 81 拒绝 → OK 且未就绪；81 恢复 → OK 且就绪
	f := newFixture(t)
	c := newSuperSdkTestClient(t, f.docker, f.sdk)
	r := c.AdmissionProbe(context.Background(), "127.0.0.1")
	if !r.OK || r.SuperSdkReady {
		t.Errorf("2375 可达 81 不可达: got %+v", r)
	}
	if r.SdkErr == "" {
		t.Error("81 失败应记录 SdkErr")
	}
	if r.HostID != "dockertest" {
		t.Errorf("HostID 应来自 2375 /info, got %q", r.HostID)
	}
	f.sdkUp.Store(true)
	r = c.AdmissionProbe(context.Background(), "127.0.0.1")
	if !r.OK || !r.SuperSdkReady || r.Containers != 1 {
		t.Errorf("81 就绪: got %+v", r)
	}
}

func TestEnsureSuperSdkRunningReadyShortCircuit(t *testing.T) {
	// 镜像在、容器 running、81 已就绪：整个流程只走幂等短路——不拉镜像、
	// 不建容器、不启动，直接 Ready=true
	f := newFixture(t)
	c := newSuperSdkTestClient(t, f.docker, f.sdk)
	f.hasImage.Store(true)
	f.ctrCount.Store(1)
	running := "running"
	f.ctrState.Store(&running)
	f.sdkUp.Store(true)
	res, err := c.EnsureSuperSdk(context.Background(), "127.0.0.1", nil)
	if err != nil {
		t.Fatalf("EnsureSuperSdk: %v", err)
	}
	if !res.Ready || res.PulledImage || res.Created || res.Started {
		t.Errorf("running+81就绪短路: got %+v, want Ready=true 其余 false", res)
	}
	if f.imgPulls.Load() != 0 || f.ctrCount.Load() != 1 || f.starts.Load() != 0 {
		t.Errorf("不应拉镜像/建容器/启动: pulls=%d ctr=%d starts=%d",
			f.imgPulls.Load(), f.ctrCount.Load(), f.starts.Load())
	}
}

func TestEnsureSuperSdkCreate409Fallback(t *testing.T) {
	// 创建容器返回 409（已存在，如并发安装/残留）：视为已存在继续 start，
	// 不报错，最终 81 回验通过
	f := newFixture(t)
	c := newSuperSdkTestClient(t, f.docker, f.sdk)
	flag409 := &atomic.Bool{}
	flag409.Store(true)
	f.ctr409.Store(flag409)
	// 409 意味着容器其实已存在（并发安装/残留）；start 照 fixture 语义把状态转
	// running 并触发 81 就绪，最终回验通过
	res, err := c.EnsureSuperSdk(context.Background(), "127.0.0.1", nil)
	if err != nil {
		t.Fatalf("EnsureSuperSdk: %v", err)
	}
	if !res.Ready || res.Created || !res.Started {
		t.Errorf("409 容错分支: got %+v, want Ready+Started 不 Created", res)
	}
	if f.starts.Load() != 1 {
		t.Errorf("409 后应继续启动容器 1 次, got %d", f.starts.Load())
	}
}

func TestSuperSdkReadyJsonFalseKept(t *testing.T) {	// 序列化语义：SuperSdkReady=false 不得被抹掉（json tag 无 omitempty）——
	// 前端靠 === false 判断「未装超级 SDK」，false 变 undefined 提示会静默失效
	data, err := json.Marshal(ProbeResult{IP: "1.2.3.4", OK: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"superSdkReady":false`) {
		t.Errorf("false 必须出现在 JSON 里, got %s", data)
	}
	ready, err := json.Marshal(ProbeResult{IP: "1.2.3.4", OK: true, SuperSdkReady: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(ready), `"superSdkReady":true`) {
		t.Errorf("true 必须出现在 JSON 里, got %s", ready)
	}
}

func TestEnsureSuperSdkRecreateMissingPortRange(t *testing.T) {
	// 旧容器（81 可达 + 缺 20000 端口段）：幂等短路降穿 → 删除重建 → 回验通过。
	// 这是「旧版客户端安装批次」的修复路径：容器在、81 在，但宿主看不见端口段。
	f := newFixture(t)
	c := newSuperSdkTestClient(t, f.docker, f.sdk)
	f.sdkUp.Store(true)              // 81 可达（短路判定需降穿）
	f.ctrCount.Store(1)              // 容器已存在
	f.ctrHasPorts.Store(false)       // 旧版安装：缺端口段
	res, err := c.EnsureSuperSdk(context.Background(), "127.0.0.1", nil)
	if err != nil {
		t.Fatalf("EnsureSuperSdk: %v", err)
	}
	if f.removes.Load() != 1 {
		t.Errorf("缺端口段应删除旧容器 1 次, got %d", f.removes.Load())
	}
	// ctrCount 语义：初始 Store(1) 是模拟「容器已存在」状态；删除归 0 后
	// 重建 create 一次 → 最终计数 1（即真实发生 1 次创建调用）
	if f.ctrCount.Load() != 1 || f.starts.Load() != 1 {
		t.Errorf("应重建容器（1 次创建 + 1 次启动）: ctr=%d starts=%d", f.ctrCount.Load(), f.starts.Load())
	}
	if !res.Ready || !res.Created || !res.Started {
		t.Errorf("重建后应 Ready+Created+Started: got %+v", res)
	}
	if f.lastCreate.Load() == nil {
		t.Fatal("应捕获到重建创建请求体")
	}
	assertCreatePayloadPortRange(t, *f.lastCreate.Load())
}

func TestEnsureSuperSdkNoRecreateWhenPortRangePresent(t *testing.T) {
	// 81 可达 + 端口段在位：正常幂等短路，不删不建不启动
	f := newFixture(t)
	c := newSuperSdkTestClient(t, f.docker, f.sdk)
	f.sdkUp.Store(true)
	f.ctrCount.Store(1)
	res, err := c.EnsureSuperSdk(context.Background(), "127.0.0.1", nil)
	if err != nil {
		t.Fatalf("EnsureSuperSdk: %v", err)
	}
	if !res.Ready || res.PulledImage || res.Created || res.Started {
		t.Errorf("端口段在位应直接短路: got %+v", res)
	}
	if f.removes.Load() != 0 || f.ctrCount.Load() != 1 || f.starts.Load() != 0 {
		t.Errorf("不应重建: removes=%d ctr=%d starts=%d",
			f.removes.Load(), f.ctrCount.Load(), f.starts.Load())
	}
}

func TestEnsureSuperSdkInspectFailKeepsShortCircuit(t *testing.T) {
	// inspect 端口段失败（瞬时网络）：保守视为端口段在位，保持幂等短路
	// 不重建（避免抖动触发无谓删除重建）。用连接到不存在端口的 client 模拟。
	f := newFixture(t)
	c := newSuperSdkTestClient(t, f.docker, f.sdk)
	f.sdkUp.Store(true)
	f.ctrCount.Store(1)
	c.port = 1 // 2375 不可达：inspect 必失败
	res, err := c.EnsureSuperSdk(context.Background(), "127.0.0.1", nil)
	if err != nil {
		t.Fatalf("EnsureSuperSdk: %v", err)
	}
	if !res.Ready {
		t.Errorf("inspect 失败应保守短路 Ready=true: got %+v", res)
	}
	if f.removes.Load() != 0 {
		t.Errorf("inspect 失败不应重建, removes=%d", f.removes.Load())
	}
}
