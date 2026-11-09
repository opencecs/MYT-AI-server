package rpa

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// 默认端口
	DefaultPort = 9083

	// 协议Magic
	Magic = 0x55667788

	// 命令ID
	CMDCheckLive           = 100
	CMDTakeCapture         = 102
	CMDTakeCaptureCompress = 103
	CMDTouchAction         = 104
	CMDKeyEvent            = 105
	CMDInputText           = 106
	CMDRunStopApp          = 107
	CMDDumpNode            = 108
	CMDGetRotate           = 109
	CMDExec                = 110
	CMDStartVideo          = 111
	CMDStopVideo           = 112
	CMDAudioStream         = 113
	CMDUseNewNode          = 114

	// 触摸事件类型
	TouchDown = 1
	TouchUp   = 2
	TouchMove = 3
	TouchTap  = 4
)

// Client RPA客户端
type Client struct {
	ip      string
	port    int
	conn    net.Conn
	session int32
	mu      sync.Mutex
	closed  bool

	// 响应处理
	respMap map[int32]*response
	respMu  sync.Mutex

	// 视频流/音频流回调
	videoCB         VideoFrameFunc
	audioCB         AudioDataFunc
	streaming       bool
	streamMu        sync.RWMutex
	displayRotation int // 设备显示旋转角度（0或90），用于修正编码器输出方向
}

type response struct {
	data []byte
	err  error
	done chan struct{}
}

// NewClient 创建RPA客户端
func NewClient(ip string, port int) (*Client, error) {
	if port == 0 {
		port = DefaultPort
	}

	c := &Client{
		ip:      ip,
		port:    port,
		session: 0,
		respMap: make(map[int32]*response),
	}

	if err := c.connect(); err != nil {
		return nil, err
	}

	return c, nil
}

// connect 建立TCP连接
func (c *Client) connect() error {
	addr := fmt.Sprintf("%s:%d", c.ip, c.port)
	logDebug("Connecting to %s", addr)
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		logDebug("Connect failed: %v", err)
		return fmt.Errorf("connect failed: %w", err)
	}

	c.conn = conn
	c.closed = false
	logDebug("Connected successfully")

	// 启动响应读取协程
	go c.readLoop()

	return nil
}

// Close 关闭连接
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// IsClosed 检查连接是否已关闭
func (c *Client) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// readLoop 读取响应循环
func (c *Client) readLoop() {
	for {
		if c.IsClosed() {
			return
		}

		// 读取4字节长度
		lenBuf := make([]byte, 4)
		if _, err := c.readFull(lenBuf); err != nil {
			logDebug("readLoop: read length failed: %v", err)
			if !c.IsClosed() {
				c.Close()
			}
			return
		}

		dataLen := binary.BigEndian.Uint32(lenBuf)
		logDebug("readLoop: data length = %d", dataLen)
		if dataLen == 0 {
			continue
		}

		// 读取数据
		data := make([]byte, dataLen)
		if _, err := c.readFull(data); err != nil {
			logDebug("readLoop: read data failed: %v", err)
			if !c.IsClosed() {
				c.Close()
			}
			return
		}

		logHex(data, "readLoop: received data")

		// 解析响应
		c.handleResponse(data)
	}
}

