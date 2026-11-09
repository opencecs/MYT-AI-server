package uimap

// snapshot_format.go — chrome-devtools-mcp src/formatters/SnapshotFormatter.ts
// 的 1:1 移植：快照行格式 `uid=N_M role "name" attrs…`，属性排序、布尔状态
// 映射、冗余 StaticText 子节点剪枝均照抄官方（行内注释标注官方行号）。
// 行尾追加「# 语义：…」为本项目扩展（语义字典叠加，官方无此机制）。
// 语义字典（原 semantics.go）与渲染强耦合，并入本文件。

import (
	"fmt"
	"sort"
	"strings"

	"github.com/moyunteng/myt/internal/mcpserver/uimap/axtree"
)

// booleanPropertyMap 官方布尔属性 → 状态标签映射（SnapshotFormatter.ts:183-188）。
var booleanPropertyMap = map[string]string{
	"disabled": "disableable",
	"expanded": "expandable",
	"focused":  "focusable",
	"selected": "selectable",
}

// formatSnapLine 渲染单行（官方 #formatNode 的行构造，SnapshotFormatter.ts:39-57）。
// note 非空时追加为行尾注释（本项目扩展：语义字典 / DOM 兜底标注）。
func formatSnapLine(n *axtree.SnapNode, depth int, note string) string {
	attrs := snapAttributes(n)
	if note != "" {
		attrs = append(attrs, "#", note)
	}
	return strings.Repeat(" ", depth*2) + strings.Join(attrs, " ")
}

// snapAttributes 官方 #getAttributes（SnapshotFormatter.ts:87-136）：
// uid → role（StaticText 不显示、none 显示为 ignored）→ "name" →
// 其余字段按 key 排序（布尔映射 + 原始值）。
func snapAttributes(n *axtree.SnapNode) []string {
	attrs := []string{"uid=" + n.UID}

	if n.Role != "" && n.Role != "StaticText" {
		if n.Role == "none" {
			attrs = append(attrs, "ignored")
		} else {
			attrs = append(attrs, n.Role)
		}
	}
	if n.Name != "" {
		attrs = append(attrs, `"`+n.Name+`"`)
	}

	simple := extractedAttrs(n)
	isOption := n.Role == "option"

	keys := make([]string, 0, len(n.Fields))
	for k := range n.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		// 布尔状态映射：disabled=true → 先输出 disableable 再输出 disabled。
		if mapped, ok := booleanPropertyMap[k]; ok {
			if mv, isBool := simple[mapped].(bool); isBool && mv &&
				!(isOption && mapped == "selectable") {
				attrs = append(attrs, mapped)
			}
		}
		val := simple[k]
		// option 节点 value 与 name 相同时不重复输出（官方 TextSnapshot.ts:99-106）。
		if isOption && k == "value" && val == n.Name {
			continue
		}
		switch v := val.(type) {
		case bool:
			if v {
				attrs = append(attrs, k)
			}
		case string:
			attrs = append(attrs, fmt.Sprintf(`%s="%s"`, k, v))
		case float64:
			attrs = append(attrs, fmt.Sprintf(`%s="%v"`, k, v))
		}
	}
	return attrs
}

// extractedAttrs 官方 #extractedAttributes（SnapshotFormatter.ts:160-180）：
// 布尔字段无条件写入映射标签（disableable 等），真值额外写入原始 key；
// 字符串/数值原样保留。
func extractedAttrs(n *axtree.SnapNode) map[string]any {
	out := make(map[string]any, len(n.Fields)+4)
	keys := make([]string, 0, len(n.Fields))
	for k := range n.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch v := n.Fields[k].(type) {
		case bool:
			if mapped, ok := booleanPropertyMap[k]; ok {
				out[mapped] = true
			}
			if v {
				out[k] = true
			}
		case string, float64:
			out[k] = v
		}
	}
	return out
}

// renderSnap 递归渲染快照行（region 过滤 + maxNodes 预算 + 行尾语义注释），
// 返回是否输出了行。无坐标且 region!=all 时跳过本行但继续遍历子树。
func renderSnap(n *axtree.SnapNode, depth int, bounds map[int64][2]float64, region string,
	verbose bool, a *uidAllocator, lines *[]string, budget *int) bool {
	if *budget <= 0 {
		return false
	}
	keep := true
	if region != "all" {
		b, ok := bounds[n.BackendID]
		keep = ok && inRegion(b[0], b[1], region)
	}
	rendered := false
	if keep {
		*budget--
		note := ""
		if s := semanticNote(n.Name); s != "" {
			note = "语义：" + s
		}
		*lines = append(*lines, formatSnapLine(n, depth, note))
		rendered = true
	}
	if !hasRedundantTextChildren(n, verbose) {
		for _, c := range n.Children {
			if renderSnap(c, depth+1, bounds, region, verbose, a, lines, budget) {
				rendered = true
			}
			if *budget <= 0 {
				return rendered
			}
		}
	}
	return rendered
}

