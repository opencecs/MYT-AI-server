package axtree

// Package axtree — chrome-devtools-mcp「快照 + uid」机制的 AX 树处理层，
// 1:1 移植自官方依赖 puppeteer-core 25.13.0 的
// packages/puppeteer-core/src/cdp/Accessibility.ts（AXNode 建树、
// isInteresting 裁剪、serialize 字段模型）。行内注释标注官方对应位置。
//
// 从 uimap 包拆出的独立子包：只依赖标准库，uimap 单向引用本包
// （snapshot.go 建树/裁剪/序列化，snapshot_format.go 渲染 SnapNode）。

import (
	"strings"
)

// AxValue CDP AXNode 的 {type, value} 复合字段。
type AxValue struct {
	Value any `json:"value"`
}

// AxProperty AXNode.properties 列表项（官方按 name 的小写形式参与序列化）。
type AxProperty struct {
	Name  string  `json:"name"`
	Value AxValue `json:"value"`
}

// AxNode Accessibility.getFullAXTree 返回的单个节点（只保留官方用到的字段）。
// 注意：CDP 字段名为 backendDOMNodeId，puppeteer 将其序列化为 backendNodeId；
// WebView2 实测该字段可能全为 0（见 uimap 的合并兜底）。
type AxNode struct {
	NodeID        string       `json:"nodeId"`
	BackendNodeID int64        `json:"backendNodeId"`
	Ignored       bool         `json:"ignored"`
	Role          AxValue      `json:"role"`
	Name          AxValue      `json:"name"`
	Value         AxValue      `json:"value"`
	Description   AxValue      `json:"description"`
	Properties    []AxProperty `json:"properties"`
	ChildIDs      []string     `json:"childIds"`
}

// axInteractive 可交互角色集合（puppeteer isControl 角色表的超集，
// 保留 DOM 兜底判定使用；官方 isControl 见 Accessibility.ts:521-548）。
var axInteractive = map[string]bool{
	"button": true, "link": true, "textbox": true, "searchbox": true,
	"checkbox": true, "radio": true, "combobox": true, "listbox": true,
	"option": true, "menuitem": true, "menuitemcheckbox": true, "menuitemradio": true,
	"tab": true, "switch": true, "slider": true, "spinbutton": true,
	"textarea": true, "select": true, "treeitem": true,
}

// AxParsed puppeteer AXNode 类的 Go 等价物（Accessibility.ts:369-430 构造逻辑）。
type AxParsed struct {
	payload  *AxNode
	role     string
	name     string
	children []*AxParsed

	ignored         bool
	hidden          bool
	focusable       bool
	editable        bool // 'editable' 属性出现即 true（官方 #editable）
	richlyEditable  bool // editable=richtext（官方 #richlyEditable）
	busy            bool
	modal           bool
	live            string
	roledescription string
	hasErrormessage bool
	hasDetails      bool

	focusableChildCache *bool
}

// newAXParsed 从 payload 解析出语义标记（官方 AXNode constructor）。
func newAXParsed(payload *AxNode) *AxParsed {
	n := &AxParsed{payload: payload, role: "Unknown"}
	if s, ok := payload.Role.Value.(string); ok && s != "" {
		n.role = s
	}
	n.ignored = payload.Ignored
	if s, ok := payload.Name.Value.(string); ok {
		n.name = s
	}
	for _, p := range payload.Properties {
		switch p.Name {
		case "editable":
			n.richlyEditable = p.Value.Value == "richtext"
			n.editable = true
		case "focusable":
			n.focusable = truthy(p.Value.Value)
		case "hidden":
			n.hidden = truthy(p.Value.Value)
		case "busy":
			n.busy = truthy(p.Value.Value)
		case "live":
			if s, ok := p.Value.Value.(string); ok {
				n.live = s
			}
		case "modal":
			n.modal = truthy(p.Value.Value)
		case "roledescription":
			if s, ok := p.Value.Value.(string); ok {
				n.roledescription = s
			}
		case "errormessage":
			n.hasErrormessage = true
		case "details":
			n.hasDetails = true
		}
	}
	return n
}

// truthy CDP 属性值的布尔判定（布尔原样，字符串按 "true"）。
func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true"
	}
	return false
}

