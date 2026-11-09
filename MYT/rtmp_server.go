package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/q191201771/lal/pkg/logic"
)

// rtmpServer 管理 LAL/SRS 流媒体服务器生命周期
type rtmpServer struct {
	lalServer logic.ILalServer
	useSrs    bool
	srsCmd    *exec.Cmd
}

// startRtmpServer 启动流媒体服务器（优先 SRS，降级 LAL）
func (r *rtmpServer) start() {
	startedSrs, err := r.tryStartSrs()
	if err != nil {
		fmt.Printf("[stream] SRS start failed: %v\n", err)
	}
	if startedSrs {
		r.useSrs = true
		fmt.Println("[stream] SRS Server started on :1935, WebRTC/HTTP on :1985")
		return
	}

	r.useSrs = false
	r.startLal()
}

// stopRtmpServer 停止流媒体服务器
func (r *rtmpServer) stop() {
	if r.lalServer != nil {
		r.lalServer.Dispose()
	}
	if r.srsCmd != nil && r.srsCmd.Process != nil {
		_ = r.srsCmd.Process.Kill()
	}
}

func (r *rtmpServer) tryStartSrs() (bool, error) {
	if r.isSrsApiAvailable() {
		return true, nil
	}
	bin := os.Getenv("MYT_SRS_BIN")
	if bin == "" {
		bin = os.Getenv("SRS_BIN")
	}
	if bin == "" {
		bin = "srs"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return false, nil
	}
	cmd := exec.Command(path, "-c", "conf/rtc.conf")
	if workDir := os.Getenv("MYT_SRS_HOME"); workDir != "" {
		cmd.Dir = workDir
	} else if workDir := os.Getenv("SRS_HOME"); workDir != "" {
		cmd.Dir = workDir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return false, err
	}
	r.srsCmd = cmd
	for i := 0; i < 10; i++ {
		time.Sleep(300 * time.Millisecond)
		if r.isSrsApiAvailable() {
			return true, nil
		}
	}
	return false, fmt.Errorf("SRS API not available")
}

func (r *rtmpServer) isSrsApiAvailable() bool {
	client := &http.Client{Timeout: 800 * time.Millisecond}
	resp, err := client.Get("http://127.0.0.1:1985/api/v1/versions")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (r *rtmpServer) startLal() {
	// lalserver 默认 log.filename="./logs/lalserver.log"（相对路径）。
	// open 启动时 cwd 为只读路径（/ 或 /Applications/...），mkdir logs 失败 →
	// lalserver OsExitAndWait(1) → 整个进程退出（闪退）。必须用绝对路径。
	logFile := "lalserver.log"
	if dir := getDarwinDataDir(); dir != "" {
		logFile = filepath.Join(dir, "logs", "lalserver.log")
	} else if exe, err := os.Executable(); err == nil {
		logFile = filepath.Join(filepath.Dir(exe), "logs", "lalserver.log")
	}
	// JSON 字符串拼接：反斜杠路径会导致非法转义（\U \D 等），统一用正斜杠
	logFile = filepath.ToSlash(logFile)
	conf := `{
		"rtmp": { "enable": true, "addr": ":1935", "gop_num": 1 },
		"default_http": { "http_listen_addr": ":8083" },
		"httpflv": { "enable": true, "enable_gop_cache": false },
		"http_api": { "enable": true, "addr": ":8084" },
		"rtsp": { "enable": true, "addr": ":554" },
		"webrtc": { "enable": true },
		"hls": { "enable": false },
		"log": { "filename": "` + logFile + `" }
	}`
	modOption := func(option *logic.Option) {
		option.ConfRawContent = []byte(conf)
	}
	l := logic.NewLalServer(modOption)
	r.lalServer = l
	go func() {
		if err := l.RunLoop(); err != nil {
			fmt.Printf("[stream] LAL Server error: %v\n", err)
		}
	}()
	fmt.Println("[stream] RTMP Server started on :1935, WebRTC/HTTP on :8083, RTSP on :554, API on :8084")
	startProxy1985()
}

// startProxy1985 启动 1985 端口代理转发到 LAL 的 8083（WebRTC 信令兼容）
func startProxy1985() {
	targetBase := "http://127.0.0.1:8083"
	client := &http.Client{Timeout: 5 * time.Second}
	go func() {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestBody, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			resp, _, err := forwardToLal(client, targetBase, r, requestBody, r.URL.Path)
			if err == nil && resp.StatusCode == http.StatusNotFound {
				altPath := mapRtcPath(r.URL.Path)
				if altPath != "" && altPath != r.URL.Path {
					_ = resp.Body.Close()
					resp, _, err = forwardToLal(client, targetBase, r, requestBody, altPath)
				}
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			defer resp.Body.Close()
			respBody, _ := io.ReadAll(resp.Body)
			for key, values := range resp.Header {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
			w.WriteHeader(resp.StatusCode)
			_, _ = w.Write(respBody)
		})
		_ = http.ListenAndServe(":1985", handler)
	}()
}

func forwardToLal(client *http.Client, targetBase string, r *http.Request, body []byte, path string) (*http.Response, string, error) {
	targetURL := targetBase + path
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, targetURL, err
	}
	for key, values := range r.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	req.Host = strings.TrimPrefix(targetBase, "http://")
	resp, err := client.Do(req)
	if err != nil {
		return nil, targetURL, err
	}
	return resp, targetURL, nil
}

func mapRtcPath(path string) string {
	if strings.HasPrefix(path, "/rtc/v1/whep") {
		return strings.Replace(path, "/rtc/v1/whep", "/webrtc/v1/play", 1)
	}
	if strings.HasPrefix(path, "/rtc/v1/play") {
		return strings.Replace(path, "/rtc/v1/play", "/webrtc/v1/play", 1)
	}
	if strings.HasPrefix(path, "/rtc/v1/whip") {
		return strings.Replace(path, "/rtc/v1/whip", "/webrtc/v1/publish", 1)
	}
	return ""
}

// deviceAddr 补全设备地址（含端口检查）
func streamDeviceAddr(deviceIP string) string {
	if strings.Contains(deviceIP, ":") {
		return deviceIP
	}
	return deviceIP + ":8000"
}

// httpGetToDevice 对设备发送 HTTP GET 请求
func httpGetToDevice(url string) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Printf("[stream] GET %s failed: %v\n", url, err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
}