// countSnapNodes 统计序列化树节点数（截断提示用）。
func countSnapNodes(roots []*axtree.SnapNode) int {
	n := 0
	var walk func(*axtree.SnapNode)
	walk = func(x *axtree.SnapNode) {
		n++
		for _, c := range x.Children {
			walk(c)
		}
	}
	for _, r := range roots {
		walk(r)
	}
	return n
}

// hasRedundantTextChildren 官方 #hasRedundantTextChildren（SnapshotFormatter.ts:59-75）：
// 非 verbose 下，节点有 name 且全部子节点为无子节点的 StaticText、
// 文本拼接（去空白）等于 name 时，子节点不再重复输出。
func hasRedundantTextChildren(n *axtree.SnapNode, verbose bool) bool {
	if verbose || n.Name == "" || len(n.Children) == 0 {
		return false
	}
	text := ""
	for _, c := range n.Children {
		if c.Role != "StaticText" || len(c.Children) != 0 {
			return false
		}
		text += c.Name
	}
	return stripWhitespace(text) == stripWhitespace(n.Name)
}

// stripWhitespace 官方 stripWhitespace：移除全部空白字符（SnapshotFormatter.ts:200-202）。
func stripWhitespace(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '\v', '\f', 0x85, 0xA0:
			// 空白字符跳过
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// --- 语义字典：对 ui_map 输出的元素做语义增强（原 semantics.go） ---
// （文本/结构模式 → 语义说明；前端补 aria-label 前的兜底；渲染时对每个
// 元素按规则表顺序匹配，命中即取第一条（更具体的规则排在前面），
// 在行尾追加「# 语义：…」注释。）

// semanticRule 单条语义规则。pattern 对元素 name 做匹配：
// exact=true 时要求完全相等（大小写敏感），否则做包含匹配（大小写不敏感）。
type semanticRule struct {
	pattern string
	note    string
	exact   bool
}

// semanticRules 有序规则表（先匹配先得，长词/专有词在前避免被泛化规则截胡）。
var semanticRules = []semanticRule{
	// --- 模式切换（精确短语，避免「本地」被泛化匹配） ---
	{"本地V3", "切换到本地 V3 模式", false},
	{"公网V3", "切换到公网 V3 模式", false},
	{"本地V2", "切换到本地 V2 模式", false},

	// --- 工具栏批量/投屏类 ---
	{"关闭投屏", "停止当前投屏会话", false},
	{"重置投屏", "重置投屏状态", false},
	{"全选", "全选当前列表设备", false},
	{"批量操作", "打开批量操作菜单（重启/关机/删除等）", false},
	{"投屏", "发起投屏", false},
	{"上传", "上传镜像/文件", false},
	{"控制", "打开远程控制", false},

	// --- 设备/分组管理 ---
	{"创建云机", "打开创建云机面板", false},
	{"添加设备", "添加新 ARM 设备", false},
	{"移除离线", "移除离线设备", false},
	{"重命名分组", "重命名当前分组", false},
	{"删除分组", "删除当前分组（组内设备不受影响）", false},
	{"+", "在此设备上创建云机（设备行加号）", true},

	// --- 导航 Tab（9 个） ---
	{"主机", "切换到主机页签", false},
	{"智能体", "切换到智能体页签", false},
	{"流媒体", "切换到流媒体页签", false},
	{"镜像", "切换到镜像页签", false},
	{"授权", "切换到授权页签", false},
	{"VPC", "切换到 VPC 页签", false},
	{"机型", "切换到机型页签", false},
	{"维护", "切换到维护页签", false},
	{"无盘", "切换到无盘页签", false},

	// --- 顶部功能入口 ---
	{"任务队列", "打开任务队列面板", false},
	{"问题反馈", "打开问题反馈", false},
	{"设置", "打开设置页", false},

	// --- 危险/确认类（放最后兜底） ---
	{"删除", "删除目标（不可恢复，谨慎点击）", false},
	{"关机", "关机目标设备/云机", false},
	{"重启", "重启目标设备/云机", false},
	{"重置", "重置目标（数据可能丢失）", false},
	{"搜索", "搜索框", false},
	{"刷新", "刷新数据", false},
}

// semanticNote 返回元素 name 命中的语义说明（未命中返回空串）。
func semanticNote(name string) string {
	if name == "" {
		return ""
	}
	lower := strings.ToLower(name)
	for _, r := range semanticRules {
		if r.exact {
			if name == r.pattern {
				return r.note
			}
			continue
		}
		if strings.Contains(lower, strings.ToLower(r.pattern)) {
			return r.note
		}
	}
	return ""
}