// BuildAXTree 官方 AXNode.createTree：按 childIds 建立父子关系，
// 返回未被引用为子节点的根列表（保持原始顺序）。
func BuildAXTree(payloads []AxNode) []*AxParsed {
	byID := make(map[string]*AxParsed, len(payloads))
	for i := range payloads {
		byID[payloads[i].NodeID] = newAXParsed(&payloads[i])
	}
	isChild := make(map[string]bool)
	for _, n := range byID {
		for _, cid := range n.payload.ChildIDs {
			if c, ok := byID[cid]; ok {
				n.children = append(n.children, c)
				isChild[cid] = true
			}
		}
	}
	roots := make([]*AxParsed, 0, 1)
	for i := range payloads {
		if !isChild[payloads[i].NodeID] {
			roots = append(roots, byID[payloads[i].NodeID])
		}
	}
	return roots
}

// isPlainTextField 官方 #isPlainTextField（Accessibility.ts:432-440）。
func (n *AxParsed) isPlainTextField() bool {
	if n.richlyEditable {
		return false
	}
	if n.editable {
		return true
	}
	return n.role == "textbox" || n.role == "searchbox"
}

// isTextOnlyObject 官方 #isTextOnlyObject（Accessibility.ts:442-450）。
func (n *AxParsed) isTextOnlyObject() bool {
	switch n.role {
	case "LineBreak", "text", "InlineTextBox", "StaticText":
		return true
	}
	return false
}

// hasFocusableChild 官方 #hasFocusableChild（带缓存，Accessibility.ts:452-463）。
func (n *AxParsed) hasFocusableChild() bool {
	if n.focusableChildCache == nil {
		v := false
		for _, c := range n.children {
			if c.focusable || c.hasFocusableChild() {
				v = true
				break
			}
		}
		n.focusableChildCache = &v
	}
	return *n.focusableChildCache
}

// isLeafNode 官方 isLeafNode（Accessibility.ts:478-519）。
func (n *AxParsed) isLeafNode() bool {
	if len(n.children) == 0 {
		return true
	}
	if n.isPlainTextField() || n.isTextOnlyObject() {
		return true
	}
	switch n.role {
	case "doc-cover", "graphics-symbol", "img", "image", "Meter",
		"scrollbar", "slider", "separator", "progressbar":
		return true
	}
	if n.hasFocusableChild() {
		return false
	}
	if n.role == "heading" && n.name != "" {
		return true
	}
	return false
}

// isControl 官方 isControl（Accessibility.ts:521-548）。
func (n *AxParsed) isControl() bool {
	switch n.role {
	case "button", "checkbox", "ColorWell", "combobox", "DisclosureTriangle",
		"listbox", "menu", "menubar", "menuitem", "menuitemcheckbox",
		"menuitemradio", "radio", "scrollbar", "searchbox", "slider",
		"spinbutton", "switch", "tab", "textbox", "tree", "treeitem":
		return true
	}
	return false
}

// isLandmark 官方 isLandmark（Accessibility.ts:550-564）。
func (n *AxParsed) isLandmark() bool {
	switch n.role {
	case "banner", "complementary", "contentinfo", "form", "main",
		"navigation", "region", "search":
		return true
	}
	return false
}

// isInteresting 官方 isInteresting（Accessibility.ts:566-600）。
func (n *AxParsed) isInteresting(insideControl bool) bool {
	if n.role == "Ignored" || n.hidden || n.ignored {
		return false
	}
	if n.isLandmark() {
		return true
	}
	if n.focusable || n.richlyEditable || n.busy ||
		(n.live != "" && n.live != "off") || n.modal ||
		n.hasErrormessage || n.hasDetails || n.roledescription != "" {
		return true
	}
	if n.isControl() {
		return true
	}
	if insideControl {
		return false
	}
	return n.isLeafNode() && (n.name != "" || n.payload.Description.Value != nil)
}

// CollectInteresting 官方 collectInterestingNodes（Accessibility.ts:351-366）。
func CollectInteresting(set map[*AxParsed]bool, n *AxParsed, insideControl bool) {
	if n.isInteresting(insideControl) {
		set[n] = true
	}
	if n.isLeafNode() {
		return
	}
	insideControl = insideControl || n.isControl()
	for _, c := range n.children {
		CollectInteresting(set, c, insideControl)
	}
}

