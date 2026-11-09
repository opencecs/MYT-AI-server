package uimap

// summary.go — ui_map 输出的「看页面」层：把检测到的局面翻译成人类视角的
// 页面描述与下一步建议，让 AI 第一眼读到的不是原始节点流，而是「这是哪个页、
// 有哪些可操作区块、建议先做什么」。全部基于真实检测，检测不到的区块不写。

import (
	"fmt"
	"regexp"
	"strings"
)

// pageDescriptions data-tab 值 → 页面人话描述（与 frontend/src/App.tsx 的
// tabs 数组一一对应；新增页面时同步维护）。
var pageDescriptions = map[string]string{
	"host":        "云机管理页（host）：左侧设备分组列表 + 右侧云机卡片/坑位操作区",
	"backup":      "备份管理页（backup）：备份计划与备份文件管理",
	"mirror":      "镜像管理页（mirror）：镜像源与镜像列表（下载/上传/注册）",
	"payment":     "授权页（payment）：账号登录、槽位与付费授权",
	"vpc":         "VPC 管理页（vpc）：虚拟私有网络配置",
	"model":       "机型管理页（model）：机型模板列表与机型编辑器",
	"maintenance": "维护页（maintenance）：设备维护工具（日志/缓存/面板安装）",
	"agent":       "智能体对话页（agent）：AI 会话与技能/任务/平台面板",
	"diskless":    "无盘管理页（diskless）：iSCSI Target 与无盘启动配置",
	"stream":      "流媒体页（stream）：推流会话管理",
	"settings":    "设置页（settings）：客户端配置（含 MCP 服务开关）",
}

// ipPattern 设备 IP 形态（整词匹配，用于估计可见列表规模）。
var ipPattern = regexp.MustCompile(`^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$`)

// buildSummary 生成 summary 段：page（页面识别）+ blocks（主要区块描述）
// + next（建议下一步）。
func buildSummary(state map[string]any, items []interactiveItem) map[string]any {
	s := map[string]any{}
	if tab, _ := state["activeTab"].(string); tab != "" {
		if desc, ok := pageDescriptions[tab]; ok {
			s["page"] = desc
		}
	}
	blocks := summaryBlocks(state, items)
	if len(blocks) > 0 {
		s["blocks"] = blocks
	}
	if next := suggestNext(state, items); next != "" {
		s["next"] = next
	}
	return s
}

// summaryBlocks 扫描可交互元素列出的主要区块（≤6 条，控制 summary 篇幅）。
func summaryBlocks(state map[string]any, items []interactiveItem) []string {
	blocks := make([]string, 0, 6)
	if sb := findSearchBox(items); sb != nil {
		blocks = append(blocks, fmt.Sprintf("顶部有搜索框（placeholder=「%s」，uid=%s）——列表长时先搜索过滤",
			sb.e.Placeholder, sb.uid))
	}
	collapsed, expanded := countGroupToggles(items)
	switch {
	case collapsed > 0:
		blocks = append(blocks, fmt.Sprintf("列表按分组折叠显示：有 %d 个折叠分组开关，目标条目不在可见列表时先展开分组（如 uid=%s）",
			collapsed, firstGroupUID(items, false)))
	case expanded > 0:
		blocks = append(blocks, fmt.Sprintf("分组当前已展开（%d 个分组开关）", expanded))
	}
	if n := countIPEntries(items); n > 0 {
		blocks = append(blocks, fmt.Sprintf("可见列表中检测到约 %d 个 IP 条目", n))
	}
	if m, ok := state["modal"].(map[string]any); ok {
		if hint, _ := m["hint"].(string); hint != "" {
			blocks = append(blocks, "注意："+hint)
		}
	}
	return blocks
}

// suggestNext 规则推导建议下一步（优先级：弹窗 > 搜索过滤 > 展开分组 > 通用）。
func suggestNext(state map[string]any, items []interactiveItem) string {
	if m, ok := state["modal"].(map[string]any); ok {
		if sug, ok := m["suggested"].(map[string]any); ok {
			text, _ := sug["text"].(string)
			uid, _ := sug["uid"].(string)
			return fmt.Sprintf("先处理弹窗：点击「%s」（uid=%s）", text, uid)
		}
		return "先关闭弹窗再进行其它操作"
	}
	if sb := findSearchBox(items); sb != nil && countIPEntries(items) >= 8 {
		return "先在搜索框输入目标 IP/名称过滤，再点击对应条目"
	}
	if hasCollapsedGroup(items) {
		return "目标条目可能被折叠隐藏：先展开分组，再在可见元素中定位目标"
	}
	return "在 interactiveElements 中按文本/名称找到目标元素，用其 uid 操作"
}

// findSearchBox 找搜索框：input/textarea 且 placeholder/aria-label/文本含「搜索」。
func findSearchBox(items []interactiveItem) *interactiveItem {
	for i := range items {
		e := items[i].e
		if e.Tag != "input" && e.Tag != "textarea" {
			continue
		}
		if strings.Contains(e.Placeholder, "搜索") ||
			strings.Contains(e.AriaLabel, "搜索") ||
			strings.Contains(e.Text, "搜索") {
			return &items[i]
		}
	}
	return nil
}

// countGroupToggles 统计折叠/展开分组开关（aria-expanded 语义元素）。
func countGroupToggles(items []interactiveItem) (collapsed, expanded int) {
	for _, it := range items {
		switch it.e.Expanded {
		case "false":
			collapsed++
		case "true":
			expanded++
		}
	}
	return collapsed, expanded
}

// hasCollapsedGroup 是否存在折叠分组。
func hasCollapsedGroup(items []interactiveItem) bool {
	c, _ := countGroupToggles(items)
	return c > 0
}

// firstGroupUID 第一个折叠（expand=false）或展开分组的 uid。
func firstGroupUID(items []interactiveItem, expanded bool) string {
	want := "false"
	if expanded {
		want = "true"
	}
	for _, it := range items {
		if it.e.Expanded == want {
			return it.uid
		}
	}
	return ""
}

// countIPEntries 文本为 IP 形态的元素计数（设备列表规模估计）。
func countIPEntries(items []interactiveItem) int {
	n := 0
	for _, it := range items {
		if ipPattern.MatchString(strings.TrimSpace(it.e.Text)) {
			n++
		}
	}
	return n
}

// textClip 元素文本输出截断（runes，避免中文截半）。
func textClip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
