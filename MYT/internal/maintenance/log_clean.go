package maintenance

import (
	"fmt"
	"log"
	"strings"

	"github.com/moyunteng/myt/internal/sdkapi"
)

// LogItem 日志项
type LogItem struct {
	Key     string  `json:"key"`
	Label   string  `json:"label"`
	SizeMB  float64 `json:"sizeMB"`
	Risk    string  `json:"risk"`
	RiskTip string  `json:"riskTip,omitempty"`
	Group   string  `json:"group"`
}

// 设备级日志项定义（SSH + sudo 执行，用 du -sk KB精度）
var deviceLogDefs = []struct {
	key, label, duCmd string
}{
	{"docker_container", "Docker 容器日志", "sudo du -sk /var/lib/docker/containers/ 2>/dev/null | awk '{s+=$1} END {print s+0}'"},
	{"docker_log", "Docker 服务日志", "sudo du -sk /var/log/docker.log 2>/dev/null | awk '{s+=$1} END {print s+0}'"},
	{"messages", "系统日志(messages)", "sudo du -sk /var/log/messages /var/log/messages.0 /var/log/syslog /var/log/syslog.* 2>/dev/null | awk '{s+=$1} END {print s+0}'"},
	{"rotated", "轮转归档(*.1/*.gz/*.old)", "sudo du -sk /var/log/*.1 /var/log/*.gz /var/log/*.old /var/log/messages.0 2>/dev/null | awk '{s+=$1} END {print s+0}'"},
	{"other_log", "其他服务日志(*.log)", "sudo du -sk /var/log/acpid.log /var/log/wtmp 2>/dev/null | awk '{s+=$1} END {print s+0}'"},
	{"pkg_cache", "包管理缓存(apk)", "sudo du -sk /var/cache/apk/ 2>/dev/null | awk '{s+=$1} END {print s+0}'"},
	{"tmp", "临时文件(/tmp)", "sudo du -sk /tmp/ 2>/dev/null | awk '{s+=$1} END {print s+0}'"},
}

// GetLogUsage 获取日志占用大小（结构化，带风险标注）
func GetLogUsage(deviceIP, androidName string, client *sdkapi.Client) map[string]interface{} {
	ip := extractPureIP(deviceIP)

	// 设备级：一次 SSH 连接执行所有 du 命令
	cmds := make(map[string]string)
	for _, d := range deviceLogDefs {
		cmds[d.key] = d.duCmd
	}

	outputs, err := SSHBatchExec(ip, cmds)
	if err != nil {
		log.Printf("[维护] 批量获取日志大小失败: %v", err)
		// 降级：逐个执行
		outputs = make(map[string]string)
		for _, d := range deviceLogDefs {
			out, e := SSHExecSimple(ip, d.duCmd)
			if e != nil {
				out = ""
			}
			outputs[d.key] = out
		}
	}

	items := make([]LogItem, 0, len(deviceLogDefs)+4)
	for _, d := range deviceLogDefs {
		sizeMB := parseDuMB(outputs[d.key])
		items = append(items, LogItem{
			Key: d.key, Label: d.label, SizeMB: sizeMB,
			Risk: "safe", Group: "device",
		})
	}

	// 云机级日志项（DockerExec 执行，需要选中云机）
	if androidName != "" && client != nil {
		items = append(items,
			LogItem{Key: "android_anr", Label: "ANR日志", SizeMB: getAndroidLogSize(client, androidName, "/data/anr/"), Risk: "safe", Group: "android"},
			LogItem{Key: "android_tombstones", Label: "崩溃日志", SizeMB: getAndroidLogSize(client, androidName, "/data/tombstones/"), Risk: "safe", Group: "android"},
			LogItem{Key: "android_dontpanic", Label: "Panic日志", SizeMB: getAndroidLogSize(client, androidName, "/data/dontpanic/"), Risk: "safe", Group: "android"},
			LogItem{Key: "android_syslog", Label: "云机系统日志", SizeMB: getAndroidLogSize(client, androidName, "/data/log/", "/data/logs/"), Risk: "safe", Group: "android"},
		)
	}

	// 云机级项始终保留；设备级项始终显示（即使 0MB，让用户知道有哪些类别）
	visible := items

	return map[string]interface{}{
		"success": true,
		"items":   visible,
	}
}

// parseDuMB 解析 du -sk 的输出（KB数值，转为MB）
func parseDuMB(output string) float64 {
	output = strings.TrimSpace(output)
	if output == "" || output == "0" || output == "0B" {
		return 0
	}
	// du -sk | awk sum 输出的是纯数字（KB）
	lines := strings.Split(output, "\n")
	lastLine := ""
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l != "" {
			lastLine = l
			break
		}
	}
	if lastLine == "" {
		return 0
	}
	var kb float64
	fmt.Sscanf(lastLine, "%f", &kb)
	return kb / 1024 // KB → MB
}

