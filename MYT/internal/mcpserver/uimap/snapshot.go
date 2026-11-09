package uimap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/moyunteng/myt/internal/mcpserver/cdp"
	"github.com/moyunteng/myt/internal/mcpserver/reg"
	"github.com/moyunteng/myt/internal/mcpserver/uimap/axtree"
)

// ui_map：界面观测快照 + uid 引用点击。主路径为 Runtime.evaluate 注入采集
//（capture_eval.go，cdp-shot.js dumpPage 同源，真机实战验证可用），辅助输出
// 为可访问性树（axtree 子包 + snapshot_format.go，chrome-devtools-mcp 1:1 移植）。
//
// uid 机制：
//   - 可交互元素 uid：注入采集按文档序编号，key 为 `eval_<快照号>_<序号>`
//     （每快照全新，不跨快照复用，与「uid 仅本次快照有效」约定一致）；
//     uid 登记为元素身份（RefTable），动作工具按身份在页面内重新定位执行，
//     全程不依赖 CDP 节点 id（见 browser/act.go）。
//   - 树行 uid：沿用官方 TextSnapshot 机制（本文件 uidAllocator），key 为
//     `${loaderId}_${backendNodeId}`，跨快照复用；WebView2 下 backendNodeId
//     常为 0，故树行 uid 仅供阅读，可点击 uid 以 interactiveElements 为准。

// maxNodesDefault/maxNodesCap ui_map 返回节点数：默认 300，上限 1000。
const (
	maxNodesDefault = 300
	maxNodesCap     = 1000
)

type Args struct {
	Region   string `json:"region,omitempty" jsonschema_desc:"区域过滤：all（默认）/sidebar（左侧栏）/toolbar（顶部工具栏）/main（主内容）/dialog（弹窗）"`
	MaxNodes int    `json:"maxNodes,omitempty" jsonschema_desc:"最多返回节点数（默认 300，上限 1000）"`
	Verbose  bool   `json:"verbose,omitempty" jsonschema_desc:"true 时输出完整可访问性树（不做 interesting 裁剪），默认 false（对标官方 take_snapshot 的 verbose）"`
}

// RefSink 快照产出的 uid→元素身份表的消费方（browser.Hub 实现：
// 每次 ui_map 重建后替换其身份表，click/type_text 等按 uid 查身份后
// 在页面内重新定位元素）。
type RefSink interface {
	ReplaceRefs(refs RefTable)
}

// Collector ui_map 快照编排器：持有 CDP 客户端、官方 TextSnapshot 的
// uid 复用表与快照号（原 Server 上的 uidReuse/nextSnapID 状态）。
type Collector struct {
	client *cdp.Client
	sinkMu sync.RWMutex // 保护 sink（SetSink 可在运行中接线，Collect 并发读）
	sink   RefSink

	uidReuse   map[string]string
	nextSnapID int
}

// NewCollector 创建快照编排器。sink 可为 nil（仅取快照文本，不产出 refs，
// 事后可用 SetSink 补接线）。
func NewCollector(client *cdp.Client, sink RefSink) *Collector {
	return &Collector{client: client, sink: sink, nextSnapID: 1}
}

// SetSink 接线/更换 refs 消费方（server.go 装配时先建 Hub 再回填）。
func (c *Collector) SetSink(sink RefSink) {
	c.sinkMu.Lock()
	defer c.sinkMu.Unlock()
	c.sink = sink
}

// currentSink 读取当前 sink（无则 nil）。
func (c *Collector) currentSink() RefSink {
	c.sinkMu.RLock()
	defer c.sinkMu.RUnlock()
	return c.sink
}

// cdpGate 校验 CDP 可用性，不可用返回友好错误（与 browser 包同一文案语义）。
func (c *Collector) cdpGate(ctx context.Context) error {
	if !c.client.Ready(ctx) {
		return errors.New(cdpUnavailableMsg)
	}
	return nil
}

// cdpUnavailableMsg CDP 未就绪时的统一提示（客户端启动时未开远程调试或窗口未创建）。
const cdpUnavailableMsg = "界面控制需重启客户端后可用（CDP 未就绪：客户端启动时未开启远程调试，或窗口尚未创建）"

