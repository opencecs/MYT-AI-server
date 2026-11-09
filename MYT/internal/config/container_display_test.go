package config

import "testing"

// 对称用例：与前端 frontend/src/utils/containerName.ts 的 containerDisplayName 逐条对齐。
// 前端用例基准（containerName.ts 注释 + StreamTab 等页面的既有行为）。
func TestContainerDisplayName(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		deviceID string
		indexNum int
		want     string
	}{
		// pc-client 式时间戳名 → 实例序号（本次修复的主场景）
		{"时间戳名取实例序号", "1793348153001_6_1", "dev123", 6, "1"},
		{"用户名取实例序号", "myname_4_1", "dev123", 4, "1"},
		// 实例序号恰等于 indexNum 的 3 段名：判定键是第三段，不能误退回原名
		{"实例序号恰等于坑位号", "1729012345678_4_4", "dev123", 4, "4"},
		// 壳子式名称段
		{"壳子式取名称段", "c043b1cf1234_1_T1001", "c043b1cf1234", 1, "T1001"},
		// 名称段本身含 `_`：join 回来
		{"名称段含下划线", "dev123_2_foo_bar", "dev123", 2, "foo_bar"},
		// 用户名自带 `_`（4 段）且第三段恰为坑位号 → 取 slice(3)
		{"用户名带下划线防御", "my_name_4_1", "dev123", 4, "1"},
		{"用户名带下划线防御序号相等", "my_name_4_4", "dev123", 4, "4"},
		// 4 段但第三段非坑位号（非纯数字 / 数字≠indexNum）→ 防御不触发，仍取 slice(2)（与前端一致）
		{"四段第三段非数字", "my_name_a_1", "dev123", 4, "a_1"},
		{"四段第三段非坑位号", "my_name_5_1", "dev123", 4, "5_1"},
		// 段数 <3 → 原样
		{"两段名原样", "abc_1", "dev123", 1, "abc_1"},
		{"无下划线原样", "plainname", "dev123", 1, "plainname"},
		// slice(3) 为空 → 原名
		{"四段尾段为空退回原名", "my_name_4_", "dev123", 4, "my_name_4_"},
		// slice(2) 为空 → T 兜底
		{"三段尾段为空T兜底", "dev123_1_", "dev123", 1, "T1001"},
		// 空名 → T 兜底
		{"空名T兜底", "", "dev123", 3, "T1003"},
		{"空名零坑位", "", "dev123", 0, "T1000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContainerDisplayName(tt.in, tt.deviceID, tt.indexNum); got != tt.want {
				t.Errorf("ContainerDisplayName(%q, %q, %d) = %q, want %q", tt.in, tt.deviceID, tt.indexNum, got, tt.want)
			}
		})
	}
}
