// Package v2host 对接 V2 模式的数据源：云机主机（宿主机）裸露的 Docker Engine API（2375）。
// 链路参考 V2 Web 端（view/device/v2.html + public/admin/js/docker.sdk.js）：
//   - 主机探测   GET http://{ip}:2375/info
//   - 容器列表   GET http://{ip}:2375/containers/json
package v2host

// HostInfo Docker /info 返回的主机信息子集
type HostInfo struct {
	ID            string `json:"ID"`
	Name          string `json:"Name"`
	ServerVersion string `json:"ServerVersion"`
	Containers    int    `json:"Containers"`
}

// PortJSON Docker /containers/json 返回的端口映射条目
type PortJSON struct {
	IP          string `json:"IP"`
	PrivatePort int    `json:"PrivatePort"`
	PublicPort  int    `json:"PublicPort"`
	Type        string `json:"Type"`
}

// ContainerInfo Docker /containers/json 返回的容器条目子集
type ContainerInfo struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`  // running / exited / paused ...
	Status string            `json:"Status"` // 人类可读状态（"Up 2 hours"）
	Labels map[string]string `json:"Labels"`
	Ports  []PortJSON        `json:"Ports"`
}

// ProbeResult 单台主机的探测结果（前端展示用）
type ProbeResult struct {
	IP         string `json:"ip"`
	OK         bool   `json:"ok"`
	HostID     string `json:"hostId,omitempty"`
	HostName   string `json:"hostName,omitempty"`
	Version    string `json:"version,omitempty"`
	Containers int    `json:"containers,omitempty"`
	Err        string `json:"err,omitempty"`
	// SuperSdkReady 超级 SDK（81 myt_sdk 容器）是否就绪。准入放宽后 OK 的主机
	// 不再要求 81 可达：仅 UDP+2375 可达即可添加，81 缺失时 Ready=false（前端
	// 标注「未装超级 SDK」并引导安装）。不用 omitempty——false 被抹掉后前端
	// 拿 undefined 会与「未知」混淆，待添加徽章/添加结果标注静默失效。
	SuperSdkReady bool `json:"superSdkReady"`
	// SdkErr 81 探测失败原因（Ready=false 时给 UI/日志用，如 "81 连接拒绝"）
	SdkErr string `json:"sdkErr,omitempty"`
}

// SystemInfo 主机设备信息（myt_sdk 容器 81 端口 /systeminfo）
type SystemInfo struct {
	CPU          float64 `json:"cpu"`          // CPU 占用百分比
	DevCode      string  `json:"devCode"`      // 设备序列号（与 UDP 发现的 DeviceID 一致）
	DiskTotal    int64   `json:"disk_total"`   // 磁盘总量（字节）
	DiskPercent  float64 `json:"disk_percent"` // 磁盘占用百分比
	MemTotal     int64   `json:"mem_total"`    // 内存总量（字节）
	MemPercent   float64 `json:"mem_percent"`  // 内存占用百分比
	SwapTotal    int64   `json:"swap_total"`   // Swap 总量（字节）
	SwapPercent  float64 `json:"swap_percent"` // Swap 占用百分比
	Temperatures float64 `json:"temperatures"` // 温度（℃）
}