// uidLifecycleNote uid 生命周期声明（随 ui_map 输出下发，提示修正 ③）。
const uidLifecycleNote = "uid 仅本次快照有效；操作报 uid 失效/过期时重新调用 ui_map 取新 uid，禁止用旧 uid 重试。" +
	"uid 以 interactiveElements 为准（tree 行的 uid 为辅助，WebView2 下可能不可点击）"

// clampMaxNodes 钳制 maxNodes：非正数落回默认，超上限钳到上限。
func clampMaxNodes(n int) int {
	if n <= 0 {
		return maxNodesDefault
	}
	if n > maxNodesCap {
		return maxNodesCap
	}
	return n
}

// normalizeRegion 规范化 region 参数（空/未知 → all）。
func normalizeRegion(s string) string {
	switch s {
	case "sidebar", "toolbar", "main", "dialog":
		return s
	default:
		return "all"
	}
}

// inRegion 按中心坐标判断节点归属区域（与工具栏/侧栏布局约定对齐）。
func inRegion(cx, cy float64, region string) bool {
	switch region {
	case "sidebar":
		return cx < 340
	case "toolbar":
		return cy < 110
	case "main":
		return cx >= 340 && cy >= 110
	default:
		return true
	}
}

// Register 注册 ui_map 工具（对标 chrome-devtools-mcp take_snapshot）。
func Register(srv *mcp.Server, sink reg.Sink, c *Collector) {
	reg.Reg(srv, sink, "ui_map",
		"获取客户端界面的「观察即分析」快照（复刻 chrome-devtools-mcp take_snapshot 并增强）："+
			"summary 为页面人话描述与建议下一步（先读它，按它的建议行动）；"+
			"state 为自动分析的局面（viewport 视口尺寸 / modal 检测到的全屏遮罩或弹窗（含弹窗内按钮 uid 与建议操作）/ activeTab 当前所在页签）；"+
			"interactiveElements 为全部可见可交互元素清单（tag/text/placeholder/value/disabled/rect/uid），是 uid 的权威来源；"+
			"tree 为可访问性树（辅助阅读结构，其行内 uid 仅供阅读，点击请用 interactiveElements 的 uid）。"+
			"点击/输入用 uid 定位（click/type_text/press_key/fill 的 ref 参数传 uid），优先 uid 勿用坐标。"+
			"uid 仅本次快照有效：操作报 uid 失效时重新调用 ui_map 取新 uid，禁止用旧 uid 重试。"+
			"verbose=true 输出完整树（默认 interesting 裁剪）；region 可按界面分区过滤（sidebar/toolbar/main/dialog，作用于 interactiveElements）；"+
			"maxNodes 限制树行数量（默认 300，上限 1000）。无破坏性。",
		func(ctx context.Context, a Args) (any, error) {
			if err := c.cdpGate(ctx); err != nil {
				return nil, err
			}
			return c.Collect(ctx, a)
		}, reg.ReadOnly())
}

