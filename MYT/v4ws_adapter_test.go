package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/v4ws"
)

// --- V4 链路端到端：伪 tobox 服务端 → v4ws → 适配层 → config v4 桶 ---
//
// 覆盖用户可见的关键行为：
//   - snapshot 落到 v4 桶（key 约定：DeviceID/IP=主机 IP，ID=backend/id，indexNum 稳定）
//   - instance/state 事件补丁状态
//   - deleted 事件移除条目
//   - 快照变空（删掉最后一台）必须真的清空（不是保留幽灵条目）
//   - 快照触发 androidlist:updated

// fakeTobox 伪 tobox /ws/events 服务端
type fakeTobox struct {
	srv   *httptest.Server
	mu    sync.Mutex
	conns []*websocket.Conn
	dial  chan struct{}
}

func newFakeTobox(t *testing.T) *fakeTobox {
	t.Helper()
	f := &fakeTobox{dial: make(chan struct{}, 8)}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/events" {
			http.NotFound(w, r)
			return
		}
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		f.mu.Lock()
		f.conns = append(f.conns, ws)
		f.mu.Unlock()
		f.dial <- struct{}{}

		ctx := context.Background()
		_ = ws.Write(ctx, websocket.MessageText, []byte(`{"type":"hello","proto":1,"sdkVersion":"0.7.0","serverTime":1}`))
		_ = ws.Write(ctx, websocket.MessageText, []byte(`{"type":"snapshot","seq":0,"data":{"instances":[
			{"backend":"be","id":"a","name":"inst-a","variant":"img1","state":"running","running":true},
			{"backend":"be","id":"b","name":"inst-b","variant":"img2","state":"stopped","running":false}],"total":2}}`))
		for {
			if _, _, err := ws.Read(ctx); err != nil {
				return
			}
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTobox) host() string {
	u := f.srv.URL
	return u[strings.LastIndex(u, "/")+1:]
}

func (f *fakeTobox) send(t *testing.T, frame string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.conns) == 0 {
		t.Fatal("尚无连接")
	}
	if err := f.conns[len(f.conns)-1].Write(context.Background(), websocket.MessageText, []byte(frame)); err != nil {
		t.Errorf("发送帧失败: %v", err)
	}
}

// newV4TestManager 建一个 v4 模式的临时 config.Manager
func newV4TestManager(t *testing.T) *config.Manager {
	t.Helper()
	cm := config.NewManager(t.TempDir())
	if err := cm.InitDirs(); err != nil {
		t.Fatalf("InitDirs 失败: %v", err)
	}
	if err := cm.SetDeployMode("v4"); err != nil {
		t.Fatalf("SetDeployMode(v4) 失败: %v", err)
	}
	return cm
}

// 等待条件成立
func waitFor(t *testing.T, d time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", msg)
}

// findAndroid 按容器 ID 找缓存条目
func findAndroid(list []config.AndroidInfo, id string) (config.AndroidInfo, bool) {
	for _, a := range list {
		if a.ID == id {
			return a, true
		}
	}
	return config.AndroidInfo{}, false
}

// 主链路：snapshot 落库（key 约定 + 状态映射 + indexNum 稳定）
func TestV4AdapterSnapshotToCache(t *testing.T) {
	cm := newV4TestManager(t)
	var mu sync.Mutex
	events := []string{}
	idx := newV4Indexer()
	ad := newV4EventAdapter(cm, idx, func(name string, _ any) {
		mu.Lock()
		events = append(events, name)
		mu.Unlock()
	})

	f := newFakeTobox(t)
	svc := v4ws.NewService("admin", "myt")
	svc.SetHandlers(ad)
	defer svc.Stop()
	svc.Connect(f.host())

	waitFor(t, 3*time.Second, func() bool { return len(cm.GetAndroidList()) == 2 }, "snapshot 未落库")

	list := cm.GetAndroidList()
	a, ok := findAndroid(list, "be/a")
	if !ok {
		t.Fatalf("缺少 be/a: %+v", list)
	}
	// key 约定：IP/DeviceID = 主机纯 IP；ID = backend/id；Name = 服务端实例名
	hostIP := v4HostIP(f.host())
	if a.DeviceID != hostIP || a.IP != hostIP {
		t.Errorf("设备 key 应为主机 IP(%s): %+v", hostIP, a)
	}
	if a.Name != "inst-a" || a.Image != "img1" {
		t.Errorf("实例字段不符: %+v", a)
	}
	// 状态映射：running 原样、stopped → exited
	if a.Status != "running" {
		t.Errorf("running 实例状态 = %q", a.Status)
	}
	b, _ := findAndroid(list, "be/b")
	if b.Status != "exited" {
		t.Errorf("stopped 实例应映射为 exited, got %q", b.Status)
	}
	// indexNum：同主机内唯一且稳定（a/b 字典序 a 在前）
	if a.IndexNum != 1 || b.IndexNum != 2 {
		t.Errorf("indexNum 不符: a=%d b=%d", a.IndexNum, b.IndexNum)
	}
	// 通知：androidlist:updated 至少一次
	waitFor(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(events) > 0
	}, "未推送 androidlist:updated")
	mu.Lock()
	defer mu.Unlock()
	if events[0] != "androidlist:updated" {
		t.Errorf("事件名不符: %v", events)
	}
}

