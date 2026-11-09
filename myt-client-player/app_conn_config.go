package main

// connConfig 连接参数（main.go 从命令行赋值，Connect 读取）。
// 通过匿名嵌入 App，字段自动提升为 a.RTSPURL 等。
type connConfig struct {
	RTSPURL        string
	WebURL         string           // WebView2 加载的 URL（公网投屏模式）
	WebCmdForward  func(cmd string) // WebView2 模式：转发命令给 player 页面
	User           string
	Pass           string
	CPort          int
	UseTCP         bool
	RTSPTCP        bool
	Landscape      bool
	NoAudio        bool
	StallTimeout   int
	Playarg        string
	ApiPort        int
	NoSidebar      bool
	HardwareDecode bool // GPU 硬件加速解码（-hwaccel，默认 true）。false=纯 CPU 软解
	NotifyPort     int // 首帧就绪后向此 UDP 端口发通知（父进程错开启动用，0=禁用）

	// 容器安装上下文（侧边栏上传 APK 安装用，与 MYT 客户端 UploadToCloud 同路径）：
	// ContainerName 目标云机容器名（SDK /android/exec 的 name 参数）；
	// SdkHost 设备 SDK 宿主地址（端口固定 8000）；SdkPassword Basic Auth 密码（可空）。
	// ContainerName 为空 = 主程序未注入（手动调试等场景），播放器禁用安装（仅上传）。
	// V2Mode：V2 部署模式安装改走主机 81 and_api/v1/shell（SdkHost 为主机 IP）。
	ContainerName string
	SdkHost       string
	SdkPassword   string
	V2Mode        bool

	// tebox 投屏模式（-cast-mode=tebox，QEMU Android 模拟器）：
	// CastHost tebox-api 主机；CastID 实例 id（握手行与 UDP 事件 id）；
	// CastPort 视频 TCP 端口（默认 9901）；CtrlPort 控制 UDP 端口（默认 9905）。
	// CastMode 为空 = RTSP 原有路径。
	CastMode string
	CastHost string
	CastID   string
	CastPort int
	CtrlPort int
	// AudioPort 独立音频 TCP 端口（0=不连独立音频，旧部署靠 cast 流内
	// codec=16；>0 连 TEBOX-AUDIO/1 独立音频端点，上游 0360f5e 拆分后新部署）。
	AudioPort int
}
