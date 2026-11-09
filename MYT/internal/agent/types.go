package agent

// AgentStatus 设备上 Agent 的运行状态
type AgentStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Status    string `json:"status"` // running | stopped | unknown
}

// StepStatus 步骤状态
type StepStatus string

const (
	StepPending StepStatus = "pending"
	StepRunning StepStatus = "running"
	StepSuccess StepStatus = "success"
	StepFailed  StepStatus = "failed"
	StepSkipped StepStatus = "skipped"
)

// InstallStep 单个安装步骤
type InstallStep struct {
	Index   int        `json:"index"`
	Name    string     `json:"name"`
	Status  StepStatus `json:"status"`
	Message string     `json:"message,omitempty"`
}

// InstallProgress 安装/卸载进度
type InstallProgress struct {
	DeviceIP string        `json:"deviceIP"`
	Steps    []InstallStep `json:"steps"`
	Current  int           `json:"current"`
	Total    int           `json:"total"`
	Done     bool          `json:"done"`
	Success  bool          `json:"success"`
	Error    string        `json:"error,omitempty"`
	Action   string        `json:"action"` // uninstall
}

// WithDeviceIP 返回带设备IP的副本
func (p InstallProgress) WithDeviceIP(ip string) InstallProgress {
	p.DeviceIP = ip
	return p
}

// ProgressCallback 进度回调函数类型
type ProgressCallback func(progress InstallProgress)

// InstallResult 卸载结果
type InstallResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}
