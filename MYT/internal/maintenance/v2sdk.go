package maintenance

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/sdkver"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// V2 盒子 SDK（myt-sdk-go-v2）自动部署。
//
// 背景：V2 主机原生只有 81（mytsdk 封装）与 2375，没有 8000——客户端大量
// 8000 风格接口（国家列表/switchModel 等）被 v2 翻译层拒之门外。myt-sdk-go-v2
// 部署到盒子后监听 8000，提供完整 8000 API。本文件在 V2 设备添加时：
//
//	检测 http://{ip}:8000/info → 有响应：不管；无响应：下载→上传→安装→拉起→验证
//
// 下载无需鉴权（实测 2026-10-10：/sdk/download-url 免 token，CDN 直链公开）。
// SSH 凭据沿用 ssh_utils 的 user/myt；sudo 喂密码沿用 v2.sh 的 SUDO_PASS 语义。

const (
	// v2SdkType newapi 上 V2 盒子 SDK 的类型标识（版本号动态查询，见 DeployV2Sdk）
	v2SdkType = "v2_box_sdk"
	// v2SdkAPIBase newapi SDK 接口
	v2SdkAPIBase = "https://newapi.moyunteng.com/api/v1"
	// v2SdkRemoteDir 设备端安装目录（与 v2.sh 约定一致）
	v2SdkRemoteDir = "/home/user/v2"
	// v2SdkRemoteBin 设备端二进制名（v2.sh 按名匹配进程，勿改）
	v2SdkRemoteBin = "myt-sdk-go-v2"
)

// probeV2SdkProcPath 探测设备上 8000 端口 SDK 进程的二进制真实路径（SSH）。
// 老版本设备（v1/"2.0.0"）二进制可能不在 /home/user/v2/myt-sdk-go-v2——SSH 部署
// 若只 mv 覆盖约定路径，旧进程占着 8000、拉起的新进程 bind 失败退出，升级「永远
// 装不上」。探测链：ss/netstat 找 8000 监听 PID → /proc/{pid}/exe 真实路径 →
// pgrep 兜底。返回空串 = 没找到（新装场景，用约定路径）。
func probeV2SdkProcPath(client *ssh.Client) string {
	_, p := probeV2SdkProcPathEx(client)
	return p
}

// readProcExe 读 /proc/{pid}/exe（符号链接指向真实二进制；需 root——sudo readlink）。
//
// (deleted) 后缀：二进制文件本体已被删除但进程仍占着旧 inode 时，内核给
// readlink 结果追加 " (deleted)"（实测 10.10.2.46：mv 走旧二进制后
// readlink → "/home/user/v2/myt-sdk-go-v2.oldbin (deleted)"）。必须 strip，
// 否则带后缀的字符串被当 mv 目标 → mv 非法 exit 2（2026-10-14 现场失败根因）。
func readProcExe(client *ssh.Client, pid string) string {
	out, err := RunSSHCmd(client, fmt.Sprintf(
		"printf '%%s\\n' myt | sudo -S readlink /proc/%s/exe 2>/dev/null", pid))
	if err != nil {
		return ""
	}
	p := strings.TrimSpace(out)
	if p == "" || !strings.HasPrefix(p, "/") {
		return "" // sudo 密码提示行/报错都会混进来，非绝对路径即无效
	}
	return strings.TrimSuffix(p, " (deleted)")
}