// fetchLoaderID 取主 frame 的 loaderId（官方 uid key 的 loaderId 部分，
// puppeteer 取 frame._loaderId，Accessibility.ts:645）。失败返回空串
// （uid 复用退化为仅按 backendNodeId 区分，仍可用）。
func (c *Collector) fetchLoaderID(ctx context.Context, wsURL string) string {
	res, err := c.client.Call(ctx, wsURL, "Page.getResourceTree", map[string]any{}, 5*time.Second)
	if err != nil {
		return ""
	}
	var out struct {
		FrameTree struct {
			Frame struct {
				LoaderID string `json:"loaderId"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	if cdp.JSONUnmarshal(res, &out) != nil {
		return ""
	}
	return out.FrameTree.Frame.LoaderID
}

// assignTree 深度优先为序列化树分配 uid（官方 assignIds，TextSnapshot.ts:73-110）。
// 树行 uid 为辅助输出（WebView2 下 backendNodeId 常为 0，不可点击）；
// 可点击 uid 以注入采集的 interactiveElements 为准。
func assignTree(n *axtree.SnapNode, loaderID string, a *uidAllocator) {
	var key string
	if n.BackendID != 0 {
		key = fmt.Sprintf("%s_%d", loaderID, n.BackendID)
	} else {
		key = fmt.Sprintf("%s_0#%s", loaderID, n.AxID)
	}
	n.UID = a.assign(key, n.BackendID)
	for _, c := range n.Children {
		assignTree(c, loaderID, a)
	}
}

// Collect 执行快照（主路径 = 注入采集，cdp-shot.js dumpPage 同源）：
//  1. Runtime.evaluate 注入采集：可见可交互元素（打 data-mcp-i 标）/遮罩候选/视口/当前页签；
//  2. DOM 域对齐：querySelectorAll + getFlattenedDocument → uid→backendNodeId 注册 refs
//     （click/fill 等工具经 DOM.resolveNode(backendNodeId) 解析，下游链路不变）；
//  3. a11y 树（辅助输出）：getFullAXTree（失败降级 getPartialAXTree）→ 官方裁剪渲染；
//  4. summary/state/interactiveElements 分析（analyze.go / summary.go）。
func (c *Collector) Collect(ctx context.Context, a Args) (any, error) {
	wsURL, err := c.client.FirstPageTarget(ctx)
	if err != nil {
		return nil, err
	}

	// --- 主路径：注入采集（uid 与观测数据的唯一来源，失败直接报错） ---
	ev, err := c.evalCollect(ctx)
	if err != nil {
		return nil, fmt.Errorf("界面注入采集失败: %w", err)
	}
	alloc := newUIDAllocator(c)
	items, refs := itemsFromEval(ev, alloc)

	// --- a11y 树（辅助输出；注入采集只有可交互元素带坐标，tree 不做 region 过滤） ---
	if _, err := c.client.Call(ctx, wsURL, "Accessibility.enable", map[string]any{}, 5*time.Second); err != nil {
		return nil, fmt.Errorf("开启可访问性域失败: %w", err)
	}
	var payloads []axtree.AxNode
	degraded := ""
	res, err := c.client.Call(ctx, wsURL, "Accessibility.getFullAXTree", map[string]any{}, 15*time.Second)
	if err == nil {
		var out struct {
			Nodes []axtree.AxNode `json:"nodes"`
		}
		if jerr := cdp.JSONUnmarshal(res, &out); jerr == nil {
			payloads = out.Nodes
		}
	}
	if len(payloads) == 0 {
		payloads, degraded, err = c.partialAXTree(ctx, wsURL)
		if err != nil {
			return nil, err
		}
	}
	loaderID := c.fetchLoaderID(ctx, wsURL)

	// 官方 accessibility.snapshot({interestingOnly: !verbose})：includeIframes
	// 依赖 OOP iframe 采集（WebView2 单页场景无此需求，不移植）。
	roots := axtree.BuildAXTree(payloads)
	var interesting map[*axtree.AxParsed]bool
	if !a.Verbose {
		interesting = map[*axtree.AxParsed]bool{}
		for _, r := range roots {
			axtree.CollectInteresting(interesting, r, false)
		}
	}
	var serialized []*axtree.SnapNode
	for _, r := range roots {
		serialized = append(serialized, axtree.SerializeTree(r, interesting)...)
	}
	for _, r := range serialized {
		assignTree(r, loaderID, alloc)
	}

	lines := []string(nil)
	budget := clampMaxNodes(a.MaxNodes)
	for _, r := range serialized {
		renderSnap(r, 0, nil, "all", a.Verbose, alloc, &lines, &budget)
		if budget <= 0 {
			break
		}
	}
	nodeCount := 0
	for _, l := range lines {
		if strings.Contains(l, "uid=") {
			nodeCount++
		}
	}
	if budget <= 0 {
		lines = append(lines, fmt.Sprintf("# 已截断：快照共 %d 节点，仅显示前 %d 个：目标元素可能不可见，"+
			"建议用搜索框过滤、region 参数缩小范围或展开折叠分组后重取",
			countSnapNodes(serialized), nodeCount))
	}
	alloc.cleanup() // 官方：清理本轮未出现的 uid 复用条目
	if sink := c.currentSink(); sink != nil {
		sink.ReplaceRefs(refs)
	}

	// --- 分析层：summary / state / interactiveElements ---
	region := normalizeRegion(a.Region)
	summary, state, interactive := c.analyze(ev, items, region)

	var notes []string
	if degraded != "" {
		notes = append(notes, degraded)
	}
	if region != "all" {
		notes = append(notes, "region 过滤作用于 interactiveElements；tree 为全量辅助输出")
	}
	out := map[string]any{
		// summary 放最前：AI 第一眼读页面人话描述，而不是原始节点流
		"summary":   summary,
		"tree":      strings.Join(lines, "\n"),
		"nodeCount": nodeCount,
		"region":    region,
		"uidNote":   uidLifecycleNote,
	}
	if state != nil {
		out["state"] = state
	}
	if interactive != nil {
		out["interactiveElements"] = interactive
	}
	if len(notes) > 0 {
		out["note"] = strings.Join(notes, "；")
	}
	return out, nil
}

// partialAXTree 降级路径：DOM.getDocument 取根节点 → getPartialAXTree(fetchRelatives)。
func (c *Collector) partialAXTree(ctx context.Context, wsURL string) ([]axtree.AxNode, string, error) {
	res, err := c.client.Call(ctx, wsURL, "DOM.getDocument", map[string]any{}, 10*time.Second)
	if err != nil {
		return nil, "", fmt.Errorf("获取可访问性树失败（full/partial 均不可用）: %w", err)
	}
	var doc struct {
		Root struct {
			NodeID string `json:"nodeId"`
		} `json:"root"`
	}
	if err := cdp.JSONUnmarshal(res, &doc); err != nil {
		return nil, "", err
	}
	res, err = c.client.Call(ctx, wsURL, "Accessibility.getPartialAXTree", map[string]any{
		"nodeId": doc.Root.NodeID, "fetchRelatives": true,
	}, 15*time.Second)
	if err != nil {
		return nil, "", fmt.Errorf("获取可访问性树失败（full/partial 均不可用）: %w", err)
	}
	var out struct {
		Nodes []axtree.AxNode `json:"nodes"`
	}
	if err := cdp.JSONUnmarshal(res, &out); err != nil {
		return nil, "", err
	}
	return out.Nodes, "getFullAXTree 不可用，已降级 getPartialAXTree（可能不含跨 frame 节点）", nil
}

// --- uid 分配器：官方 TextSnapshot.create 的 uid 分配与 uid→backendNodeId 映射 ---
// （原 uid_alloc.go，与快照编排强耦合，并入本文件）

// uidAllocator 官方 TextSnapshot.create 的 uid 分配器：
// 复用表（官方 uniqueBackendNodeIdToMcpId）+ 快照内自增计数。
type uidAllocator struct {
	snapshotID int
	counter    int
	reuse      map[string]string // uniqueBackendId → uid（跨快照复用，挂 Server）
	seen       map[string]bool   // 本轮快照出现过的 key（官方 seenUniqueIds）
	refs       map[string]int64  // uid → backendNodeID（0 = 无 DOM id，不可点击）
}

// newUIDAllocator 从 Collector 取复用表并推进快照号（官方 nextSnapshotId）。
func newUIDAllocator(c *Collector) *uidAllocator {
	if c.uidReuse == nil {
		c.uidReuse = map[string]string{}
	}
	a := &uidAllocator{
		snapshotID: c.nextSnapID,
		reuse:      c.uidReuse,
		seen:       map[string]bool{},
		refs:       map[string]int64{},
	}
	c.nextSnapID++
	return a
}

// assign 为节点分配 uid：命中复用表沿用旧 uid（同一 DOM 节点跨快照稳定），
// 否则新分配 `${snapshotId}_${counter}`（官方 TextSnapshot.ts:73-110）。
// key 约定：AX 节点 = `${loaderId}_${backendNodeId}`；DOM 补录 =
// `custom_${backendNodeId}`；WebView2 零 backendNodeId 的 AX 节点用
// `${loaderId}_0#${axNodeID}`（AX nodeId 跨快照不稳定 → 每次新 uid；
// 若沿用官方的共享 key 会把所有零 id 节点复用成同一个 uid，故此处分叉）。
func (a *uidAllocator) assign(key string, backendID int64) string {
	a.seen[key] = true
	if uid, ok := a.reuse[key]; ok {
		return uid
	}
	uid := fmt.Sprintf("%d_%d", a.snapshotID, a.counter)
	a.counter++
	a.reuse[key] = uid
	return uid
}

// cleanup 删除本轮快照未出现的复用条目（官方 TextSnapshot.ts:141-146）。
func (a *uidAllocator) cleanup() {
	for k := range a.reuse {
		if !a.seen[k] {
			delete(a.reuse, k)
		}
	}
}
