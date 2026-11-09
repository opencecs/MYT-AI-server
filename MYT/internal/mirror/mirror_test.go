package mirror

import (
	"testing"

	"github.com/moyunteng/myt/internal/util"
)

func TestHasV3Marker(t *testing.T) {
	cases := []struct {
		ttype2 []string
		want   bool
	}{
		{[]string{"q1_v3"}, true},
		{[]string{"p1_v2", "p1_v3"}, true},
		{[]string{"q1_v2"}, false},
		{[]string{"q1_v30"}, false},
		{[]string{"q1_v3_cecs"}, false},
		{[]string{"C1"}, false},
		{[]string{}, false},
	}
	for _, c := range cases {
		got := hasV3Marker(util.MirrorItem{TType2: c.ttype2})
		if got != c.want {
			t.Errorf("hasV3Marker(%v) = %v, want %v", c.ttype2, got, c.want)
		}
	}
}

func TestFilterV3Mirrors(t *testing.T) {
	items := []util.MirrorItem{
		{ID: "1", TType2: []string{"q1_v3"}},        // 保留
		{ID: "2", TType2: []string{"q1_v2"}},        // 过滤
		{ID: "3", TType2: []string{"p1_v2", "p1_v3"}}, // 保留
		{ID: "4", TType2: []string{"q1_v30"}},       // 过滤
		{ID: "5", TType2: []string{"C1"}},            // 过滤
	}
	got := filterV3Mirrors(items)
	if len(got) != 2 {
		t.Fatalf("filterV3Mirrors() len = %d, want 2", len(got))
	}
	expectIDs := map[string]bool{"1": true, "3": true}
	for _, it := range got {
		if !expectIDs[it.ID] {
			t.Errorf("unexpected ID = %q", it.ID)
		}
	}
}

func TestFilterEmulatorMirrors(t *testing.T) {
	items := []util.MirrorItem{
		{ID: "1", Name: "模拟器专用", SysVer: "5", TType2: []string{"q1_v3"}},     // 模拟器
		{ID: "2", Name: "容器v3", SysVer: "1", TType2: []string{"q1_v3"}},        // 容器（不是模拟器）
		{ID: "3", Name: "无v3模拟器", SysVer: "5", TType2: []string{"q1_v2"}},      // 过滤（无 _v3）
		{ID: "4", Name: "兼容v2v3容器", SysVer: "1", TType2: []string{"p1_v2", "p1_v3"}}, // 容器
	}

	got := filterEmulatorMirrors(items)
	if len(got) != 1 {
		t.Fatalf("filterEmulatorMirrors() len = %d, want 1", len(got))
	}
	if got[0].ID != "1" {
		t.Errorf("got[0].ID = %q, want %q", got[0].ID, "1")
	}
}

func TestFilterContainerMirrors(t *testing.T) {
	items := []util.MirrorItem{
		{ID: "1", Name: "模拟器专用", SysVer: "5", TType2: []string{"q1_v3"}},          // 不是容器
		{ID: "2", Name: "纯容器v2", SysVer: "1", TType2: []string{"q1_v2"}},            // 过滤（无 _v3）
		{ID: "3", Name: "兼容v2v3", SysVer: "1", TType2: []string{"p1_v2", "p1_v3"}}, // 容器
		{ID: "4", Name: "无后缀", SysVer: "2", TType2: []string{"C1"}},                  // 过滤（无 _v3）
		{ID: "5", Name: "容器v3", SysVer: "", TType2: []string{"q1_v3"}},               // 容器（sys_ver 空）
	}

	got := filterContainerMirrors(items)
	if len(got) != 2 {
		t.Fatalf("filterContainerMirrors() len = %d, want 2", len(got))
	}
	expectIDs := map[string]bool{"3": true, "5": true}
	for _, it := range got {
		if !expectIDs[it.ID] {
			t.Errorf("unexpected ID = %q in container list", it.ID)
		}
	}
}

// 模拟器 ∪ 容器 == 全部 _v3 镜像；交集为空
func TestEmulatorContainerPartitionV3(t *testing.T) {
	svc := NewService()
	items := []util.MirrorItem{
		{ID: "1", Name: "模拟器", SysVer: "5", TType2: []string{"q1_v3"}},
		{ID: "2", Name: "容器", SysVer: "1", TType2: []string{"q1_v3"}},
		{ID: "3", Name: "兼容", SysVer: "1", TType2: []string{"p1_v2", "p1_v3"}},
		{ID: "4", Name: "无v3", SysVer: "2", TType2: []string{"C1"}}, // 应被过滤
	}
	svc.SetCache(items)

	emulator := svc.GetCachedEmulatorMirrors()
	container := svc.GetCachedContainerMirrors()
	v3 := svc.GetCachedV3Mirrors()

	if len(emulator) != 1 || emulator[0].ID != "1" {
		t.Errorf("emulator mirrors = %v, want only ID=1", emulator)
	}
	if len(container) != 2 {
		t.Errorf("container mirrors = %v, want 2 (ID=2,3)", container)
	}
	if len(v3) != 3 {
		t.Errorf("v3 mirrors = %v, want 3 (ID=1,2,3)", v3)
	}
	if len(emulator)+len(container) != len(v3) {
		t.Errorf("emulator + container = %d, want %d (v3 全集)", len(emulator)+len(container), len(v3))
	}
}

func TestServiceSetAndGetCache(t *testing.T) {
	svc := NewService()

	items := []util.MirrorItem{
		{ID: "img1", Name: "测试镜像"},
	}
	svc.SetCache(items)

	got := svc.GetCachedMirrors()
	if len(got) != 1 || got[0].ID != "img1" {
		t.Errorf("GetCachedMirrors() = %v, want img1", got)
	}
}

func TestServiceEmptyCache(t *testing.T) {
	svc := NewService()
	got := svc.GetCachedMirrors()
	if len(got) != 0 {
		t.Errorf("empty cache should return 0 items, got %d", len(got))
	}
}