// readFull 完整读取指定长度数据
func (c *Client) readFull(buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := c.conn.Read(buf[total:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// handleResponse 处理响应数据
// 响应格式: Magic + WrapInt(type) + WrapInt(needRet) + WrapInt(session) + WrapInt(method) + WrapData(data)
// 即: Magic(4) + len(4)+type(4) + len(4)+needRet(4) + len(4)+session(4) + len(4)+method(4) + len(4)+data
// type=1: 服务器主动推送（SysCall），需要路由到回调
// type=2: 请求响应（SysCallRet），匹配session返回给请求者
func (c *Client) handleResponse(data []byte) {
	logDebug("handleResponse: data len=%d", len(data))

	// 使用PkgData解析
	pkg := NewPkgData(data)

	// 跳过Magic
	pkg.offset = 4

	// 解析字段
	field1, err := pkg.UnwrapInt()
	if err != nil {
		logDebug("handleResponse: unwrap field1 failed: %v", err)
		return
	}
	logDebug("handleResponse: field1=%d", field1)

	field2, err := pkg.UnwrapInt()
	if err != nil {
		logDebug("handleResponse: unwrap field2 failed: %v", err)
		return
	}
	logDebug("handleResponse: field2=%d", field2)

	session, err := pkg.UnwrapInt()
	if err != nil {
		logDebug("handleResponse: unwrap session failed: %v", err)
		return
	}
	logDebug("handleResponse: session=%d", session)

	method, err := pkg.UnwrapInt()
	if err != nil {
		logDebug("handleResponse: unwrap method failed: %v", err)
		return
	}
	logDebug("handleResponse: method=%d", method)

	respData, err := pkg.UnwrapData()
	if err != nil {
		logDebug("handleResponse: unwrap data failed: %v", err)
		return
	}
	logDebug("handleResponse: data len=%d", len(respData))

	// type=1: 服务器主动推送，路由到回调
	if field1 == 1 {
		c.handleSysCall(int(method), respData)
		// 如果需要返回，发送确认
		if field2 == 1 {
			c.sendSysCallRet(int(session), int(method), nil)
		}
		return
	}

	// type=2: 请求响应，匹配session
	c.respMu.Lock()
	resp, ok := c.respMap[int32(session)]
	c.respMu.Unlock()

	if ok {
		logDebug("handleResponse: found session %d, notifying", session)
		resp.data = respData
		resp.err = nil
		close(resp.done)
	} else {
		logDebug("handleResponse: session %d not found in respMap", session)
	}
}

// handleSysCall 处理服务器主动推送
func (c *Client) handleSysCall(method int, data []byte) {
	switch method {
	case CMDStartVideo:
		c.streamMu.RLock()
		cb := c.videoCB
		rot := c.displayRotation
		c.streamMu.RUnlock()
		if cb != nil && len(data) > 0 {
			var rotation int
			frameData := data
			// C++代码: ptr[0]为rotation, ptr+8为h264数据
			// 协议格式: [rotation(1)][padding(7)][h264_data]
			// 始终遵循C++ SDK协议，前8字节为header，之后为H.264帧数据
			if len(data) > 8 {
				rotation = int(data[0])
				frameData = data[8:]
			}
			// 修正rotation：设备编码器对竖屏请求总是返回rotation=0，
			// 但实际输出内容需要旋转。当设备返回0时使用displayRotation覆盖。
			if rotation == 0 && rot != 0 {
				rotation = rot
			}
			// 拷贝帧数据，避免引用readLoop的buffer导致GC压力
			frameCopy := append([]byte{}, frameData...)
			cb(VideoFrame{
				Rotation: rotation,
				Data:     frameCopy,
			})
		}
	case CMDAudioStream:
		c.streamMu.RLock()
		cb := c.audioCB
		c.streamMu.RUnlock()
		if cb != nil && len(data) > 0 {
			// 拷贝音频数据，避免引用readLoop的buffer
			dataCopy := append([]byte{}, data...)
			cb(AudioData{Data: dataCopy})
		}
	default:
		logDebug("handleSysCall: unknown method=%d", method)
	}
}

// sendSysCallRet 发送SysCall确认回包
func (c *Client) sendSysCallRet(session, method int, data []byte) {
	pkg := NewPkgData(nil)
	pkg.WrapInt(2) // type=2 响应
	pkg.WrapInt(0) // needRet=0 确认包不需要再响应
	pkg.WrapInt(session)
	pkg.WrapInt(method)
	pkg.WrapData(data)

	c.mu.Lock()
	err := c.sendPkg(pkg)
	c.mu.Unlock()

	if err != nil {
		logDebug("sendSysCallRet: send failed: %v", err)
	}
}

// call 发送请求并等待响应
func (c *Client) call(method int, data []byte, timeout time.Duration) ([]byte, error) {
	if c.IsClosed() {
		return nil, fmt.Errorf("connection closed")
	}

	// 生成session ID
	session := atomic.AddInt32(&c.session, 1)

	// 创建响应通道
	resp := &response{
		done: make(chan struct{}),
	}

	c.respMu.Lock()
	c.respMap[session] = resp
	c.respMu.Unlock()

	defer func() {
		c.respMu.Lock()
		delete(c.respMap, session)
		c.respMu.Unlock()
	}()

	// 构建请求包
	// 格式: int(1) + int(needRet) + int(session) + int(method) + data
	pkg := NewPkgData(nil)
	pkg.WrapInt(1) // 固定值1
	pkg.WrapInt(1) // needRet=1 需要返回
	pkg.WrapInt(int(session))
	pkg.WrapInt(method)
	pkg.WrapData(data)

	pkgData, _ := pkg.Bytes()
	logHex(pkgData, fmt.Sprintf("call: sending method=%d session=%d dataLen=%d", method, session, len(data)))

	// 发送请求
	c.mu.Lock()
	err := c.sendPkg(pkg)
	c.mu.Unlock()

	if err != nil {
		logDebug("call: send failed: %v", err)
		return nil, err
	}

	// 等待响应
	select {
	case <-resp.done:
		return resp.data, resp.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout")
	}
}

// sendPkg 发送数据包
func (c *Client) sendPkg(pkg *PkgData) error {
	data, err := pkg.Bytes()
	if err != nil {
		return err
	}

	// 发送4字节长度 + 数据
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))

	if _, err := c.conn.Write(lenBuf); err != nil {
		return err
	}
	if _, err := c.conn.Write(data); err != nil {
		return err
	}

	return nil
}
