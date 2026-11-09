package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// ─── 侧边栏文件上传 + APK 安装（平台无关协议层） ─────────────────────────────
//
// 与 MYT 客户端 UploadToCloud（sdkapi_service_file.go）保持同一条安装路径：
//   - .apks/.xapk/.apkm：容器级 /installapks 直传安装（设备端处理分包）
//   - .apk：容器 /upload 上传 → SDK /android/exec 在目标容器内执行
//     cp → pm install -r -t（判定输出含 "Success"）→ rm 清理
//   - 其他文件：仅上传到 /sdcard/upload
//
// 旧路径（modifydev?cmd=6 设备级 shell）已删除：它没有容器上下文，会装错位置；
// 且共用 httpClient 的 10s 全局超时，稍大的 APK 上传/安装必超时。
//
// UI 层（Windows overlay / Darwin overlay）通过 onStatus 回调接收进度，
// 本层不做任何 UI 操作。

// uploadStatus 状态回调：text 为主文案（通常文件名），detail 为进度/结果描述，
// pct 为 0-100 进度；done=true 时 detail 为最终结果（err 非空表示失败）。
// 进度仅推送侧边栏（无视频区 overlay）。
type uploadStatus func(text, detail string, pct int, done bool, err error)

// uploading 上传并发互斥：单窗口同一时刻只允许一次上传/安装
// （upload_file 对话框是模态的，但拖拽/base64 消息与对话框路径可能并发）。
var uploading atomic.Bool

// apkAutoGrant 本次上传是否自动授予运行时权限（pm install -g）。
// 侧边栏每次上传时随消息传入（upload_file/upload_file_base64 的 autogrant 字段），
// upload_file 对话框是模态的 + uploading 互斥保证同一时刻只有一个上传任务，
// App 级单字段安全。默认 false。
var apkAutoGrant atomic.Bool

// startUpload 互斥入口：已在传则拒绝。返回 false 表示有任务在跑。
func startUpload() bool { return uploading.CompareAndSwap(false, true) }

func endUpload() { uploading.Store(false) }

// 安装/上传专用超时：不复用 a.httpClient（其 10s 全局超时会掐死长传输）。
const (
	uploadHTTPTimeout = 30 * time.Minute // 容器 /upload、/installapks 传输
	execHTTPTimeout   = 5 * time.Minute  // SDK /android/exec（pm install 大 APK 常超 10s，客户端同款 5min）
)

// uploadClient 长超时 HTTP client（上传/安装专用，独立于 a.httpClient）。
var uploadClient = &http.Client{
	Timeout: uploadHTTPTimeout,
	Transport: &http.Transport{
		DisableCompression: true,
	},
}

// execClient SDK exec 专用 client（5min）。
var execClient = &http.Client{
	Timeout: execHTTPTimeout,
	Transport: &http.Transport{
		DisableCompression: true,
	},
}

// UploadAndInstall 上传文件到投屏对应的云机；APK/XAPK 安装（客户端同款路径）。
// data 为完整文件内容（侧边栏拖拽的小文件路径，≤200MB）。
func (a *App) UploadAndInstall(fileName string, data []byte, onStatus uploadStatus) {
	if !a.beginUploadCheck(fileName, onStatus) {
		return
	}
	defer endUpload()

	ext := strings.ToLower(filepath.Ext(fileName))
	switch ext {
	case ".apks", ".xapk", ".apkm":
		a.installApks(fileName, data, onStatus)
	case ".apk":
		a.uploadThenInstallAPK(fileName, data, onStatus)
	default:
		a.uploadFile(fileName, data, onStatus)
	}
}

// UploadAndInstallPath 文件路径版上传安装（系统对话框选择的任意大小文件）。
// 流式传输（io.Pipe + multipart），不做全量内存拷贝——944MB 级 XAPK 在多窗口
// 播放器进程里全量进内存不可行（每窗口独立进程，批量投屏内存叠加）。
func (a *App) UploadAndInstallPath(filePath string, onStatus uploadStatus) {
	if !a.beginUploadCheck(filepath.Base(filePath), onStatus) {
		return
	}
	defer endUpload()

	fi, err := os.Stat(filePath)
	if err != nil {
		onStatus(filepath.Base(filePath), "读取文件失败", 0, true, err)
		return
	}
	fileName := filepath.Base(filePath)
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".apks", ".xapk", ".apkm":
		a.installApksReader(fileName, filePath, fi.Size(), onStatus)
	case ".apk":
		a.uploadThenInstallAPKReader(fileName, filePath, fi.Size(), onStatus)
	default:
		a.uploadFileReader(fileName, filePath, fi.Size(), onStatus)
	}
}

