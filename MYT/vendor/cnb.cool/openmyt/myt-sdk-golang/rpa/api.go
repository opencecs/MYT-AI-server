package rpa

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"
)

// RPA 自动化操作接口
type RPA interface {
	// CheckLive 检查连接是否存活
	CheckLive(ctx context.Context) error

	// TakeCapture 截图（返回RGBA数据，全屏）
	TakeCapture(ctx context.Context) (*CaptureResult, error)

	// TakeCaptureRegion 区域截图（返回RGBA数据，指定区域）
	TakeCaptureRegion(ctx context.Context, rect Rect) (*CaptureResult, error)

	// TakeCaptureCompress 截图（压缩格式，全屏）
	TakeCaptureCompress(ctx context.Context, captureType int, quality int) ([]byte, error)

	// TakeCaptureCompressRegion 区域截图（压缩格式，指定区域）
	TakeCaptureCompressRegion(ctx context.Context, rect Rect, captureType int, quality int) ([]byte, error)

	// TouchDown 按下
	TouchDown(ctx context.Context, id, x, y int) error

	// TouchUp 抬起
	TouchUp(ctx context.Context, id, x, y int) error

	// TouchMove 移动
	TouchMove(ctx context.Context, id, x, y int) error

	// TouchClick 点击
	TouchClick(ctx context.Context, id, x, y int) error

	// Swipe 滑动
	Swipe(ctx context.Context, id, x0, y0, x1, y1 int, millis int64, async bool) error

	// KeyPress 按键
	KeyPress(ctx context.Context, code int) error

	// SendText 输入文本
	SendText(ctx context.Context, text string) error

	// OpenApp 打开应用
	OpenApp(ctx context.Context, pkg string) error

	// StopApp 停止应用
	StopApp(ctx context.Context, pkg string) error

	// GetDisplayRotate 获取屏幕旋转角度
	GetDisplayRotate(ctx context.Context) (int, error)

	// ExecCmd 执行shell命令
	ExecCmd(ctx context.Context, cmdline string, waitForExit bool) (string, error)

	// DumpNodeXml 获取界面节点XML
	DumpNodeXml(ctx context.Context, dumpAll bool) (string, error)

	// UseNewNodeMode 使用新节点模式
	UseNewNodeMode(ctx context.Context, use bool) error

	// DumpNodeXmlEx 获取界面节点XML（带超时和新模式参数）
	DumpNodeXmlEx(ctx context.Context, useNewMode bool, timeout int) (string, error)

	// StartVideoStream 启动视频流（H.264帧通过回调接收）
	StartVideoStream(ctx context.Context, w, h, bitrate int, videoCB VideoFrameFunc, audioCB AudioDataFunc) error

	// StopVideoStream 停止视频流
	StopVideoStream(ctx context.Context) error

	// Close 关闭连接
	Close() error
}

// Rect 截图区域矩形
type Rect struct {
	Left   int // 左边界 x
	Top    int // 上边界 y
	Right  int // 右边界 x
	Bottom int // 下边界 y
}

// CaptureResult 截图结果
type CaptureResult struct {
	Width  int
	Height int
	Stride int
	Data   []byte // RGBA格式
}

// Node 界面节点
type Node struct {
	Text          string `json:"text"`
	Desc          string `json:"desc"`
	Package       string `json:"package"`
	Class         string `json:"class"`
	ID            string `json:"id"`
	Index         int    `json:"index"`
	Depth         int    `json:"depth"`
	DrawingOrder  int    `json:"drawingOrder"`
	Rotation      int    `json:"rotation"` // 屏幕旋转角度（0/90/180/270），来自hierarchy标签
	Bounds        [4]int `json:"bounds"`   // left, top, right, bottom
	Center        [2]int `json:"center"`   // x, y
	Checkable     bool   `json:"checkable"`
	Checked       bool   `json:"checked"`
	Clickable     bool   `json:"clickable"`
	Focusable     bool   `json:"focusable"`
	Focused       bool   `json:"focused"`
	Scrollable    bool   `json:"scrollable"`
	LongClickable bool   `json:"longclickable"`
	Password      bool   `json:"password"`
	Selected      bool   `json:"selected"`
	Visible       bool   `json:"visible"`
	Enabled       bool   `json:"enabled"`

	// 树导航（不序列化到JSON）
	parent   *Node
	children []*Node `json:"-"`

	// 客户端引用（用于节点操作，不序列化到JSON）
	client *Client `json:"-"`
}

