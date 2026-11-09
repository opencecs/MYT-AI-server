package rpa

import (
	"context"
	"fmt"
	"time"
)

// VideoFrame 视频帧数据
type VideoFrame struct {
	Rotation int    // 旋转角度 0/90/180/270
	Data     []byte // H.264 视频帧数据
}

// AudioData 音频数据
type AudioData struct {
	Data []byte // 音频原始数据
}

// VideoFrameFunc 视频帧回调函数类型
type VideoFrameFunc func(frame VideoFrame)

// AudioDataFunc 音频数据回调函数类型
type AudioDataFunc func(data AudioData)

// StartVideoStream 启动视频流
// w: 视频宽度, h: 视频高度, bitrate: 比特率
// videoCB: 视频帧回调（每个H.264帧触发）, audioCB: 音频数据回调（可选，传nil忽略音频）
//
// 注意：魔云腾设备的视频编码器始终以横屏方式输出H.264帧数据。
// 当请求竖屏参数(w<h)时，编码器输出的像素数据实际是横向排列的，
// SPS中的宽高也被标注为竖屏，但内容相对于真实屏幕方向旋转了90°。
// 为修正此问题，SDK会自动将竖屏请求的宽高互换为横屏参数发送给设备，
// 并在回调的VideoFrame.Rotation中标记正确的旋转角度（90°），
// 上层应用在封装MP4时应根据Rotation值设置track的display matrix。
func (c *Client) StartVideoStream(ctx context.Context, w, h, bitrate int, videoCB VideoFrameFunc, audioCB AudioDataFunc) error {
	if videoCB == nil {
		return fmt.Errorf("video callback is required")
	}

	// 防止重复启动
	c.streamMu.Lock()
	if c.streaming {
		c.streamMu.Unlock()
		return fmt.Errorf("video stream is already running")
	}
	c.videoCB = videoCB
	c.audioCB = audioCB
	c.streaming = true

	// 检测设备屏幕方向，确定是否需要旋转修正
	// 编码器始终横屏输出，如果请求的是竖屏(w<h)，需要交换宽高参数
	displayRotation := 0
	reqW, reqH := w, h
	if w < h {
		// 竖屏请求：交换为横屏参数，让编码器自然输出
		reqW, reqH = h, w
		displayRotation = 90
	}
	c.displayRotation = displayRotation

	c.streamMu.Unlock()

	// 构建请求参数: WrapInt(w) + WrapInt(h) + WrapInt(bitrate)
	pkg := NewPkgData(nil)
	pkg.WrapInt(reqW)
	pkg.WrapInt(reqH)
	pkg.WrapInt(bitrate)

	data, err := pkg.Bytes()
	if err != nil {
		c.clearStreamState()
		return err
	}

	// 发送CMD_STARTVIDEO请求
	resp, err := c.call(CMDStartVideo, data, 10*time.Second)
	if err != nil {
		c.clearStreamState()
		return fmt.Errorf("start video stream failed: %w", err)
	}

	// 解析返回值
	// call()返回的是UnwrapData后的裸数据，没有Magic头
	// C++中: PkgData pkg(pRet->data,pRet->len); pkg.UnWrapInt(ret);
	// C++ PkgData构造时会加Magic，但这里resp是裸数据
	if len(resp) >= 8 {
		// 裸数据格式: len(4) + int(4)
		// 读取长度
		dataLen := readInt(resp)
		if dataLen == 4 && len(resp) >= 8 {
			ret := readInt(resp[4:])
			if ret != 0 {
				c.clearStreamState()
				return fmt.Errorf("start video stream returned error code: %d", ret)
			}
		}
	}

	return nil
}

// StopVideoStream 停止视频流
func (c *Client) StopVideoStream(ctx context.Context) error {
	// 无论stop是否成功，都清除流状态
	defer c.clearStreamState()

	_, err := c.call(CMDStopVideo, nil, 10*time.Second)
	if err != nil {
		return fmt.Errorf("stop video stream failed: %w", err)
	}

	return nil
}

// clearStreamState 清除流状态和回调
func (c *Client) clearStreamState() {
	c.streamMu.Lock()
	c.videoCB = nil
	c.audioCB = nil
	c.streaming = false
	c.displayRotation = 0
	c.streamMu.Unlock()
}