// beginUploadCheck 上传前置检查（端口/连接/并发互斥），失败已回调终态则返回 false。
func (a *App) beginUploadCheck(fileName string, onStatus uploadStatus) bool {
	if a.ApiPort <= 0 {
		onStatus(fileName, "需要 -apiport 参数", 0, true, fmt.Errorf("未配置API端口"))
		return false
	}
	if a.RTSPURL == "" {
		onStatus(fileName, "未连接设备", 0, true, fmt.Errorf("未连接设备"))
		return false
	}
	if !startUpload() {
		onStatus(fileName, "已有上传任务进行中，请稍候", 0, true, fmt.Errorf("上传冲突"))
		return false
	}
	return true
}

// uploadFile 上传普通文件到容器 /sdcard/upload（解析响应 data.path，不安装）。
func (a *App) uploadFile(fileName string, data []byte, onStatus uploadStatus) {
	a.postMultipart("/upload", fileName, bytes.NewReader(data), int64(len(data)), onStatus, func(remotePath string, respBody []byte) {
		onStatus(fileName, "上传成功 — /sdcard/upload", 100, true, nil)
	})
}

// uploadFileReader uploadFile 的文件路径版（流式，任意大小）。
func (a *App) uploadFileReader(fileName, filePath string, size int64, onStatus uploadStatus) {
	a.postFileMultipart("/upload", fileName, filePath, size, onStatus, func(remotePath string, respBody []byte) {
		onStatus(fileName, "上传成功 — /sdcard/upload", 100, true, nil)
	})
}

// uploadThenInstallAPK 单 APK：容器 /upload 上传 → SDK 容器内 cp + pm install + rm。
func (a *App) uploadThenInstallAPK(fileName string, data []byte, onStatus uploadStatus) {
	a.postMultipart("/upload", fileName, bytes.NewReader(data), int64(len(data)), onStatus, func(remotePath string, respBody []byte) {
		a.installAPKInContainer(fileName, remotePath, onStatus)
	})
}

// uploadThenInstallAPKReader uploadThenInstallAPK 的文件路径版（流式，任意大小）。
func (a *App) uploadThenInstallAPKReader(fileName, filePath string, size int64, onStatus uploadStatus) {
	a.postFileMultipart("/upload", fileName, filePath, size, onStatus, func(remotePath string, respBody []byte) {
		a.installAPKInContainer(fileName, remotePath, onStatus)
	})
}

// uploadProgressReader 上传字节进度统计：wrap 在 multipart part 的 io.Copy 源上，
// 每读 1% 回调一次（节流，避免高频刷侧边栏）。字节进度映射到总进度的 0-70%，
// 75%+ 留给安装阶段（cp/pm install），100% 为完成。
type uploadProgressReader struct {
	r       io.Reader
	total   int64
	read    int64
	lastPct int
	onPct   func(pct int) // 入参为 0-100 的字节百分比
}

func (pr *uploadProgressReader) Read(p []byte) (int, error) {
	n, err := pr.r.Read(p)
	if n > 0 && pr.total > 0 {
		pr.read += int64(n)
		pct := int(pr.read * 100 / pr.total)
		if pct > 100 {
			pct = 100
		}
		if pct != pr.lastPct {
			pr.lastPct = pct
			if pr.onPct != nil {
				pr.onPct(pct)
			}
		}
	}
	return n, err
}

// postMultipart 容器级 multipart POST（内容已在内存）。
func (a *App) postMultipart(path, fileName string, content io.Reader, size int64, onStatus uploadStatus, onDone func(remotePath string, respBody []byte)) {
	prog := &uploadProgressReader{r: content, total: size,
		onPct: func(pct int) { onStatus(fileName, fmt.Sprintf("上传中 %d%%", pct), pct*7/10, false, nil) }}
	a.postMultipartCommon(path, fileName, func(w *multipart.Writer) error {
		part, err := w.CreateFormFile("file", fileName)
		if err != nil {
			return err
		}
		_, err = io.Copy(part, prog)
		return err
	}, size, onStatus, onDone)
}

