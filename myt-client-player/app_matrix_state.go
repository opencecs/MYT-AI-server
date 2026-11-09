package main

// matrixState 公网矩阵群控状态（WebURL 模式专用，独立于本地 RTSP 矩阵）。
// 通过匿名嵌入 App，字段自动提升为 a.MatrixListenPort 等。
type matrixState struct {
	MatrixListenPort int               // UDP 监听端口 (0=禁用)
	MatrixPrimary    bool              // 初始为主控
	MatrixTargets    []int             // 被控 UDP 端口列表
	MatrixMgr        *MatrixManager    // 运行时矩阵管理器（nil=未启用）
	MatrixInject     *MatrixInjectConfig // nil=未启用（主控或非公网矩阵）
	FanoutURLs       []string          // 公网矩阵 v2：fanout WebSocket URL 列表
}