// probeV2SdkProcPathEx 探测 8000 SDK 进程（PID + 真实二进制路径），比
// probeV2SdkProcPath 多返回 PID——精确 kill 用（busybox pkill 只支持 -f，
// 二进制名非约定名时按名匹配会杀漏）。
// 返回 pid="" 表示没找到（新装场景，用约定路径）。
func probeV2SdkProcPathEx(client *ssh.Client) (pid, procPath string) {
	// 8000 监听 PID（ss 优先，busybox netstat 兜底）
	out, err := RunSSHCmd(client,
		`ss -lntp 2>/dev/null | grep ':8000 ' || netstat -lntp 2>/dev/null | grep ':8000 '`)
	if err == nil {
		// 提取 users:(("myt-sdk-go-v2",pid=1234,...)) 里的 pid=
		for _, seg := range strings.Fields(out) {
			if i := strings.Index(seg, "pid="); i >= 0 {
				if p := digitsOnly(seg[i+4:]); p != "" {
					if path := readProcExe(client, p); path != "" {
						return p, path
					}
				}
			}
		}
	}
	// pgrep 兜底：按两种历史二进制名匹配（老版本 myt-sdk-v2 / 现版本 myt-sdk-go-v2）
	out, err = RunSSHCmd(client, `pgrep -f 'myt-sdk(-go)?-v2' | head -1`)
	if err == nil {
		if p := digitsOnly(strings.TrimSpace(out)); p != "" {
			if path := readProcExe(client, p); path != "" {
				return p, path
			}
		}
	}
	return "", ""
}

// digitsOnly 取字符串前导数字段（"1234,users:..." → "1234"）。
func digitsOnly(s string) string {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return s[:i]
}

// CheckV2SdkAlive 探测设备 8000 是否已有 SDK 服务（HTTP /info，2s 超时）。
// 有任何 HTTP 响应即视为已部署（不校验内容——8000 上是别的服务也不动它）。
func CheckV2SdkAlive(deviceIP string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s:8000/info", deviceIP))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

// checkV2SdkHasInfo 判定设备 8000 是否有「可用的 /info 接口」：
// HTTP 200 且响应是 JSON 且解析出 status 或 currentVersion 任一字段才算有。
// 404/超时/连接拒绝/非 JSON（8000 上可能跑着完全不同的服务甚至非 HTTP）都算没有。
// 与 CheckV2SdkAlive 的区别：Alive 只要有任何 HTTP 响应就算，老版本 SDK 8000
// 活着但无 /info 时会被误判为已部署——按「有 /info 才跳过升级」的语义应走本函数。
//
// 响应结构（实测 10.10.2.46 v2_box_sdk v3）：
// {"code":0,"message":"OK","data":{"status":"ok","currentVersion":"3",...}}——
// status/currentVersion 在 data 下；老版本可能顶层平铺，两层都试。
func checkV2SdkHasInfo(deviceIP string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s:8000/info", deviceIP))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	// 限制读取体积，避免把非 JSON 大响应整个拉回
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return false
	}
	return parseV2SdkInfoBody(body)
}

