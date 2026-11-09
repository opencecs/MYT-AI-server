package v2host

import "testing"

func TestSlotIndex(t *testing.T) {
	cases := []struct {
		name string
		c    ContainerInfo
		want int
	}{
		{"label优先", ContainerInfo{Names: []string{"/myt_9"}, Labels: map[string]string{"idx": "3"}}, 3},
		{"名称末段", ContainerInfo{Names: []string{"/myt_12"}}, 12},
		{"无坑位", ContainerInfo{Names: []string{"/myt"}}, 0},
		{"末段非数字", ContainerInfo{Names: []string{"/myt_abc"}}, 0},
		{"空名称", ContainerInfo{}, 0},
	}
	for _, tc := range cases {
		if got := slotIndex(tc.c); got != tc.want {
			t.Errorf("%s: slotIndex=%d want=%d", tc.name, got, tc.want)
		}
	}
}

func TestToAndroidInfo(t *testing.T) {
	c := ContainerInfo{
		ID:     "abc123def456",
		Names:  []string{"/T_5"},
		Image:  "myt:latest",
		State:  "running",
		Status: "Up 2 hours",
		Ports: []PortJSON{
			{PrivatePort: 9082, PublicPort: 19082, Type: "tcp"},
			{PrivatePort: 10000, PublicPort: 20000, Type: "tcp"},
			{PrivatePort: 10001, PublicPort: 20001, Type: "udp"},
			{PrivatePort: 22, Type: "tcp"}, // 未发布，应被过滤
		},
	}
	a := ToAndroidInfo(c, "host1", "192.168.1.10")
	if a.ID != "abc123def456" || a.Name != "T_5" || a.Status != "running" {
		t.Fatalf("基础字段映射错误: %+v", a)
	}
	if a.IndexNum != 5 || a.DeviceID != "host1" || a.IP != "192.168.1.10" {
		t.Fatalf("坑位/归属映射错误: %+v", a)
	}
	if len(a.PortBindings) != 3 {
		t.Fatalf("端口映射条数=%d want=3", len(a.PortBindings))
	}
	if pb := a.PortBindings["9082/tcp"]; len(pb) != 1 || pb[0].HostPort != "19082" {
		t.Fatalf("9082/tcp 映射错误: %+v", a.PortBindings["9082/tcp"])
	}
	if _, ok := a.PortBindings["22/tcp"]; ok {
		t.Fatalf("未发布端口不应进映射")
	}
}

func TestContainerName(t *testing.T) {
	if got := containerName(ContainerInfo{Names: []string{"/x", "/y"}}); got != "x" {
		t.Fatalf("containerName=%q want=x", got)
	}
}