// instance/state 事件：状态补丁 + deleted 移除
func TestV4AdapterInstanceStateEvents(t *testing.T) {
	cm := newV4TestManager(t)
	idx := newV4Indexer()
	ad := newV4EventAdapter(cm, idx, nil)

	f := newFakeTobox(t)
	svc := v4ws.NewService("admin", "myt")
	svc.SetHandlers(ad)
	defer svc.Stop()
	svc.Connect(f.host())
	waitFor(t, 3*time.Second, func() bool { return len(cm.GetAndroidList()) == 2 }, "snapshot 未落库")

	// 状态变化：be/b stopped → running
	f.send(t, `{"type":"event","seq":1,"event":"instance","action":"state","data":{"backend":"be","id":"b","name":"inst-b","state":"running","running":true}}`)
	waitFor(t, 3*time.Second, func() bool {
		a, ok := findAndroid(cm.GetAndroidList(), "be/b")
		return ok && a.Status == "running"
	}, "状态未补丁为 running")

	// 删除：be/a 消失
	f.send(t, `{"type":"event","seq":2,"event":"instance","action":"state","data":{"backend":"be","id":"a","name":"inst-a","state":"deleted","running":false}}`)
	waitFor(t, 3*time.Second, func() bool {
		_, ok := findAndroid(cm.GetAndroidList(), "be/a")
		return !ok
	}, "deleted 事件未移除条目")

	if n := len(cm.GetAndroidList()); n != 1 {
		t.Errorf("删除后应剩 1 条, got %d", n)
	}
}

// 空快照（删掉最后一台）必须真的清空缓存——这是 MergeAndroidList 做不到、
// 必须用 ReplaceAndroidListForDevice 的关键场景（否则幽灵条目永久残留）。
func TestV4AdapterEmptySnapshotClearsCache(t *testing.T) {
	cm := newV4TestManager(t)
	idx := newV4Indexer()
	ad := newV4EventAdapter(cm, idx, nil)

	host := "10.0.0.9"
	// 先塞两条（模拟上一轮快照）
	ad.OnSnapshot(host, &v4ws.SnapshotFrame{Seq: 1, Data: struct {
		Instances []v4ws.InstanceItem `json:"instances"`
		Total     int                 `json:"total"`
	}{Instances: []v4ws.InstanceItem{
		{Backend: "be", ID: "a", Name: "a", State: "running"},
		{Backend: "be", ID: "b", Name: "b", State: "stopped"},
	}}})
	if n := len(cm.GetAndroidList()); n != 2 {
		t.Fatalf("预置快照后条数 = %d, want 2", n)
	}

	// 空快照 = 设备上已无实例
	ad.OnSnapshot(host, &v4ws.SnapshotFrame{Seq: 2, Data: struct {
		Instances []v4ws.InstanceItem `json:"instances"`
		Total     int                 `json:"total"`
	}{}})
	if n := len(cm.GetAndroidList()); n != 0 {
		t.Fatalf("空快照应清空缓存（不能保留幽灵条目），实际 %d 条: %+v", n, cm.GetAndroidList())
	}
}

// 模式复查：非 v4 模式收到快照必须丢弃（切模式后旧连接的残余帧不得回灌）
func TestV4AdapterDropsSnapshotOutsideV4(t *testing.T) {
	cm := newV4TestManager(t)
	if err := cm.SetDeployMode("local"); err != nil {
		t.Fatal(err)
	}
	ad := newV4EventAdapter(cm, newV4Indexer(), nil)
	ad.OnSnapshot("10.0.0.9", &v4ws.SnapshotFrame{Seq: 1, Data: struct {
		Instances []v4ws.InstanceItem `json:"instances"`
		Total     int                 `json:"total"`
	}{Instances: []v4ws.InstanceItem{{Backend: "be", ID: "a"}}}})
	if n := len(cm.GetAndroidList()); n != 0 {
		t.Errorf("非 v4 模式的快照应丢弃, 实际 %d 条", n)
	}
}

// v4ResolveRef：前端传容器名 → REST 的 (backend, id)
func TestV4ResolveRef(t *testing.T) {
	cm := newV4TestManager(t)
	cm.SetAndroidList([]config.AndroidInfo{
		{ID: "be/a", Name: "inst-a", DeviceID: "10.0.0.1", IP: "10.0.0.1"},
	})

	ref, err := v4ResolveRef(cm, "10.0.0.1", "inst-a")
	if err != nil || ref.ID != "a" || ref.Backend != "be" {
		t.Fatalf("按名字反查失败: %+v %v", ref, err)
	}
	// 直接传缓存 ID 也能解析
	ref, err = v4ResolveRef(cm, "10.0.0.1", "be/a")
	if err != nil || ref.ID != "a" || ref.Backend != "be" {
		t.Fatalf("按 ID 反查失败: %+v %v", ref, err)
	}
	// 缓存未命中：按裸 id 处理（backend 留空 → 服务端默认后端）
	ref, err = v4ResolveRef(cm, "10.0.0.1", "unknown-id")
	if err != nil || ref.ID != "unknown-id" || ref.Backend != "" {
		t.Fatalf("未命中时应按裸 id 处理: %+v %v", ref, err)
	}
	// 空标识：报错
	if _, err := v4ResolveRef(cm, "10.0.0.1", ""); err == nil {
		t.Error("空标识应返回错误")
	}
}

// 快照与 REST 两条路径派生的 indexNum 必须一致（截图 key 正确性的前提）
func TestV4IndexerSharedAcrossPaths(t *testing.T) {
	idx := newV4Indexer()
	viaWS := idx.Assign("10.0.0.1", []string{"be/a", "be/b"})
	viaREST := idx.Assign("10.0.0.1", []string{"be/a", "be/b"})
	if viaWS["be/a"] != viaREST["be/a"] || viaWS["be/b"] != viaREST["be/b"] {
		t.Errorf("两条路径序号不一致: ws=%v rest=%v", viaWS, viaREST)
	}
}