// parseV2SdkInfoBody 判定 /info 响应体是否「可用」：JSON 解析成功且
// status/currentVersion 任一非空（顶层或 data 层任一处）。抽成纯函数便于单测。
func parseV2SdkInfoBody(body []byte) bool {
	var info struct {
		Status         string `json:"status"`
		CurrentVersion string `json:"currentVersion"`
		Data           struct {
			Status         string `json:"status"`
			CurrentVersion string `json:"currentVersion"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &info) != nil {
		return false
	}
	return info.Status != "" || info.CurrentVersion != "" ||
		info.Data.Status != "" || info.Data.CurrentVersion != ""
}

// HasV2SdkInfo checkV2SdkHasInfo 的导出包装（main 包触发点判断用）。
func HasV2SdkInfo(deviceIP string) bool {
	return checkV2SdkHasInfo(deviceIP)
}

// v2SdkProgress 部署进度回调（stage: download/verify/upload/install/verify_run）
type v2SdkProgress func(stage, message string)

// DownloadV2Sdk 从 newapi 拉取 SDK 包并解出二进制（本地临时目录）。
// 返回二进制路径；调用方负责删除临时目录。
func DownloadV2Sdk(version string, progress v2SdkProgress) (string, error) {
	zipPath, err := FetchV2SdkZip(version, progress)
	if err != nil {
		return "", err
	}
	defer os.Remove(zipPath)
	return extractV2SdkZip(zipPath, version)
}

// extractV2SdkZip 解压 zip（myt-sdk-v2 + hash + version 三文件）并校验，
// 返回解出的二进制路径（临时文件，调用方负责删除——DeployV2Sdk 有 defer os.Remove）。
func extractV2SdkZip(zipPath, version string) (string, error) {
	if err := verifyV2SdkZip(zipPath, version); err != nil {
		return "", err
	}
	return extractV2SdkBinary(zipPath)
}

// extractV2SdkBinary 从 zip 解出 myt-sdk-v2 二进制到临时文件。
func extractV2SdkBinary(zipPath string) (string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("打开 SDK 包失败: %w", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if strings.ToLower(filepath.Base(f.Name)) != "myt-sdk-v2" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		tmp, err := os.CreateTemp(os.TempDir(), "myt-sdk-v2-*")
		if err != nil {
			rc.Close()
			return "", err
		}
		if _, err := io.Copy(tmp, rc); err != nil {
			tmp.Close()
			rc.Close()
			os.Remove(tmp.Name())
			return "", err
		}
		tmp.Close()
		rc.Close()
		return tmp.Name(), nil
	}
	return "", fmt.Errorf("SDK 包内缺少二进制 myt-sdk-v2")
}

// FetchV2SdkZip 从 newapi 下载指定版本的 SDK 原始 zip（保留 zip 本体）。
// 返回 zip 路径；调用方负责删除。供两条链路复用：
//   - DownloadV2Sdk：解出二进制走 SFTP 部署（DeployV2Sdk）
//   - InstallV2SdkVersion：原始 zip 直接上传设备 /server/upgrade/upload
//     （设备端要求的包结构就是 zip 内 myt-sdk-v2 + hash + version 三文件，
//     与云端原始包一致，无需重新打包）
func FetchV2SdkZip(version string, progress v2SdkProgress) (string, error) {
	if progress == nil {
		progress = func(string, string) {} // 防御：任务队列等无进度回调的调用方传 nil
	}
	// 版本号拼进 download-url 与本地临时文件名，非法字符会打出坏 URL/路径——
	// 在共享入口统一拦截，报清晰错误而不是深入到下载/解析才失败
	if version == "" {
		return "", fmt.Errorf("无效的 SDK 版本号: %q", version)
	}
	if strings.ContainsAny(version, "/\\?&#% \t\r\n\"'") {
		return "", fmt.Errorf("SDK 版本号含非法字符: %q", version)
	}
	progress("download", "获取下载地址")
	url := fmt.Sprintf("%s/sdk/download-url?sdk_type=v2_box_sdk&version=%s&filename=myt-sdk-v2.zip", v2SdkAPIBase, version)
	dlURL, err := fetchV2SdkDownloadURL(url)
	if err != nil {
		return "", err
	}

	progress("download", "下载 SDK 包")
	zipPath := filepath.Join(os.TempDir(), fmt.Sprintf("myt-sdk-v2-%s.zip", version))
	if err := downloadFile(dlURL, zipPath); err != nil {
		return "", fmt.Errorf("下载 SDK 包失败: %w", err)
	}

	progress("verify", "校验")
	if err := verifyV2SdkZip(zipPath, version); err != nil {
		os.Remove(zipPath)
		return "", err
	}
	return zipPath, nil
}

// fetchV2SdkDownloadURL 解析 download-url（兼容 data.download_url / data.url / data 纯字符串）
func fetchV2SdkDownloadURL(apiURL string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("请求下载地址失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var r struct {
		CodeID int             `json:"code_id"`
		Msg    string          `json:"msg"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("解析下载地址响应失败: %w", err)
	}
	if r.CodeID != 200 {
		return "", fmt.Errorf("获取下载地址失败: code=%d msg=%s", r.CodeID, r.Msg)
	}
	// data 三种形态
	var s string
	var o struct {
		DownloadURL string `json:"download_url"`
		URL         string `json:"url"`
	}
	if json.Unmarshal(r.Data, &s) == nil && s != "" {
		return s, nil
	}
	if json.Unmarshal(r.Data, &o) == nil {
		if o.DownloadURL != "" {
			return o.DownloadURL, nil
		}
		if o.URL != "" {
			return o.URL, nil
		}
	}
	return "", fmt.Errorf("响应中无下载地址: %.200s", string(body))
}

// verifyV2SdkZip 校验 zip 内容（二进制 sha256 == zip 内 hash 文件；version 文件 == 请求版本）。
func verifyV2SdkZip(zipPath, version string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("打开 SDK 包失败: %w", err)
	}
	defer zr.Close()

	var binFile, hashContent, versionContent string
	for _, f := range zr.File {
		switch strings.ToLower(filepath.Base(f.Name)) {
		case "myt-sdk-v2":
			rc, err := f.Open()
			if err != nil {
				return err
			}
			tmp, err := os.CreateTemp(os.TempDir(), "myt-sdk-v2-*")
			if err != nil {
				rc.Close()
				return err
			}
			if _, err := io.Copy(tmp, rc); err != nil {
				tmp.Close()
				rc.Close()
				return err
			}
			tmp.Close()
			rc.Close()
			binFile = tmp.Name()
			defer os.Remove(binFile)
		case "hash":
			if hashContent, err = readZipFile(f); err != nil {
				return err
			}
		case "version":
			if versionContent, err = readZipFile(f); err != nil {
				return err
			}
		}
	}
	if binFile == "" {
		return fmt.Errorf("SDK 包内缺少二进制 myt-sdk-v2")
	}
	// sha256 校验
	f, err := os.Open(binFile)
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		f.Close()
		return err
	}
	f.Close()
	actual := hex.EncodeToString(h.Sum(nil))
	if hashContent != "" && actual != strings.TrimSpace(hashContent) {
		return fmt.Errorf("SDK 二进制 sha256 校验失败: 期望 %s 实际 %s", strings.TrimSpace(hashContent), actual)
	}
	if versionContent != "" && strings.TrimSpace(versionContent) != version {
		return fmt.Errorf("SDK 版本不匹配: 包内 %s 请求 %s", strings.TrimSpace(versionContent), version)
	}
	return nil
}