// SnapNode 官方 SerializedAXNode 的 Go 等价物（Accessibility.ts:18-141）。
// uid 由 uimap 的 uidAllocator 分配；Fields 为参与渲染的其余字段
// （渲染时按 key 排序，规则见 uimap/snapshot_format.go）。
type SnapNode struct {
	UID       string
	Role      string
	Name      string
	Fields    map[string]any
	BackendID int64 // 官方 backendNodeId（= CDP backendDOMNodeId）
	AxID      string
	Children  []*SnapNode
}

// serialize 官方 AXNode.serialize（Accessibility.ts:602-777）：
// 属性表 → 用户字符串 / 布尔 / 三态 / 数值 / 枚举字段，逐组照抄。
func (n *AxParsed) serialize() *SnapNode {
	props := make(map[string]any, len(n.payload.Properties)+3)
	for _, p := range n.payload.Properties {
		props[strings.ToLower(p.Name)] = p.Value.Value
	}
	if n.payload.Name.Value != nil {
		props["name"] = n.payload.Name.Value
	}
	if n.payload.Value.Value != nil {
		props["value"] = n.payload.Value.Value
	}
	if n.payload.Description.Value != nil {
		props["description"] = n.payload.Description.Value
	}

	sn := &SnapNode{
		Role:      n.role,
		Name:      n.name,
		Fields:    map[string]any{},
		BackendID: n.payload.BackendNodeID,
		AxID:      n.payload.NodeID,
	}

	// 用户字符串属性（name 单独存放，其余进 Fields）。
	for _, k := range []string{"name", "value", "description", "keyshortcuts",
		"roledescription", "valuetext", "url"} {
		v, ok := props[k]
		if !ok {
			continue
		}
		if k == "name" {
			if s, isStr := v.(string); isStr && sn.Name == "" {
				sn.Name = s
			}
			continue
		}
		sn.Fields[k] = v
	}

	// 布尔属性（官方对 RootWebArea 跳过 focused）。
	for _, k := range []string{"disabled", "expanded", "focused", "modal",
		"multiline", "multiselectable", "readonly", "required", "selected",
		"busy", "atomic"} {
		v, ok := props[k]
		if !ok {
			continue
		}
		if k == "focused" && n.role == "RootWebArea" {
			continue
		}
		sn.Fields[k] = truthy(v)
	}

	// 三态属性：'mixed' → "mixed"，'true' → true，其余 → false（照抄官方）。
	for _, k := range []string{"checked", "pressed"} {
		v, ok := props[k]
		if !ok {
			continue
		}
		switch s, _ := v.(string); s {
		case "mixed":
			sn.Fields[k] = "mixed"
		case "true":
			sn.Fields[k] = true
		default:
			sn.Fields[k] = false
		}
	}

	// 数值属性。
	for _, k := range []string{"level", "valuemax", "valuemin"} {
		switch v := props[k].(type) {
		case float64:
			sn.Fields[k] = v
		case int:
			sn.Fields[k] = float64(v)
		case int64:
			sn.Fields[k] = float64(v)
		}
	}

	// 枚举属性：空串或 "false" 不输出（照抄官方）。
	for _, k := range []string{"autocomplete", "haspopup", "invalid",
		"orientation", "live", "relevant", "errormessage", "details"} {
		s, ok := props[k].(string)
		if !ok || s == "" || s == "false" {
			continue
		}
		sn.Fields[k] = s
	}
	return sn
}

// SerializeTree 官方 serializeTree（Accessibility.ts:325-349）：
// interesting 为 nil 时保留全部节点（verbose 模式）；否则未命中 interesting
// 的节点被裁掉、其子节点上移一层。
func SerializeTree(n *AxParsed, interesting map[*AxParsed]bool) []*SnapNode {
	var out []*SnapNode
	for _, c := range n.children {
		out = append(out, SerializeTree(c, interesting)...)
	}
	_, keep := interesting[n] // nil map 查询安全返回 false
	if interesting == nil || keep {
		sn := n.serialize()
		sn.Children = out
		return []*SnapNode{sn}
	}
	return out
}