// Parent 返回父节点，根节点返回nil
func (n *Node) Parent() *Node {
	return n.parent
}

// ChildCount 返回子节点数量
func (n *Node) ChildCount() int {
	return len(n.children)
}

// Child 返回指定索引的子节点，越界返回nil
func (n *Node) Child(index int) *Node {
	if index < 0 || index >= len(n.children) {
		return nil
	}
	return n.children[index]
}

// JSON 返回节点的JSON字符串（与C++ toJson()对齐，但修复了C++的bug：
// C++的"visible"字段被序列化了两次，且缺少depth/drawingOrder字段）
func (n *Node) JSON() (string, error) {
	data, err := json.Marshal(n)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// IsInRect 判断节点bounds是否完全在指定矩形区域内（与C++ NodeBounds::IsInRect一致）
func (n *Node) IsInRect(l, t, r, b int) bool {
	return n.Bounds[0] >= l && n.Bounds[1] >= t &&
		n.Bounds[2] <= r && n.Bounds[3] <= b
}

// BoundsEqual 判断节点bounds是否与指定矩形完全相等
func (n *Node) BoundsEqual(l, t, r, b int) bool {
	return n.Bounds == [4]int{l, t, r, b}
}

// CenterX 返回节点中心X坐标（与C++ getNodeNoundCenter对齐）
func (n *Node) CenterX() int {
	return n.Center[0]
}

// CenterY 返回节点中心Y坐标（与C++ getNodeNoundCenter对齐）
func (n *Node) CenterY() int {
	return n.Center[1]
}

// Click 点击节点中心坐标（与C++ clickNode对齐，使用pointer ID=8）
func (n *Node) Click(ctx context.Context) error {
	if n.client == nil {
		return fmt.Errorf("node has no client reference")
	}
	return n.client.TouchClick(ctx, nodePointerID, n.Center[0], n.Center[1])
}

// LongClick 长按节点中心坐标（与C++ longClickNode对齐，按下3秒后抬起）
func (n *Node) LongClick(ctx context.Context) error {
	if n.client == nil {
		return fmt.Errorf("node has no client reference")
	}
	if err := n.client.TouchDown(ctx, nodePointerID, n.Center[0], n.Center[1]); err != nil {
		return err
	}
	select {
	case <-time.After(3 * time.Second):
	case <-ctx.Done():
		n.client.TouchUp(ctx, nodePointerID, n.Center[0], n.Center[1])
		return ctx.Err()
	}
	return n.client.TouchUp(ctx, nodePointerID, n.Center[0], n.Center[1])
}

// Selector 节点选择器
type Selector struct {
	client  *Client
	finders []nodeFinder
}

// NodeSet 节点集合
type NodeSet struct {
	client *Client
	nodes  []*Node
}

// New 创建RPA客户端
// ip: 设备IP
// port: 端口（默认为9083）
func New(ip string, port int) (RPA, error) {
	if port == 0 {
		port = DefaultPort
	}
	return NewClient(ip, port)
}

// 确保Client实现了RPA接口
var _ RPA = (*Client)(nil)

// CheckLive 检查连接是否存活
func (c *Client) CheckLive(ctx context.Context) error {
	_, err := c.call(CMDCheckLive, nil, 5*time.Second)
	return err
}

// TakeCapture 截图（返回RGBA数据，全屏）
func (c *Client) TakeCapture(ctx context.Context) (*CaptureResult, error) {
	return c.TakeCaptureRegion(ctx, Rect{})
}

// TakeCaptureRegion 区域截图（返回RGBA数据，指定区域）
// rect: 截图区域，全零值表示全屏
func (c *Client) TakeCaptureRegion(ctx context.Context, rect Rect) (*CaptureResult, error) {
	pkg := NewPkgData(nil)
	pkg.WrapInt(rect.Left)
	pkg.WrapInt(rect.Top)
	pkg.WrapInt(rect.Right)
	pkg.WrapInt(rect.Bottom)

	data, err := pkg.Bytes()
	if err != nil {
		return nil, err
	}

	resp, err := c.call(CMDTakeCapture, data, 30*time.Second)
	if err != nil {
		return nil, err
	}

	// 解析响应: width, height, stride, data
	pkgResp := NewPkgData(resp)
	w, err := pkgResp.UnwrapInt()
	if err != nil {
		return nil, err
	}
	h, err := pkgResp.UnwrapInt()
	if err != nil {
		return nil, err
	}
	stride, err := pkgResp.UnwrapInt()
	if err != nil {
		return nil, err
	}
	imgData, err := pkgResp.UnwrapData()
	if err != nil {
		return nil, err
	}

	return &CaptureResult{
		Width:  w,
		Height: h,
		Stride: stride,
		Data:   imgData,
	}, nil
}

// TakeCaptureCompress 截图（压缩格式，全屏）
func (c *Client) TakeCaptureCompress(ctx context.Context, captureType int, quality int) ([]byte, error) {
	return c.TakeCaptureCompressRegion(ctx, Rect{}, captureType, quality)
}

// TakeCaptureCompressRegion 区域截图（压缩格式，指定区域）
// rect: 截图区域，全零值表示全屏
// captureType: 0=png, 1=jpg
// quality: 0-100
func (c *Client) TakeCaptureCompressRegion(ctx context.Context, rect Rect, captureType int, quality int) ([]byte, error) {
	pkg := NewPkgData(nil)
	pkg.WrapInt(captureType)
	pkg.WrapInt(quality)
	pkg.WrapInt(rect.Left)
	pkg.WrapInt(rect.Top)
	pkg.WrapInt(rect.Right)
	pkg.WrapInt(rect.Bottom)

	data, err := pkg.Bytes()
	if err != nil {
		return nil, err
	}

	return c.call(CMDTakeCaptureCompress, data, 30*time.Second)
}

// TouchDown 按下
func (c *Client) TouchDown(ctx context.Context, id, x, y int) error {
	return c.touchAction(TouchDown, id, x, y)
}

// TouchUp 抬起
func (c *Client) TouchUp(ctx context.Context, id, x, y int) error {
	return c.touchAction(TouchUp, id, x, y)
}

// TouchMove 移动
func (c *Client) TouchMove(ctx context.Context, id, x, y int) error {
	return c.touchAction(TouchMove, id, x, y)
}

// TouchClick 点击
func (c *Client) TouchClick(ctx context.Context, id, x, y int) error {
	return c.touchAction(TouchTap, id, x, y)
}

// touchAction 触摸操作
func (c *Client) touchAction(ev, id, x, y int) error {
	pkg := NewPkgData(nil)
	pkg.WrapInt(id)
	pkg.WrapInt(ev)
	pkg.WrapInt(x)
	pkg.WrapInt(y)

	data, err := pkg.Bytes()
	if err != nil {
		return err
	}

	_, err = c.call(CMDTouchAction, data, 5*time.Second)
	return err
}

// Swipe 滑动
func (c *Client) Swipe(ctx context.Context, id, x0, y0, x1, y1 int, millis int64, async bool) error {
	// 使用touchMove模拟滑动
	steps := int(millis / 16) // 约60fps
	if steps < 1 {
		steps = 1
	}

	dx := float64(x1-x0) / float64(steps)
	dy := float64(y1-y0) / float64(steps)

	// 按下
	if err := c.TouchDown(ctx, id, x0, y0); err != nil {
		return err
	}

	// 移动
	for i := 1; i <= steps; i++ {
		x := int(float64(x0) + dx*float64(i))
		y := int(float64(y0) + dy*float64(i))
		if err := c.TouchMove(ctx, id, x, y); err != nil {
			return err
		}
		time.Sleep(16 * time.Millisecond)
	}

	// 抬起
	return c.TouchUp(ctx, id, x1, y1)
}

// KeyPress 按键
func (c *Client) KeyPress(ctx context.Context, code int) error {
	pkg := NewPkgData(nil)
	pkg.WrapInt(code)

	data, err := pkg.Bytes()
	if err != nil {
		return err
	}

	_, err = c.call(CMDKeyEvent, data, 5*time.Second)
	return err
}

// SendText 输入文本
func (c *Client) SendText(ctx context.Context, text string) error {
	pkg := NewPkgData(nil)
	pkg.WrapString(text)

	data, err := pkg.Bytes()
	if err != nil {
		return err
	}

	_, err = c.call(CMDInputText, data, 5*time.Second)
	return err
}

// OpenApp 打开应用
func (c *Client) OpenApp(ctx context.Context, pkg string) error {
	return c.runStopApp(pkg, true)
}

// StopApp 停止应用
func (c *Client) StopApp(ctx context.Context, pkg string) error {
	return c.runStopApp(pkg, false)
}

// runStopApp 运行或停止应用
func (c *Client) runStopApp(pkg string, isRun bool) error {
	pkgData := NewPkgData(nil)
	if isRun {
		pkgData.WrapInt(1)
	} else {
		pkgData.WrapInt(0)
	}
	pkgData.WrapString(pkg)

	data, err := pkgData.Bytes()
	if err != nil {
		return err
	}

	_, err = c.call(CMDRunStopApp, data, 10*time.Second)
	return err
}

// GetDisplayRotate 获取屏幕旋转角度
func (c *Client) GetDisplayRotate(ctx context.Context) (int, error) {
	data, err := c.call(CMDGetRotate, nil, 5*time.Second)
	if err != nil {
		return 0, err
	}

	// 返回的是原始4字节整数
	if len(data) < 4 {
		return 0, fmt.Errorf("invalid rotate data length: %d", len(data))
	}
	return int(binary.LittleEndian.Uint32(data)), nil
}

// ExecCmd 执行shell命令
func (c *Client) ExecCmd(ctx context.Context, cmdline string, waitForExit bool) (string, error) {
	pkg := NewPkgData(nil)
	if waitForExit {
		pkg.WrapInt(1)
	} else {
		pkg.WrapInt(0)
	}
	pkg.WrapString(cmdline)

	data, err := pkg.Bytes()
	if err != nil {
		return "", err
	}

	resp, err := c.call(CMDExec, data, 30*time.Second)
	if err != nil {
		return "", err
	}

	return string(resp), nil
}

// DumpNodeXml 获取界面节点XML
func (c *Client) DumpNodeXml(ctx context.Context, dumpAll bool) (string, error) {
	var flag int
	if dumpAll {
		flag = 1
	}

	pkg := NewPkgData(nil)
	pkg.WrapInt(flag)

	data, err := pkg.Bytes()
	if err != nil {
		return "", err
	}

	resp, err := c.call(CMDDumpNode, data, 10*time.Second)
	if err != nil {
		return "", err
	}

	return string(resp), nil
}

// UseNewNodeMode 使用新节点模式
func (c *Client) UseNewNodeMode(ctx context.Context, use bool) error {
	var flag int
	if use {
		flag = 1
	}

	pkg := NewPkgData(nil)
	pkg.WrapInt(flag)

	data, err := pkg.Bytes()
	if err != nil {
		return err
	}

	_, err = c.call(CMDUseNewNode, data, 5*time.Second)
	return err
}

// Tap 点击（简化方法）
func (c *Client) Tap(x, y int) error {
	return c.TouchClick(context.Background(), 0, x, y)
}

// Screenshot 截图（简化方法，全屏PNG）
func (c *Client) Screenshot() ([]byte, error) {
	return c.TakeCaptureCompress(context.Background(), 0, 100)
}

// ScreenshotRegion 区域截图（简化方法，PNG格式）
func (c *Client) ScreenshotRegion(rect Rect) ([]byte, error) {
	return c.TakeCaptureCompressRegion(context.Background(), rect, 0, 100)
}

// Shell 执行shell命令（简化方法）
func (c *Client) Shell(cmd string) (string, error) {
	return c.ExecCmd(context.Background(), cmd, true)
}

// Input 输入文本（简化方法）
func (c *Client) Input(text string) error {
	return c.SendText(context.Background(), text)
}

// StartApp 启动应用（简化方法）
func (c *Client) StartApp(pkg string) error {
	return c.OpenApp(context.Background(), pkg)
}

// KillApp 停止应用（简化方法）
func (c *Client) KillApp(pkg string) error {
	return c.StopApp(context.Background(), pkg)
}
