package maintenance

// teboxsdk.go 的基础工具函数（独立小文件，便于单测）。
// 命名避让既有工具：httpGetFast / sshExecStreaming / sftpWriteBytes /
// tempFilePath / osRemove 在 maintenance 包内均无同名先例。

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// httpResp 简化 HTTP 响应（Body 已读为字符串，Body 字段是 LimitReader 限制下的小体）
type httpResp struct {
	StatusCode int
	Body       string
}

// httpGetFast 快速 GET（2s 超时 + 不走系统代理——内网直连必须 Proxy:nil，
// 与 v4host.probeTransport 同因：系统代理会让局域网探测全部失败）。
func httpGetFast(url string) (*httpResp, error) {
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		},
	}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, err
	}
	return &httpResp{StatusCode: resp.StatusCode, Body: string(body)}, nil
}

// sshExecStreaming SSH 执行长命令并逐行流式回调输出（install-sdk.sh 的
// [N/8] 步骤输出透传给前端进度）。整体超时兜底（挂死强制返回）。
func sshExecStreaming(client *ssh.Client, cmd string, timeout time.Duration, onLine func(line string)) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("创建SSH session失败: %w", err)
	}
	defer session.Close()

	pipe, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("获取输出管道失败: %w", err)
	}
	// 合并 stderr（脚本用 2>&1 执行时已合并，这里再兜一层）
	session.Stderr = session.Stdout

	done := make(chan error, 1)
	go func() { done <- session.Run(cmd) }()

	// 读输出 goroutine：逐行回调；session.Run 返回后 EOF 自然退出
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		scanner := bufio.NewScanner(pipe)
		scanner.Buffer(make([]byte, 0, 64*1024), 256*1024) // 长行容忍（安装摘要）
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			if onLine != nil {
				onLine(line)
			}
		}
	}()

	select {
	case err := <-done:
		<-readerDone
		if err != nil {
			return fmt.Errorf("执行命令失败: %w", err)
		}
		return nil
	case <-time.After(timeout):
		session.Close() // 中断会话（读 goroutine 随 EOF 退出）
		return fmt.Errorf("安装命令超时（%d 分钟）", int(timeout.Minutes()))
	}
}

// sftpWriteBytes 写字节内容到远端文件（上传内嵌脚本用）。
func sftpWriteBytes(sftpClient *sftp.Client, content []byte, remotePath string, perm os.FileMode) error {
	f, err := sftpClient.Create(remotePath)
	if err != nil {
		return fmt.Errorf("创建远程文件失败 %s: %w", remotePath, err)
	}
	defer f.Close()
	if _, err := f.Write(content); err != nil {
		return fmt.Errorf("写入远程文件失败 %s: %w", remotePath, err)
	}
	sftpClient.Chmod(remotePath, perm)
	return nil
}

// tempFilePath 生成临时目录下的带版本文件名的路径（tebox-sdk-2.zip 风格）。
func tempFilePath(prefix, version, ext string) string {
	name := fmt.Sprintf("%s-%s%s", prefix, version, ext)
	return filepath.Join(os.TempDir(), name)
}

// osRemove 忽略错误的 os.Remove（defer 清理用）。
func osRemove(path string) { _ = os.Remove(path) }
