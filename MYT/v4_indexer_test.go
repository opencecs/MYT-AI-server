package main

import (
	"reflect"
	"testing"

	"github.com/moyunteng/myt/internal/config"
)

// V4 实例序号派生：稳定性是截图 key（IP_indexNum）正确性的前提
func TestV4IndexerStableAssignment(t *testing.T) {
	idx := newV4Indexer()

	// 首轮：按字典序分配 1..3
	got := idx.Assign("10.0.0.1", []string{"b/2", "a/1", "c/3"})
	want := map[string]int{"a/1": 1, "b/2": 2, "c/3": 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("首轮分配 = %v, want %v", got, want)
	}

	// 新增实例：既有实例序号不变，新实例取最小空位（4）
	got = idx.Assign("10.0.0.1", []string{"a/1", "b/2", "c/3", "d/4"})
	if got["a/1"] != 1 || got["b/2"] != 2 || got["c/3"] != 3 {
		t.Errorf("既有实例序号漂移: %v", got)
	}
	if got["d/4"] != 4 {
		t.Errorf("新实例序号 = %d, want 4", got["d/4"])
	}

	// 删除中间实例：其余序号不变（不重排）
	got = idx.Assign("10.0.0.1", []string{"a/1", "c/3", "d/4"})
	if got["a/1"] != 1 || got["c/3"] != 3 || got["d/4"] != 4 {
		t.Errorf("删除后序号不应重排: %v", got)
	}

	// 再新增：复用空出的 2
	got = idx.Assign("10.0.0.1", []string{"a/1", "c/3", "d/4", "e/5"})
	if got["e/5"] != 2 {
		t.Errorf("应复用空出的序号 2, got %d (%v)", got["e/5"], got)
	}
}

// 不同主机的序号互相独立
func TestV4IndexerPerHost(t *testing.T) {
	idx := newV4Indexer()
	idx.Assign("10.0.0.1", []string{"a/1", "a/2"})
	got := idx.Assign("10.0.0.2", []string{"a/1"})
	if got["a/1"] != 1 {
		t.Errorf("另一台主机的实例应从 1 起, got %v", got)
	}
}

// DropAll：模式切换后重新从 1 分配
func TestV4IndexerDropAll(t *testing.T) {
	idx := newV4Indexer()
	idx.Assign("10.0.0.1", []string{"a/1", "a/2"})
	idx.DropAll()
	got := idx.Assign("10.0.0.1", []string{"a/2"})
	if got["a/2"] != 1 {
		t.Errorf("DropAll 后应重新从 1 起, got %v", got)
	}
}

// 空 key 与重复 key 防御
func TestV4IndexerIgnoresInvalidKeys(t *testing.T) {
	idx := newV4Indexer()
	got := idx.Assign("10.0.0.1", []string{"", "a/1", "a/1"})
	if _, ok := got[""]; ok {
		t.Error("空 key 不应分配序号")
	}
	if len(got) != 1 || got["a/1"] != 1 {
		t.Errorf("结果不符: %v", got)
	}
}

// V4 状态词汇映射（前端 containerStatus 枚举）
func TestV4StateToStatus(t *testing.T) {
	cases := map[string]string{
		"stopped":  "exited",   // 「已关机」
		"starting": "starting", // 开机中
		"running":  "running",
		"stopping": "stopping",
		"unknown":  "unknown",
		"":         "",
	}
	for in, want := range cases {
		if got := v4StateToStatus(in); got != want {
			t.Errorf("v4StateToStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

// 主机 IP 归一（"ip:8088" → "ip"）
func TestV4HostIP(t *testing.T) {
	if got := v4HostIP("10.10.2.142:8088"); got != "10.10.2.142" {
		t.Errorf("v4HostIP = %q", got)
	}
	if got := v4HostIP("10.10.2.142"); got != "10.10.2.142" {
		t.Errorf("v4HostIP = %q", got)
	}
}

// 设备列表 → WS 对账主机列表（跳过无 IP 条目）
func TestV4HostsOf(t *testing.T) {
	hosts := v4HostsOf([]config.DeviceCache{
		{DeviceID: "10.0.0.1", IP: "10.0.0.1"},
		{DeviceID: "10.0.0.2"}, // IP 空：用 DeviceID 兜底
		{DeviceID: "", IP: ""}, // 全空：跳过
	})
	if len(hosts) != 2 || hosts[0] != "10.0.0.1" || hosts[1] != "10.0.0.2" {
		t.Errorf("v4HostsOf = %v", hosts)
	}
}