func readZipFile(f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, 1<<16))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// DeployV2Sdk 完整部署：下载 → SFTP 上传(.new) → mv 覆盖 → sudo 拉起 → /info 验证。
// 已在运行（8000 有响应）时直接返回成功（幂等）。
func DeployV2Sdk(deviceIP string, progress v2SdkProgress) error {
	if CheckV2SdkAlive(deviceIP) {
		return nil
	}
	return DeployV2SdkVersion(deviceIP, progress)
}

// DeployV2SdkVersion 无条件部署最新版（幂等门卫已由调用方把关，如
// EnsureV2SdkUpgraded 按 /info 判定需要升级）：下载 → SFTP 上传(.new) →
// mv 覆盖 → sudo 拉起（v2.sh restart，无 v2.sh 时 nohup 兜底）→ /info 验证。
func DeployV2SdkVersion(deviceIP string, progress v2SdkProgress) error {
	if progress == nil {
		progress = func(string, string) {}
	}

	// 版本号动态获取：服务端 v2_box_sdk 的发布版本为整数自增（如 "1"），
	// 与包内 version 文件一致；硬编码版本会导致 download-url 404
	progress("download", "查询最新版本")
	version, err := sdkver.FetchLatestVersionOfType(v2SdkType)
	if err != nil {
		return fmt.Errorf("查询 %s 最新版本失败: %w", v2SdkType, err)
	}
	log.Printf("[v2sdk] %s 最新发布版本: %s", v2SdkType, version)

	progress("download", "下载 SDK 包")
	binPath, err := DownloadV2Sdk(version, progress)
	if err != nil {
		return err
	}
	defer os.Remove(binPath)

	progress("upload", "上传到设备")
	sshClient, err := DialSSH(deviceIP)
	if err != nil {
		return fmt.Errorf("SSH 连接失败: %w", err)
	}
	defer sshClient.Close()
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return fmt.Errorf("SFTP 连接失败: %w", err)
	}
	defer sftpClient.Close()

	// 目录（v2.sh 可能不存在，确保目录在）
	if _, err := RunSSHCmd(sshClient, "mkdir -p "+v2SdkRemoteDir); err != nil {
		return fmt.Errorf("创建安装目录失败（mkdir -p %s）: %w", v2SdkRemoteDir, err)
	}
	remoteNew := v2SdkRemoteDir + "/" + v2SdkRemoteBin + ".new"
	if err := SftpUploadFile(sftpClient, binPath, remoteNew, 0755); err != nil {
		return fmt.Errorf("上传失败（SFTP → %s）: %w", remoteNew, err)
	}

	progress("install", "安装并启动")
	// 探测旧进程的真实二进制路径与 PID：老版本设备（v1/"2.0.0"）二进制可能不在
	// 约定路径——只 mv 覆盖约定路径会让旧进程继续占 8000、新进程 bind 失败，
	// 升级「永远装不上」。找到真实路径时替换它（mv -f rename 替换 inode，避免
	// ETXTBSY），让设备端/进程管理语义保持「跑的就是被替换的那个文件」。
	//
	// (deleted) 回退：进程占着已被删除的 inode（如上次部署把旧二进制 mv 走后
	// 未重启成功）时，readlink 会返回 "/path/to/bin (deleted)"——readProcExe
	// 已 strip 后缀，但 strip 出的路径在文件系统上已不存在，拿它当 mv 目标会在
	// 设备上造出一个 .oldbin 之类的怪名文件、约定路径却没被替换。此时回退约定
	// 路径安装（旧进程精确 kill 后新进程从约定路径拉起）。
	remoteBin := v2SdkRemoteDir + "/" + v2SdkRemoteBin
	oldPid, real := probeV2SdkProcPathEx(sshClient)
	if real != "" && real != remoteBin {
		if _, err := RunSSHCmd(sshClient, "test -e "+real); err != nil {
			log.Printf("[v2sdk] %s SDK 进程实际路径 %s 已失效（旧 inode 被删），回退约定路径 %s", deviceIP, real, remoteBin)
			real = ""
		}
	}
	if real != "" && real != remoteBin {
		log.Printf("[v2sdk] %s 检测到 SDK 进程实际路径 %s（非约定 %s），按实际路径替换", deviceIP, real, remoteBin)
		remoteBin = real
	}
	// mv 覆盖（rename 替换 inode，避免 ETXTBSY）→ sudo 后台拉起（需 root 读 docker.sock）
	if _, err := RunSSHCmd(sshClient, fmt.Sprintf(
		"chmod +x %s && mv -f %s %s", remoteNew, remoteNew, remoteBin)); err != nil {
		return fmt.Errorf("安装失败（chmod/mv %s → %s）: %w", remoteNew, remoteBin, err)
	}
	// 重启策略（按真实路径所在目录找 v2.sh；不存在则杀旧进程后 nohup 兜底）：
	// v2.sh restart 的进程匹配是「(^|/)myt-sdk-go-v2$」——旧二进制名不同时杀
	// 不掉、8000 端口被占，新进程起不来。所以非约定路径场景先精确 kill 旧进程。
	//
	// 注意必须用 path 包（POSIX 语义）处理远程路径——filepath 在 Windows 宿主
	// 上把 /home/user/v2 切成 \home\user\v2，设备端 cd 直接失败（2026-10-14
	// 实测：Windows 客户端跑 DeployV2SdkVersion 在 nohup 拉起步必挂）。
	binDir := path.Dir(remoteBin)
	binName := path.Base(remoteBin)
	// sudo 喂密码模板（与 v2.sh 的 SUDO_PASS 语义一致）：printf 管道喂 stdin，
	// stderr 合并进输出（sudo 报错如「密码错误/不在 sudoers」直接可见）
	sudo := func(inner string) string {
		return fmt.Sprintf("printf '%%s\\n' myt | sudo -S -p '' sh -c %q 2>&1", inner)
	}
	if _, err := RunSSHCmd(sshClient, fmt.Sprintf("test -x %s/v2.sh", binDir)); err == nil {
		if _, err := RunSSHCmd(sshClient, fmt.Sprintf(
			"cd %s && %s", binDir, sudo("./v2.sh restart"))); err != nil {
			return fmt.Errorf("v2.sh 重启失败（cd %s）: %w", binDir, err)
		}
	} else {
		// 无 v2.sh：先杀旧进程再 nohup 拉起。杀按探测到的 PID 精确杀（busybox
		// pkill 只支持 -f，旧二进制名非约定名时按名匹配会杀漏、8000 仍被占）；
		// 无探测 PID（新装）或 v2.sh 路径时按名 pkill 兜底，无匹配返回 1 正常
		// （|| true 容忍，不影响后续拉起）。
		killByPID := ""
		if oldPid != "" {
			killByPID = fmt.Sprintf("printf '%%s\\n' myt | sudo -S -p '' kill %s 2>/dev/null; ", oldPid)
		}
		killCmd := killByPID + fmt.Sprintf(
			"printf '%%s\\n' myt | sudo -S -p '' pkill -f '(^|/)%s$' 2>/dev/null || true", binName)
		if _, err := RunSSHCmd(sshClient, killCmd); err != nil {
			log.Printf("[v2sdk] %s 杀旧进程失败（不阻断）: %v", deviceIP, err)
		}
		// nohup 后台拉起：sh -c 里 & 后台化后 sudo/sh 立即返回；新进程 stdout
		// 落 v2.log。启动失败时 15s 后的 /info 验证会兜底报错。
		startCmd := fmt.Sprintf(
			"cd %s && %s", binDir, sudo(fmt.Sprintf("nohup ./%s >> v2.log 2>&1 &", binName)))
		if _, err := RunSSHCmd(sshClient, startCmd); err != nil {
			return fmt.Errorf("启动失败（cd %s && nohup ./%s）: %w", binDir, binName, err)
		}
	}

	// 验证用 checkV2SdkHasInfo（要求 /info 可解析）而非 CheckV2SdkAlive——
	// 老版本场景 8000 可能活着但没 /info，升级必须确认新接口真正可用。
	// 窗口 60s：实测 10.10.2.46（eMMC 上 20MB Go 二进制冷启动）进程拉起后
	// 约 18s 才开始监听 8000——15s 窗口会把成功部署误判为失败（2026-10-14 实测）。
	progress("verify_run", "验证服务")
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if checkV2SdkHasInfo(deviceIP) {
			progress("verify_run", "SDK 已在 8000 端口就绪（/info 可用）")
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("SDK 启动后 60s 内 8000 端口仍未就绪（详见设备 %s/v2.log）", v2SdkRemoteDir)
}

// EnsureV2SdkUpgraded 保证设备 8000 上跑的是带 /info 接口的 myt-sdk-go-v2：
//   - /info 可用 → 返回 nil（不升级；版本是否最新交给现有显示/安装链路）
//   - /info 不可用（老版本 SDK 无该接口、8000 跑别的服务或未监听）→ 走 SSH
//     部署链路强制升级到最新版（不经过 DeployV2Sdk 的 CheckV2SdkAlive 幂等
//     门卫——8000 活着但没 /info 时它会把老版本误判为已部署而跳过）。
//
// 覆盖二进制 + 重启对设备状态无前置要求；老环境无 v2.sh 时走 sudo nohup 兜底
// （DeployV2SdkVersion 内已有该分支）。
func EnsureV2SdkUpgraded(deviceIP string, progress v2SdkProgress) error {
	if checkV2SdkHasInfo(deviceIP) {
		return nil
	}
	log.Printf("[v2sdk] %s 8000 无可用 /info 接口，自动升级 SDK 到最新版", deviceIP)
	return DeployV2SdkVersion(deviceIP, progress)
}
