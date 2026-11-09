package main

import "time"

// VideoSource 视频源抽象：向解码渲染管线产出 H.264 NALU 帧（不带 start code，
// 与 RTSP depacketize 输出同格式，H264Decoder.Decode 直接消费），并回报连接状态。
//
// 现有 RTSP 路径（RTSPSource 包装 RTSPClient）与 tebox 路径（TeboxSource 包装
// teboxcast）各自适配本接口，App 按启动参数（-cast-mode）选择，
// ffdecode→D3D11 解码渲染管线零改动。
type VideoSource interface {
	// Start 建立连接并开始产出帧。自带重连策略的实现（tebox）在内部消化
	// 瞬时断连，Start 仅在无法启动（如重复启动）时返回错误。
	Start() error
	// Stop 停止产出并释放连接（幂等）。
	Stop()
	// SetOnH264Frame 注册帧回调：nalus 为不带 start code 的 NALU 列表，
	// pts 为该帧展示时间戳。
	SetOnH264Frame(cb func(nalus [][]byte, pts time.Duration))
	// SetOnState 注册连接状态回调：connected=false 表示当前连接断开。
	SetOnState(cb func(connected bool))
}
