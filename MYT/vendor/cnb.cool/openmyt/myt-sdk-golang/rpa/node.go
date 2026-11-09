package rpa

import (
	"context"
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// nodeFinder 节点查找条件接口
type nodeFinder interface {
	isInclude(node *Node) bool
}

// --- 字符串匹配 Finder ---

// stringEqualFinder 精确匹配
type stringEqualFinder struct {
	field string
	value string
}

func (f *stringEqualFinder) isInclude(node *Node) bool {
	v := node.getStringField(f.field)
	return v == f.value
}

// stringStartWithFinder 前缀匹配
type stringStartWithFinder struct {
	field string
	value string
}

func (f *stringStartWithFinder) isInclude(node *Node) bool {
	v := node.getStringField(f.field)
	return strings.HasPrefix(v, f.value)
}

// stringEndWithFinder 后缀匹配
type stringEndWithFinder struct {
	field string
	value string
}

func (f *stringEndWithFinder) isInclude(node *Node) bool {
	v := node.getStringField(f.field)
	return strings.HasSuffix(v, f.value)
}

// stringContainWithFinder 包含匹配
type stringContainWithFinder struct {
	field string
	value string
}

func (f *stringContainWithFinder) isInclude(node *Node) bool {
	v := node.getStringField(f.field)
	return strings.Contains(v, f.value)
}

// stringMatchWithFinder 正则全匹配（与C++ std::regex_match语义一致）
// 通过在编译时添加锚定 ^(?:pattern)$ 来确保全匹配，
// 而非依赖 FindString==v（后者在交替模式下不正确，如 a|axb 匹配 "axb"）
type stringMatchWithFinder struct {
	field   string
	pattern *regexp.Regexp
	err     error // 存储编译错误，避免MustCompile panic
}

func (f *stringMatchWithFinder) isInclude(node *Node) bool {
	if f.err != nil {
		return false
	}
	v := node.getStringField(f.field)
	return f.pattern.MatchString(v)
}

// --- 布尔匹配 Finder ---

// boolFinder 布尔属性匹配
type boolFinder struct {
	field string
	value bool
}

func (f *boolFinder) isInclude(node *Node) bool {
	v := node.getBoolField(f.field)
	return v == f.value
}

// --- 整数匹配 Finder ---

// intFinder 整数属性匹配
type intFinder struct {
	field string
	value int
}

func (f *intFinder) isInclude(node *Node) bool {
	v := node.getIntField(f.field)
	return v == f.value
}

// --- Bounds 匹配 Finder ---

// boundsEqualFinder bounds精确匹配
type boundsEqualFinder struct {
	bounds [4]int
}

func (f *boundsEqualFinder) isInclude(node *Node) bool {
	return node.Bounds == f.bounds
}

// boundsInsideFinder bounds包含匹配（节点bounds在指定区域内）
type boundsInsideFinder struct {
	bounds [4]int
}

func (f *boundsInsideFinder) isInclude(node *Node) bool {
	return node.Bounds[0] >= f.bounds[0] &&
		node.Bounds[1] >= f.bounds[1] &&
		node.Bounds[2] <= f.bounds[2] &&
		node.Bounds[3] <= f.bounds[3]
}

// --- Node 字段访问辅助方法 ---

func (n *Node) getStringField(field string) string {
	switch field {
	case "Text":
		return n.Text
	case "Desc":
		return n.Desc
	case "Package":
		return n.Package
	case "Class":
		return n.Class
	case "ID":
		return n.ID
	}
	return ""
}

func (n *Node) getBoolField(field string) bool {
	switch field {
	case "Checkable":
		return n.Checkable
	case "Checked":
		return n.Checked
	case "Clickable":
		return n.Clickable
	case "Focusable":
		return n.Focusable
	case "Focused":
		return n.Focused
	case "Scrollable":
		return n.Scrollable
	case "LongClickable":
		return n.LongClickable
	case "Password":
		return n.Password
	case "Selected":
		return n.Selected
	case "Visible":
		return n.Visible
	case "Enabled":
		return n.Enabled
	}
	return false
}

func (n *Node) getIntField(field string) int {
	switch field {
	case "Index":
		return n.Index
	case "Depth":
		return n.Depth
	case "DrawingOrder":
		return n.DrawingOrder
	}
	return -1 // 未知字段默认-1，与Node整数字段默认值一致
}

// --- xmlNode XML解析中间结构 ---

type xmlHierarchy struct {
	XMLName  xml.Name  `xml:"hierarchy"`
	Rotation int       `xml:"rotation,attr"`
	Nodes    []xmlNode `xml:"node"`
}

type xmlNode struct {
	XMLName      xml.Name  `xml:"node"`
	Text         string    `xml:"text,attr"`
	Desc         string    `xml:"content-desc,attr"`
	Package      string    `xml:"package,attr"`
	Class        string    `xml:"class,attr"`
	ID           string    `xml:"resource-id,attr"`
	Index        *int      `xml:"index,attr"`
	Depth        *int      `xml:"depth,attr"`
	DrawingOrder *int      `xml:"drawingorder,attr"`
	Bounds       string    `xml:"bounds,attr"`
	Checkable    bool      `xml:"checkable,attr"`
	Checked      bool      `xml:"checked,attr"`
	Clickable    bool      `xml:"clickable,attr"`
	Enabled      bool      `xml:"enabled,attr"`
	Focusable    bool      `xml:"focusable,attr"`
	Focused      bool      `xml:"focused,attr"`
	Scrollable   bool      `xml:"scrollable,attr"`
	LongClick    bool      `xml:"long-clickable,attr"`
	Password     bool      `xml:"password,attr"`
	Selected     bool      `xml:"selected,attr"`
	Visible      bool      `xml:"visible,attr"`
	Nodes        []xmlNode `xml:"node"`
}

// --- Selector 方法 ---

// NewSelector 创建节点选择器
func (c *Client) NewSelector() *Selector {
	return &Selector{client: c}
}

// Clear 清空选择器所有条件（与C++ clearSelector对齐）
func (s *Selector) Clear() *Selector {
	s.finders = s.finders[:0]
	return s
}

// --- 字符串匹配条件 ---

// TextEqual 精确匹配text
func (s *Selector) TextEqual(text string) *Selector {
	s.finders = append(s.finders, &stringEqualFinder{field: "Text", value: text})
	return s
}

// TextStartWith 前缀匹配text
func (s *Selector) TextStartWith(text string) *Selector {
	s.finders = append(s.finders, &stringStartWithFinder{field: "Text", value: text})
	return s
}

// TextEndWith 后缀匹配text
func (s *Selector) TextEndWith(text string) *Selector {
	s.finders = append(s.finders, &stringEndWithFinder{field: "Text", value: text})
	return s
}

// TextContainWith 包含匹配text
func (s *Selector) TextContainWith(text string) *Selector {
	s.finders = append(s.finders, &stringContainWithFinder{field: "Text", value: text})
	return s
}

// TextMatchWith 正则全匹配text（无效正则不匹配任何节点，不panic）
func (s *Selector) TextMatchWith(pattern string) *Selector {
	re, err := regexp.Compile("^(?:" + pattern + ")$")
	s.finders = append(s.finders, &stringMatchWithFinder{field: "Text", pattern: re, err: err})
	return s
}

// IdEqual 精确匹配resource-id
func (s *Selector) IdEqual(id string) *Selector {
	s.finders = append(s.finders, &stringEqualFinder{field: "ID", value: id})
	return s
}

// IdStartWith 前缀匹配resource-id
func (s *Selector) IdStartWith(id string) *Selector {
	s.finders = append(s.finders, &stringStartWithFinder{field: "ID", value: id})
	return s
}

// IdContainWith 包含匹配resource-id
func (s *Selector) IdContainWith(id string) *Selector {
	s.finders = append(s.finders, &stringContainWithFinder{field: "ID", value: id})
	return s
}

// IdEndWith 后缀匹配resource-id
func (s *Selector) IdEndWith(id string) *Selector {
	s.finders = append(s.finders, &stringEndWithFinder{field: "ID", value: id})
	return s
}

// IdMatchWith 正则全匹配resource-id（无效正则不匹配任何节点，不panic）
func (s *Selector) IdMatchWith(pattern string) *Selector {
	re, err := regexp.Compile("^(?:" + pattern + ")$")
	s.finders = append(s.finders, &stringMatchWithFinder{field: "ID", pattern: re, err: err})
	return s
}

// DescEqual 精确匹配content-desc
func (s *Selector) DescEqual(desc string) *Selector {
	s.finders = append(s.finders, &stringEqualFinder{field: "Desc", value: desc})
	return s
}

// DescContainWith 包含匹配content-desc
func (s *Selector) DescContainWith(desc string) *Selector {
	s.finders = append(s.finders, &stringContainWithFinder{field: "Desc", value: desc})
	return s
}

// DescStartWith 前缀匹配content-desc
func (s *Selector) DescStartWith(desc string) *Selector {
	s.finders = append(s.finders, &stringStartWithFinder{field: "Desc", value: desc})
	return s
}

// DescEndWith 后缀匹配content-desc
func (s *Selector) DescEndWith(desc string) *Selector {
	s.finders = append(s.finders, &stringEndWithFinder{field: "Desc", value: desc})
	return s
}

// DescMatchWith 正则全匹配content-desc（无效正则不匹配任何节点，不panic）
func (s *Selector) DescMatchWith(pattern string) *Selector {
	re, err := regexp.Compile("^(?:" + pattern + ")$")
	s.finders = append(s.finders, &stringMatchWithFinder{field: "Desc", pattern: re, err: err})
	return s
}

// ClassEqual 精确匹配class
func (s *Selector) ClassEqual(class string) *Selector {
	s.finders = append(s.finders, &stringEqualFinder{field: "Class", value: class})
	return s
}

// ClassContainWith 包含匹配class
func (s *Selector) ClassContainWith(class string) *Selector {
	s.finders = append(s.finders, &stringContainWithFinder{field: "Class", value: class})
	return s
}

// ClassStartWith 前缀匹配class
func (s *Selector) ClassStartWith(class string) *Selector {
	s.finders = append(s.finders, &stringStartWithFinder{field: "Class", value: class})
	return s
}

// ClassEndWith 后缀匹配class
func (s *Selector) ClassEndWith(class string) *Selector {
	s.finders = append(s.finders, &stringEndWithFinder{field: "Class", value: class})
	return s
}

// ClassMatchWith 正则全匹配class（无效正则不匹配任何节点，不panic）
func (s *Selector) ClassMatchWith(pattern string) *Selector {
	re, err := regexp.Compile("^(?:" + pattern + ")$")
	s.finders = append(s.finders, &stringMatchWithFinder{field: "Class", pattern: re, err: err})
	return s
}

// PackageEqual 精确匹配package
func (s *Selector) PackageEqual(pkg string) *Selector {
	s.finders = append(s.finders, &stringEqualFinder{field: "Package", value: pkg})
	return s
}

// PackageStartWith 前缀匹配package
func (s *Selector) PackageStartWith(pkg string) *Selector {
	s.finders = append(s.finders, &stringStartWithFinder{field: "Package", value: pkg})
	return s
}

// PackageEndWith 后缀匹配package
func (s *Selector) PackageEndWith(pkg string) *Selector {
	s.finders = append(s.finders, &stringEndWithFinder{field: "Package", value: pkg})
	return s
}

// PackageContainWith 包含匹配package
func (s *Selector) PackageContainWith(pkg string) *Selector {
	s.finders = append(s.finders, &stringContainWithFinder{field: "Package", value: pkg})
	return s
}

// PackageMatchWith 正则全匹配package（无效正则不匹配任何节点，不panic）
func (s *Selector) PackageMatchWith(pattern string) *Selector {
	re, err := regexp.Compile("^(?:" + pattern + ")$")
	s.finders = append(s.finders, &stringMatchWithFinder{field: "Package", pattern: re, err: err})
	return s
}

// --- 布尔匹配条件 ---

// Clickable 匹配clickable属性
func (s *Selector) Clickable(val bool) *Selector {
	s.finders = append(s.finders, &boolFinder{field: "Clickable", value: val})
	return s
}

// Checkable 匹配checkable属性
func (s *Selector) Checkable(val bool) *Selector {
	s.finders = append(s.finders, &boolFinder{field: "Checkable", value: val})
	return s
}

// Enabled 匹配enabled属性
func (s *Selector) Enabled(val bool) *Selector {
	s.finders = append(s.finders, &boolFinder{field: "Enabled", value: val})
	return s
}

// Focusable 匹配focusable属性
func (s *Selector) Focusable(val bool) *Selector {
	s.finders = append(s.finders, &boolFinder{field: "Focusable", value: val})
	return s
}

// Focused 匹配focused属性
func (s *Selector) Focused(val bool) *Selector {
	s.finders = append(s.finders, &boolFinder{field: "Focused", value: val})
	return s
}

// Checked 匹配checked属性
func (s *Selector) Checked(val bool) *Selector {
	s.finders = append(s.finders, &boolFinder{field: "Checked", value: val})
	return s
}

// Scrollable 匹配scrollable属性
func (s *Selector) Scrollable(val bool) *Selector {
	s.finders = append(s.finders, &boolFinder{field: "Scrollable", value: val})
	return s
}

// LongClickable 匹配long-clickable属性
func (s *Selector) LongClickable(val bool) *Selector {
	s.finders = append(s.finders, &boolFinder{field: "LongClickable", value: val})
	return s
}

// Password 匹配password属性
func (s *Selector) Password(val bool) *Selector {
	s.finders = append(s.finders, &boolFinder{field: "Password", value: val})
	return s
}

// Selected 匹配selected属性
func (s *Selector) Selected(val bool) *Selector {
	s.finders = append(s.finders, &boolFinder{field: "Selected", value: val})
	return s
}

// Visible 匹配visible属性
func (s *Selector) Visible(val bool) *Selector {
	s.finders = append(s.finders, &boolFinder{field: "Visible", value: val})
	return s
}

// --- 整数匹配条件 ---

// IndexEqual 匹配index属性
func (s *Selector) IndexEqual(val int) *Selector {
	s.finders = append(s.finders, &intFinder{field: "Index", value: val})
	return s
}

// DepthEqual 匹配depth属性
func (s *Selector) DepthEqual(val int) *Selector {
	s.finders = append(s.finders, &intFinder{field: "Depth", value: val})
	return s
}

// DrawingOrderEqual 匹配drawingOrder属性
func (s *Selector) DrawingOrderEqual(val int) *Selector {
	s.finders = append(s.finders, &intFinder{field: "DrawingOrder", value: val})
	return s
}

// --- Bounds 匹配条件 ---

// BoundsEqual bounds精确匹配
func (s *Selector) BoundsEqual(l, t, r, b int) *Selector {
	s.finders = append(s.finders, &boundsEqualFinder{bounds: [4]int{l, t, r, b}})
	return s
}

// BoundsInside bounds包含匹配
func (s *Selector) BoundsInside(l, t, r, b int) *Selector {
	s.finders = append(s.finders, &boundsInsideFinder{bounds: [4]int{l, t, r, b}})
	return s
}

// --- FindNodes 查找节点 ---

// InfiniteTimeout 用于FindNodes的timeout参数，表示永久等待直到找到匹配节点
const InfiniteTimeout = time.Duration(-1)

// FindNodes 查找匹配条件的节点集
// maxCount: 最多返回的节点数量（0=最多1个，与C++一致；>0限制数量）
// timeout: 查找超时（0=不等待，InfiniteTimeout=-1=一直等待，>0指定等待时间）
//
// 流程与C++ SDK一致：先dumpNodeXml获取XML，解析为节点树，遍历匹配
func (s *Selector) FindNodes(ctx context.Context, maxCount int, timeout time.Duration) (*NodeSet, error) {
	// timeout语义与C++一致：
	//   timeout > 0: 等待指定时间
	//   timeout == 0: 不等待（单次尝试）
	//   timeout < 0: 永久等待（C++的-1语义）
	deadline := time.Time{}
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	infiniteWait := timeout < 0

	for {
		xmlStr, err := s.client.DumpNodeXml(ctx, true)
		if err != nil {
			// 与C++一致：dump失败时继续重试而非立即返回错误
			if timeout == 0 {
				return &NodeSet{client: s.client}, nil
			}
			if !infiniteWait && timeout > 0 && time.Now().After(deadline) {
				return &NodeSet{client: s.client}, nil
			}
			select {
			case <-ctx.Done():
				return &NodeSet{client: s.client}, ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}

		// 解析XML为节点树（带客户端引用，支持Node.Click/LongClick）
		nodes := parseXMLNodes(xmlStr, s.client)

		// 遍历匹配
		// maxCount语义与C++一致：maxCount=0返回最多1个，maxCount>0限制数量，不限制需传很大的值
		var matched []*Node
		for _, node := range nodes {
			if s.matchNode(node) {
				matched = append(matched, node)
				if maxCount == 0 || (maxCount > 0 && len(matched) >= maxCount) {
					break
				}
			}
		}

		if len(matched) > 0 {
			return &NodeSet{client: s.client, nodes: matched}, nil
		}

		// 检查超时
		if timeout == 0 {
			return &NodeSet{client: s.client}, nil
		}
		if !infiniteWait && timeout > 0 && time.Now().After(deadline) {
			return &NodeSet{client: s.client}, nil
		}

		// 等待重试
		select {
		case <-ctx.Done():
			return &NodeSet{client: s.client}, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// matchNode 检查节点是否匹配所有条件
func (s *Selector) matchNode(node *Node) bool {
	for _, finder := range s.finders {
		if !finder.isInclude(node) {
			return false
		}
	}
	return true
}

// parseXMLNodes 解析XML为扁平化节点列表
// client参数可选：非nil时节点可调用Click/LongClick
func parseXMLNodes(xmlStr string, client ...*Client) []*Node {
	var h xmlHierarchy
	if err := xml.Unmarshal([]byte(xmlStr), &h); err != nil {
		return nil
	}

	var c *Client
	if len(client) > 0 {
		c = client[0]
	}

	var nodes []*Node
	for i := range h.Nodes {
		collectNodes(&h.Nodes[i], &nodes, h.Rotation, nil, c)
	}
	return nodes
}

// collectNodes 递归收集所有节点，同时构建树关系（parent/children）和客户端引用
func collectNodes(xn *xmlNode, nodes *[]*Node, rotation int, parent *Node, client *Client) {
	node := xmlNodeToNode(xn)
	node.Rotation = rotation
	node.parent = parent
	node.client = client
	if parent != nil {
		parent.children = append(parent.children, node)
	}
	*nodes = append(*nodes, node)

	for i := range xn.Nodes {
		collectNodes(&xn.Nodes[i], nodes, rotation, node, client)
	}
}

// xmlNodeToNode 将XML节点转为Node
func xmlNodeToNode(xn *xmlNode) *Node {
	// 整数字段：与C++一致，缺失属性默认-1
	index := -1
	if xn.Index != nil {
		index = *xn.Index
	}
	depth := -1
	if xn.Depth != nil {
		depth = *xn.Depth
	}
	drawingOrder := -1
	if xn.DrawingOrder != nil {
		drawingOrder = *xn.DrawingOrder
	}

	node := &Node{
		Text:          xn.Text,
		Desc:          xn.Desc,
		Package:       xn.Package,
		Class:         xn.Class,
		ID:            xn.ID,
		Index:         index,
		Depth:         depth,
		DrawingOrder:  drawingOrder,
		Checkable:     xn.Checkable,
		Checked:       xn.Checked,
		Clickable:     xn.Clickable,
		Focusable:     xn.Focusable,
		Focused:       xn.Focused,
		Scrollable:    xn.Scrollable,
		LongClickable: xn.LongClick,
		Password:      xn.Password,
		Selected:      xn.Selected,
		Visible:       xn.Visible,
		Enabled:       xn.Enabled,
	}

	// 解析bounds: [l,t][r,b]
	node.Bounds = parseBounds(xn.Bounds)
	node.Center = [2]int{
		(node.Bounds[0] + node.Bounds[2]) / 2,
		(node.Bounds[1] + node.Bounds[3]) / 2,
	}

	return node
}

// parseBounds 解析bounds字符串 "[l,t][r,b]"
func parseBounds(s string) [4]int {
	var l, t, r, b int
	n, err := fmt.Sscanf(s, "[%d,%d][%d,%d]", &l, &t, &r, &b)
	if err != nil || n != 4 {
		return [4]int{}
	}
	return [4]int{l, t, r, b}
}

// --- NodeSet 操作方法 ---

// Size 返回节点数量
func (ns *NodeSet) Size() int {
	return len(ns.nodes)
}

// Get 按索引获取节点
func (ns *NodeSet) Get(index int) (*Node, error) {
	if index < 0 || index >= len(ns.nodes) {
		return nil, fmt.Errorf("index out of range: %d", index)
	}
	return ns.nodes[index], nil
}

// All 返回所有节点
func (ns *NodeSet) All() []*Node {
	return ns.nodes
}

// Click 点击节点（点击节点中心坐标）
// 与C++ SDK一致，使用pointer ID=8
const nodePointerID = 8

func (ns *NodeSet) Click(ctx context.Context, index int) error {
	if ns.client == nil {
		return fmt.Errorf("nodeset has no client reference")
	}
	node, err := ns.Get(index)
	if err != nil {
		return err
	}
	return ns.client.TouchClick(ctx, nodePointerID, node.Center[0], node.Center[1])
}

// LongClick 长按节点
func (ns *NodeSet) LongClick(ctx context.Context, index int) error {
	if ns.client == nil {
		return fmt.Errorf("nodeset has no client reference")
	}
	node, err := ns.Get(index)
	if err != nil {
		return err
	}
	// 模拟长按：down → 等3秒 → up
	if err := ns.client.TouchDown(ctx, nodePointerID, node.Center[0], node.Center[1]); err != nil {
		return err
	}
	// 支持ctx取消的长按等待
	select {
	case <-time.After(3 * time.Second):
	case <-ctx.Done():
		ns.client.TouchUp(ctx, nodePointerID, node.Center[0], node.Center[1])
		return ctx.Err()
	}
	return ns.client.TouchUp(ctx, nodePointerID, node.Center[0], node.Center[1])
}

// --- DumpNodeXmlEx ---

// DumpNodeXmlEx 获取界面节点XML（带超时和新模式参数）
// useNewMode: 使用新节点模式
// timeout: 超时时间（毫秒）
func (c *Client) DumpNodeXmlEx(ctx context.Context, useNewMode bool, timeout int) (string, error) {
	pkg := NewPkgData(nil)
	if useNewMode {
		pkg.WrapInt(1)
	} else {
		pkg.WrapInt(0)
	}
	pkg.WrapInt(timeout)

	data, err := pkg.Bytes()
	if err != nil {
		return "", err
	}

	resp, err := c.call(CMDDumpNode, data, 30*time.Second)
	if err != nil {
		return "", err
	}

	return string(resp), nil
}
