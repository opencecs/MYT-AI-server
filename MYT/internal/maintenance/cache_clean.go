package maintenance

import (
	"fmt"
	"log"
	"strings"

	"github.com/moyunteng/myt/internal/sdkapi"
)

// CacheItem 缓存项信息
type CacheItem struct {
	Key    string  `json:"key"`
	Label  string  `json:"label"`
	SizeMB float64 `json:"sizeMB"`
}

// GetAndroidCacheUsage 获取安卓缓存占用（通过 DockerExec 在容器内执行，合并为一次调用）
func GetAndroidCacheUsage(deviceIP, androidName string, client *sdkapi.Client) map[string]interface{} {
	// 一次 DockerExec 执行所有 du 命令，用标签分隔输出
	// 注意：用 du -sk (KB精度) 而非 du -sm (MB精度)，避免空目录显示1MB
	cmd := `echo "===cache==="; du -sk /data/data/*/cache/ 2>/dev/null | awk '{s+=$1} END {print s+0}'; echo "===code_cache==="; du -sk /data/data/*/code_cache/ 2>/dev/null | awk '{s+=$1} END {print s+0}'; echo "===tmp==="; du -sk /data/local/tmp/ 2>/dev/null | awk '{s+=$1} END {print s+0}'`

	resp, err := client.DockerExec(androidName, cmd)
	if err != nil {
		return map[string]interface{}{"success": false, "message": fmt.Sprintf("获取缓存大小失败: %v", err)}
	}

	output := resp.Output
	cacheMB := parseTagValue(output, "cache")
	codeCacheMB := parseTagValue(output, "code_cache")
	tmpMB := parseTagValue(output, "tmp")

	items := []CacheItem{
		{Key: "cache", Label: "应用缓存", SizeMB: cacheMB},
		{Key: "code_cache", Label: "代码缓存", SizeMB: codeCacheMB},
		{Key: "tmp", Label: "临时文件", SizeMB: tmpMB},
	}

	return map[string]interface{}{
		"success": true,
		"items":   items,
	}
}

// parseTagValue 从标签分隔的输出中提取数值（KB→MB）
func parseTagValue(output, tag string) float64 {
	marker := "===" + tag + "==="
	idx := strings.Index(output, marker)
	if idx == -1 {
		return 0
	}
	after := output[idx+len(marker):]
	// 取到下一个标签或末尾
	end := len(after)
	if nextIdx := strings.Index(after, "==="); nextIdx > 0 {
		end = nextIdx
	}
	val := strings.TrimSpace(after[:end])
	var kb float64
	fmt.Sscanf(val, "%f", &kb)
	return kb / 1024 // KB → MB
}

// CleanAndroidCache 清理安卓缓存（通过 DockerExec 在容器内执行）
func CleanAndroidCache(deviceIP, androidName string, categories []string, client *sdkapi.Client) map[string]interface{} {
	return CleanAndroidCacheWithProgress(deviceIP, androidName, categories, client, nil)
}

// CleanAndroidCacheWithProgress 带进度回调的缓存清理：每完成一个 category 调用 progressCb(done, total, stage)。
func CleanAndroidCacheWithProgress(deviceIP, androidName string, categories []string, client *sdkapi.Client, progressCb func(done, total int, stage string)) map[string]interface{} {
	var results []string
	done := 0
	total := len(categories)
	for _, cat := range categories {
		var cmd string
		var desc string
		switch cat {
		case "cache":
			cmd = "rm -rf /data/data/*/cache/*"
			desc = "应用缓存"
		case "code_cache":
			cmd = "rm -rf /data/data/*/code_cache/*"
			desc = "代码缓存"
		case "tmp":
			cmd = "rm -rf /data/local/tmp/*"
			desc = "临时文件"
		default:
			continue
		}

		resp, err := client.DockerExec(androidName, cmd)
		if err != nil {
			log.Printf("[维护] 清理%s失败: %v", desc, err)
			results = append(results, fmt.Sprintf("%s清理失败", desc))
			continue
		}
		if resp.ExitCode != 0 {
			log.Printf("[维护] 清理%s失败(exit=%d): %s", desc, resp.ExitCode, resp.Output)
			results = append(results, fmt.Sprintf("%s清理失败", desc))
		} else {
			results = append(results, fmt.Sprintf("%s已清理", desc))
		}
		done++
		if progressCb != nil {
			progressCb(done, total, "清理"+desc)
		}
	}

	return map[string]interface{}{
		"success": true,
		"message": strings.Join(results, "，"),
	}
}

// getCacheSize 通过 DockerExec 执行 du 命令获取缓存大小（MB）
func getCacheSize(client *sdkapi.Client, androidName, path string) float64 {
	cmd := fmt.Sprintf("du -sk %s 2>/dev/null | awk '{s+=$1} END {print s+0}'", path)
	resp, err := client.DockerExec(androidName, cmd)
	if err != nil {
		log.Printf("[维护] 获取缓存大小失败(%s): %v", path, err)
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