// postFileMultipart 容器级 multipart POST（文件流式读取，任意大小零内存拷贝）。
func (a *App) postFileMultipart(path, fileName, filePath string, size int64, onStatus uploadStatus, onDone func(remotePath string, respBody []byte)) {
	prog := &uploadProgressReader{r: nil, total: size,
		onPct: func(pct int) { onStatus(fileName, fmt.Sprintf("上传中 %d%%", pct), pct*7/10, false, nil) }}
	a.postMultipartCommon(path, fileName, func(w *multipart.Writer) error {
		f, err := os.Open(filePath)
		if err != nil {
			return err
		}
		defer f.Close()
		part, err := w.CreateFormFile("file", fileName)
		if err != nil {
			return err
		}
		prog.r = f
		_, err = io.Copy(part, prog)
		return err
	}, size, onStatus, onDone)
}

// postMultipartCommon multipart POST 公共实现。fill 由调用方写入 form 内容
// （pipe 异步执行，错误经 CloseWithError 传回主流程）。onDone 收到
// （远端路径, 响应体）——/upload 解析 data.path，/installapks 按 code 判定。
func (a *App) postMultipartCommon(path, fileName string, fill func(w *multipart.Writer) error, size int64, onStatus uploadStatus, onDone func(remotePath string, respBody []byte)) {
	host := a.DeviceHost()
	targetURL := fmt.Sprintf("http://%s:%d%s", host, a.ApiPort, path)

	onStatus(fileName, "准备上传...", 2, false, nil)
	pr, pw := io.Pipe()
	w := multipart.NewWriter(pw)
	go func() {
		if err := fill(w); err != nil {
			pw.CloseWithError(err)
			return
		}
		w.Close()
		pw.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), uploadHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, pr)
	if err != nil {
		onStatus(fileName, "创建请求失败", 0, true, err)
		return
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.ContentLength = -1 // multipart 流式（boundary 长度未知，chunked 传输）
	resp, err := uploadClient.Do(req)
	if err != nil {
		onStatus(fileName, "网络错误", 0, true, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		logf("[upload] %s status=%d body=%q\n", path, resp.StatusCode, respBody)
		onStatus(fileName, fmt.Sprintf("上传失败 (HTTP %d) — /sdcard/upload", resp.StatusCode), 0, true, fmt.Errorf("HTTP %d", resp.StatusCode))
		return
	}

	// 解析远端路径（data.path），失败回退约定路径
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	remotePath := ""
	var result struct {
		Data struct {
			Path string `json:"path"`
		} `json:"data"`
	}
	if json.Unmarshal(respBody, &result) == nil {
		remotePath = result.Data.Path
	}
	if remotePath == "" {
		remotePath = fmt.Sprintf("/sdcard/upload/%s", fileName)
	}
	onDone(remotePath, respBody)
}

// installApks XAPK/APKS/APKM 分包安装：容器级 /installapks 直传（设备端解包安装）。
// 与客户端 mytos container client InstallApks 同接口：multipart "file" 字段 + ZIP 内容，
// 非 JSON 响应视为成功，JSON 响应按 code 判定。
func (a *App) installApks(fileName string, data []byte, onStatus uploadStatus) {
	a.postMultipart("/installapks", fileName, bytes.NewReader(data), int64(len(data)), onStatus, a.installApksDone(fileName, onStatus))
}

// installApksReader installApks 的文件路径版（流式，任意大小）。
func (a *App) installApksReader(fileName, filePath string, size int64, onStatus uploadStatus) {
	a.postFileMultipart("/installapks", fileName, filePath, size, onStatus, a.installApksDone(fileName, onStatus))
}

// installApksDone /installapks 响应判定：非 JSON 视为成功（与客户端一致），
// JSON 按 code（200/0 通过）。
func (a *App) installApksDone(fileName string, onStatus uploadStatus) func(remotePath string, respBody []byte) {
	return func(remotePath string, respBody []byte) {
		var result struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if json.Unmarshal(respBody, &result) == nil && result.Code != 0 && result.Code != 200 {
			onStatus(fileName, fmt.Sprintf("安装失败: %s", result.Msg), 0, true, fmt.Errorf("code=%d %s", result.Code, result.Msg))
			return
		}
		onStatus(fileName, "安装成功", 100, true, nil)
	}
}

// installAPKInContainer 容器内安装单 APK（客户端 UploadToCloud 同款命令序列）：
// cp 远端→/data/local/tmp/<原名> → pm install -r -t（判定输出含 "Success"）→ rm 清理。
func (a *App) installAPKInContainer(fileName, remotePath string, onStatus uploadStatus) {
	if a.ContainerName == "" || a.SdkHost == "" {
		// 主程序未注入容器上下文（手动调试等场景）：文件已上传，安装不可用
		onStatus(fileName, "上传成功 — /sdcard/upload（未配置容器信息，无法安装）", 100, true, nil)
		return
	}

	installPath := fmt.Sprintf("/data/local/tmp/%s", fileName)

	onStatus(fileName, "准备安装...", 75, false, nil)
	// cp 到容器可执行的临时目录（SDK exec 的 shell 在容器内，/sdcard 挂载点对 pm 不可读）
	if _, err := a.sdkExec(fmt.Sprintf("cp '%s' '%s'", remotePath, installPath)); err != nil {
		onStatus(fileName, fmt.Sprintf("文件已上传但安装准备失败: %v", err), 0, true, err)
		return
	}

	// pm install 同步执行：-r 覆盖安装，-t 允许测试包；侧边栏勾选自动授权时追加 -g
	// （默认不带，与客户端一致，按 Android 标准由 App 运行时按需申请）。
	onStatus(fileName, "正在安装...", 85, false, nil)
	installCmd := "pm install -r -t"
	if apkAutoGrant.Load() {
		installCmd = "pm install -r -t -g"
	}
	output, err := a.sdkExec(fmt.Sprintf("%s '%s'", installCmd, installPath))
	// 清理临时文件（尽力而为，不阻塞结果上报）
	go a.sdkExec(fmt.Sprintf("rm -f '%s'", installPath))
	if err != nil {
		onStatus(fileName, fmt.Sprintf("安装失败: %v", err), 0, true, err)
		return
	}
	// 安装结果判定：pm install 成功输出 "Success"；只查 Failure 会放过其他形式失败
	out := strings.TrimSpace(output)
	logf("[install] %s → %s: output=%q\n", fileName, a.ContainerName, out)
	if !strings.Contains(out, "Success") {
		err := fmt.Errorf("pm install 未成功: %s", out)
		onStatus(fileName, fmt.Sprintf("安装失败: %s", out), 0, true, err)
		return
	}
	onStatus(fileName, "安装成功", 100, true, nil)
}

// sdkExec 在目标容器内执行 shell 命令。
// 非 V2：设备 SDK /android/exec（`sd -c` 形式），与 MYT 客户端 sdkapi.Client.DockerExec
// 同协议：POST {name, command:["sd","-c",cmd]}，Basic Auth（admin/密码可空）。
// V2（-v2mode）：V2 主机无 8000 SDK，改走主机 81 POST /and_api/v1/shell/{ip}/{容器}
// （body {"cmd": cmd}，无鉴权），语义对照主仓 internal/sdkapi/v2route.go 的
// /android/exec 翻译——命令本身直接执行（无需 sd -c 包装），{code:200} 归一为成功，
// 失败按 v2BusinessError 语义取 message/msg。
// 均 5 分钟超时；exitCode 非零视为执行失败。
func (a *App) sdkExec(cmdline string) (string, error) {
	if a.SdkHost == "" {
		return "", fmt.Errorf("未配置设备 SDK 地址")
	}
	if a.V2Mode {
		return a.sdkExecV2(cmdline)
	}
	apiURL := fmt.Sprintf("http://%s:8000/android/exec", a.SdkHost)
	payload, _ := json.Marshal(map[string]any{
		"name":    a.ContainerName,
		"command": []string{"sd", "-c", cmdline},
	})
	ctx, cancel := context.WithTimeout(context.Background(), execHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if a.SdkPassword != "" {
		req.SetBasicAuth("admin", a.SdkPassword)
	}
	resp, err := execClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("网络错误: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("设备鉴权失败 (401)")
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d: %.200s", resp.StatusCode, string(body))
	}
	// 标准响应 {code, data:{exitCode, output}}；code 非 200 为设备侧拒绝
	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			ExitCode int    `json:"exitCode"`
			Output   string `json:"output"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		// 非标准响应（纯文本命令输出或错误文本）。
		// 空响应容错（与客户端 parseAPIResponse 同款语义）：cp/rm 等静默命令成功时
		// 接口返回空 body——视为成功无输出。真正的容器异常（未运行/不存在）实测
		// 返回非空文本「Exec Create Error: ...」，由下方 daemon 错误关键词判定。
		raw := strings.TrimSpace(string(body))
		if raw == "" {
			return "", nil
		}
		// docker daemon 错误（Exec Create Error: ... is not running / No such container）
		// 同为纯文本，必须显式判失败——实测容器停止/被删时接口仍返回 HTTP 200 + 此类文本
		if strings.Contains(raw, "Error response from daemon") || strings.Contains(raw, "Exec Create Error") {
			return "", fmt.Errorf("容器不可用: %.120s", raw)
		}
		if strings.Contains(raw, "OCI runtime exec failed") ||
			strings.Contains(raw, "executable file not found") ||
			strings.Contains(raw, "no such file or directory") {
			return "", fmt.Errorf("安卓容器未授权或未正常运行")
		}
		return raw, nil
	}
	if result.Code != 200 {
		msg := result.Msg
		if msg == "" {
			msg = fmt.Sprintf("code=%d", result.Code)
		}
		return "", fmt.Errorf("设备拒绝: %s", msg)
	}
	if result.Data.ExitCode != 0 {
		return "", fmt.Errorf("容器命令执行失败(exitCode=%d): %s", result.Data.ExitCode, result.Data.Output)
	}
	return result.Data.Output, nil
}

// sdkExecV2 V2 部署模式的容器内命令执行：主机 81 POST /and_api/v1/shell/{ip}/{容器}。
// 无 Basic Auth；{code:200}=成功（data 字段为命令输出），失败按 v2BusinessError
// 语义报错（HTTP 恒 200，业务码在 body——对照主仓 internal/sdkapi/v2route.go）。
func (a *App) sdkExecV2(cmdline string) (string, error) {
	apiURL := fmt.Sprintf("http://%s:81/and_api/v1/shell/%s/%s", a.SdkHost, a.SdkHost, url.PathEscape(a.ContainerName))
	payload, _ := json.Marshal(map[string]string{"cmd": cmdline})
	ctx, cancel := context.WithTimeout(context.Background(), execHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := execClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("网络错误: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d: %.200s", resp.StatusCode, string(body))
	}
	// 业务码拦截：{code:200}=成功；code 缺失（非 JSON/其他风格响应）视为成功，
	// 其余数字 code 报业务错误（message/msg 取其一）
	var result struct {
		Code    *float64 `json:"code"`
		Message string   `json:"message"`
		Msg     string   `json:"msg"`
		Data    any      `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		// 非 JSON：纯文本输出按成功返回（与 8000 通道同款容错）
		return strings.TrimSpace(string(body)), nil
	}
	if result.Code != nil && *result.Code != 200 {
		msg := result.Message
		if msg == "" {
			msg = result.Msg
		}
		if msg == "" {
			msg = fmt.Sprintf("code=%d", int(*result.Code))
		}
		return "", fmt.Errorf("V2 主机返回错误（code=%d）: %s", int(*result.Code), msg)
	}
	// 成功：输出取 data 字段（字符串直接用；非字符串转 JSON 文本），data 缺失回退原始 body
	switch out := result.Data.(type) {
	case string:
		return out, nil
	case nil:
		return strings.TrimSpace(string(body)), nil
	default:
		if b, err := json.Marshal(out); err == nil {
			return string(b), nil
		}
		return strings.TrimSpace(string(body)), nil
	}
}
