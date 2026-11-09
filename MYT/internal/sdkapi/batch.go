package sdkapi

// BatchOpResult 批量操作结果。
// DeviceID 为设备标识符（local 模式为 IP，public 模式为 device_id）；
// Name 为容器名（AndroidInfo.name，设备 SDK API 需要容器名）。
type BatchOpResult struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
	Success  bool   `json:"success"`
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
}
