package agent

// 设备端路径
const (
	remoteBase    = "/home/user"
	hermesHome    = "/home/user/.hermes"
	installDir    = "/home/user/.hermes/hermes-agent"
	venvDir       = "/home/user/.hermes/hermes-agent/venv"
	remotePkgDir  = "/home/user/agent"
	dashboardPort = "9119"
)

// uninstallSteps 卸载步骤
var uninstallSteps = []string{
	"停止服务",
	"删除自启动",
	"卸载 Python 包",
	"删除安装目录",
	"删除配置",
	"清理命令链接",
}

// makeProgress 创建初始进度
func makeProgress(action string, stepNames []string) InstallProgress {
	steps := make([]InstallStep, len(stepNames))
	for i, name := range stepNames {
		steps[i] = InstallStep{Index: i + 1, Name: name, Status: StepPending}
	}
	return InstallProgress{
		Steps:  steps,
		Total:  len(steps),
		Action: action,
	}
}

// updateStep 更新某步骤状态并回调
func updateStep(progress *InstallProgress, index int, status StepStatus, msg string, cb ProgressCallback) {
	if index < 0 || index >= len(progress.Steps) {
		return
	}
	progress.Steps[index].Status = status
	progress.Steps[index].Message = msg
	progress.Current = index + 1
	if cb != nil {
		cb(*progress)
	}
}

// failStep 标记步骤失败+整体失败
func failStep(progress *InstallProgress, index int, msg string, cb ProgressCallback) {
	updateStep(progress, index, StepFailed, msg, cb)
	progress.Done = true
	progress.Success = false
	progress.Error = msg
	progress.Current = progress.Total
	if cb != nil {
		cb(*progress)
	}
}

// finishProgress 标记整体完成
func finishProgress(progress *InstallProgress, cb ProgressCallback) {
	progress.Done = true
	progress.Success = true
	progress.Current = progress.Total
	for i := range progress.Steps {
		if progress.Steps[i].Status == StepPending || progress.Steps[i].Status == StepRunning {
			progress.Steps[i].Status = StepSuccess
		}
	}
	if cb != nil {
		cb(*progress)
	}
}

// failProgress 标记整体失败
func failProgress(progress *InstallProgress, msg string, cb ProgressCallback) {
	progress.Done = true
	progress.Success = false
	progress.Error = msg
	progress.Current = progress.Total
	if cb != nil {
		cb(*progress)
	}
}