// CleanLogs 清理系统日志（按类别）
func CleanLogs(deviceIP, androidName string, categories []string, client *sdkapi.Client) map[string]interface{} {
	return CleanLogsWithProgress(deviceIP, androidName, categories, client, nil)
}

// CleanLogsWithProgress 带进度回调的日志清理：每完成一个 category 调用 progressCb(done, total, stage)。
// progressCb 可为 nil（兼容旧调用，如维护 modal 直接调）。
func CleanLogsWithProgress(deviceIP, androidName string, categories []string, client *sdkapi.Client, progressCb func(done, total int, stage string)) map[string]interface{} {
	ip := extractPureIP(deviceIP)

	// 每个类别对应的清理命令
	cleanCmds := map[string]string{
		"docker_container": "truncate -s 0 /var/lib/docker/containers/*/*-json.log 2>/dev/null",
		"docker_log":       "truncate -s 0 /var/log/docker.log 2>/dev/null; rm -f /var/log/docker.log.* 2>/dev/null",
		"messages":         "truncate -s 0 /var/log/messages /var/log/messages.* /var/log/syslog /var/log/syslog.* 2>/dev/null",
		"rotated":          "rm -f /var/log/*.1 /var/log/*.gz /var/log/*.old /var/log/messages.0 2>/dev/null; find /var/log -name \"*.log.*\" -delete 2>/dev/null",
		"other_log":        "find /var/log -type f -name \"*.log\" ! -name \"docker.log\" ! -name \"myt_sdk.log\" -exec truncate -s 0 {} \\; 2>/dev/null",
		"pkg_cache":        "rm -rf /var/cache/apk/* 2>/dev/null",
		"tmp":              "find /tmp -type f -atime +1 -delete 2>/dev/null",
	}

	var results []string
	done := 0
	total := len(categories)
	for _, cat := range categories {
		// 云机级日志项，通过 DockerExec 清理
		if strings.HasPrefix(cat, "android_") {
			if androidName == "" {
				continue
			}
			var cmd string
			var desc string
			switch cat {
			case "android_anr":
				cmd = "rm -rf /data/anr/*"
				desc = "ANR日志"
			case "android_tombstones":
				cmd = "rm -rf /data/tombstones/*"
				desc = "崩溃日志"
			case "android_dontpanic":
				cmd = "rm -rf /data/dontpanic/*"
				desc = "Panic日志"
			case "android_syslog":
				cmd = "rm -rf /data/log/* /data/logs/*"
				desc = "云机系统日志"
			default:
				continue
			}
			resp, err := client.DockerExec(androidName, cmd)
			if err != nil || resp.ExitCode != 0 {
				log.Printf("[维护] 清理%s失败: %v", desc, err)
				results = append(results, fmt.Sprintf("%s清理失败", desc))
			} else {
				results = append(results, fmt.Sprintf("%s已清理", desc))
			}
			done++
			if progressCb != nil {
				progressCb(done, total, "清理"+desc)
			}
			continue
		}

		// 设备级日志项，通过 SSH + sudo 清理
		cmd, ok := cleanCmds[cat]
		if !ok {
			continue
		}
		_, err := SSHExecSimple(ip, "echo 'myt' | sudo -S sh -c '"+cmd+"'")
		if err != nil {
			log.Printf("[维护] 清理%s失败: %v", cat, err)
			results = append(results, fmt.Sprintf("%s清理失败", catLabel(cat)))
		} else {
			results = append(results, fmt.Sprintf("%s已清理", catLabel(cat)))
		}
		done++
		if progressCb != nil {
			progressCb(done, total, "清理"+catLabel(cat))
		}
	}

	return map[string]interface{}{
		"success": true,
		"message": strings.Join(results, "，"),
	}
}

func catLabel(key string) string {
	labels := map[string]string{
		"docker_container": "Docker容器日志",
		"docker_log":       "Docker服务日志",
		"messages":         "系统日志",
		"rotated":          "轮转归档",
		"other_log":        "其他服务日志",
		"pkg_cache":        "包管理缓存",
		"tmp":              "临时文件",
	}
	if l, ok := labels[key]; ok {
		return l
	}
	return key
}

// getAndroidLogSize 通过 DockerExec 获取云机内部日志大小（MB）
// 用 du -sk (KB精度) 避免 du -sm 空目录显示1MB的问题
func getAndroidLogSize(client *sdkapi.Client, androidName string, paths ...string) float64 {
	combined := strings.Join(paths, " ")
	cmd := fmt.Sprintf("du -sk %s 2>/dev/null | awk '{s+=$1} END {print s+0}'", combined)
	resp, err := client.DockerExec(androidName, cmd)
	if err != nil {
		return 0
	}
	output := strings.TrimSpace(resp.Output)
	if output == "" || output == "0" {
		return 0
	}
	var kb float64
	fmt.Sscanf(output, "%f", &kb)
	return kb / 1024 // KB → MB
}
