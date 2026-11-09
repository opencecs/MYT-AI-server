package workflow

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/crypto/ssh"
)

const maxOutputSize = 4 * 1024 // 4KB 输出截断

// limitWriter 限制写入字节数的 io.Writer
type limitWriter struct {
	buf   []byte
	limit int
}

func (w *limitWriter) Write(p []byte) (int, error) {
	remain := w.limit - len(w.buf)
	if remain <= 0 {
		return len(p), nil
	}
	if len(p) > remain {
		p = p[:remain]
	}
	w.buf = append(w.buf, p...)
	return len(p), nil
}

func (w *limitWriter) String() string { return string(w.buf) }
func (w *limitWriter) len() int        { return len(w.buf) }

// SSHExec 通过 SSH 执行命令
func SSHExec(ctx context.Context, host string, port int, user, password, command string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}

	addr := fmt.Sprintf("%s:%d", host, port)

	// 带超时的连接
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var client *ssh.Client
	var connErr error
	done := make(chan struct{})
	go func() {
		client, connErr = ssh.Dial("tcp", addr, config)
		close(done)
	}()

	select {
	case <-dialCtx.Done():
		return "", fmt.Errorf("SSH 连接超时: %s", addr)
	case <-done:
		if connErr != nil {
			return "", fmt.Errorf("SSH 连接失败: %w", connErr)
		}
	}
	defer client.Close()

	// 创建 session
	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("SSH 创建会话失败: %w", err)
	}
	defer session.Close()

	// 执行命令，限制输出
	var stdout, stderr limitWriter
	stdout.limit = maxOutputSize
	stderr.limit = 1024
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Start(command); err != nil {
		return "", fmt.Errorf("SSH 执行失败: %w", err)
	}

	// 等待命令完成或超时
	done = make(chan struct{})
	var waitErr error
	go func() {
		waitErr = session.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("SSH 命令被取消")
	case <-done:
		// 命令完成
	}

	if waitErr != nil && stdout.len() == 0 {
		return "", fmt.Errorf("SSH 命令执行错误: %w (stderr: %s)", waitErr, stderr.String())
	}

	result := stdout.String()
	if len(result) == 0 && stderr.len() > 0 {
		result = stderr.String()
	}

	return result, nil
}
