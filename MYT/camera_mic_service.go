//go:build windows

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/moyunteng/myt/internal/config"
)

//go:embed camdemo/ffmpeg_minimal/bin/ffmpeg.exe
//go:embed camdemo/ffmpeg_minimal/bin/libx264-165.dll
//go:embed camdemo/ffmpeg_minimal/bin/libgcc_s_seh-1.dll
//go:embed camdemo/ffmpeg_minimal/bin/libwinpthread-1.dll
var ffmpegMinAssets embed.FS

const ffmpegAssetDir = "camdemo/ffmpeg_minimal/bin"

// CameraMicSession 摄像头+麦克风推流会话
type CameraMicSession struct {
	ID         int
	DeviceIP   string
	IndexNum   int
	StreamName string
	CamName    string
	MicName    string
	Cmd        *exec.Cmd
	Running    bool
	StartedAt  time.Time
}

// CameraMicServiceWrapper 摄像头+麦克风推流服务（Wails Service）
type CameraMicServiceWrapper struct {
	mu       sync.Mutex
	sessions map[int]*CameraMicSession
	nextID   int
	cm       *config.Manager

	exeOnce sync.Once
	exeDir  string
	exeErr  error
}

// NewCameraMicServiceWrapper 创建摄像头+麦克风推流服务
func NewCameraMicServiceWrapper() *CameraMicServiceWrapper {
	return &CameraMicServiceWrapper{
		sessions: make(map[int]*CameraMicSession),
	}
}

// SetConfigManager 注入配置管理器
func (s *CameraMicServiceWrapper) SetConfigManager(cm *config.Manager) {
	s.cm = cm
}

// ensureExe 提取嵌入的精简 ffmpeg.exe 及依赖 DLL 到临时目录
func (s *CameraMicServiceWrapper) ensureExe() (string, error) {
	s.exeOnce.Do(func() {
		dir := filepath.Join(os.TempDir(), "myt-ffmpeg")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			s.exeErr = fmt.Errorf("create ffmpeg temp dir: %w", err)
			return
		}
		if err := extractFfmpegAssets(dir); err != nil {
			s.exeErr = err
			return
		}
		s.exeDir = dir
		fmt.Printf("[camera-mic] ffmpeg extracted to %s\n", dir)
	})
	if s.exeErr != nil {
		return "", s.exeErr
	}
	return filepath.Join(s.exeDir, "ffmpeg.exe"), nil
}

func extractFfmpegAssets(destDir string) error {
	return fs.WalkDir(ffmpegMinAssets, ffmpegAssetDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(ffmpegAssetDir, filepath.FromSlash(path))
		if strings.HasSuffix(rel, ".gitkeep") {
			return nil
		}
		data, err := ffmpegMinAssets.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embed %s: %w", path, err)
		}
		if len(data) == 0 {
			return fmt.Errorf("embedded file %s is empty", path)
		}
		want := sha256.Sum256(data)
		dest := filepath.Join(destDir, rel)
		if existing, err := os.ReadFile(dest); err == nil {
			if sha256.Sum256(existing) == want {
				return nil
			}
		}
		return os.WriteFile(dest, data, 0o755)
	})
}

// StartCameraMic 启动摄像头+麦克风推流到指定设备
func (s *CameraMicServiceWrapper) StartCameraMic(deviceIP string, indexNum int, camName, micName string) map[string]interface{} {
	if s.cm != nil && s.cm.GetDeployMode() == "public" {
		return map[string]interface{}{"success": false, "message": "公网模式不支持摄像头推流"}
	}
	if deviceIP == "" || indexNum < 0 {
		return map[string]interface{}{"success": false, "message": "设备信息不完整"}
	}

	// 检查是否已有同设备会话
	s.mu.Lock()
	for _, sess := range s.sessions {
		if sess.DeviceIP == deviceIP && sess.IndexNum == indexNum && sess.Running {
			s.mu.Unlock()
			return map[string]interface{}{"success": true, "id": sess.ID, "message": "已在推流中"}
		}
	}
	s.mu.Unlock()

	exePath, err := s.ensureExe()
	if err != nil {
		return map[string]interface{}{"success": false, "message": "ffmpeg 初始化失败: " + err.Error()}
	}

	streamName := fmt.Sprintf("cam_%s_%d", strings.ReplaceAll(deviceIP, ".", "_"), indexNum)
	rtmpURL := fmt.Sprintf("rtmp://127.0.0.1:1935/live/%s", streamName)

	// 构造 ffmpeg 参数
	args := []string{
		"-y", "-rtbufsize", "100M",
		"-f", "dshow", "-framerate", "30", "-video_size", "1280x720",
		"-i", fmt.Sprintf("video=%s", camName),
		"-f", "dshow",
		"-i", fmt.Sprintf("audio=%s", micName),
		"-vcodec", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
		"-pix_fmt", "yuv420p", "-g", "30",
		"-acodec", "aac", "-ar", "44100", "-ac", "1", "-b:a", "64k",
		"-f", "flv", rtmpURL,
	}

	cmd := exec.Command(exePath, args...)
	cmd.Dir = filepath.Dir(exePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return map[string]interface{}{"success": false, "message": "ffmpeg 启动失败: " + err.Error()}
	}

	// 等 3 秒确认进程存活（RTMP 握手+首帧编码需要时间）
	time.Sleep(3 * time.Second)
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return map[string]interface{}{"success": false, "message": "ffmpeg 异常退出: " + strings.TrimSpace(stderr.String())}
	}

	s.mu.Lock()
	id := s.nextID
	s.nextID++
	s.sessions[id] = &CameraMicSession{
		ID: id, DeviceIP: deviceIP, IndexNum: indexNum,
		StreamName: streamName, CamName: camName, MicName: micName,
		Cmd: cmd, Running: true, StartedAt: time.Now(),
	}
	s.mu.Unlock()

	// 推流到设备
	go s.pushToDevice(deviceIP, indexNum, streamName)

	// 监控进程退出
	go func() {
		_ = cmd.Wait()
		s.mu.Lock()
		if sess, ok := s.sessions[id]; ok {
			sess.Running = false
		}
		s.mu.Unlock()
		camMicLogf("session %d ffmpeg exited", id)
	}()

	return map[string]interface{}{"success": true, "id": id, "streamName": streamName, "message": "摄像头+麦克风推流已启动"}
}

// pushToDevice 将流推送到设备虚拟摄像头
func (s *CameraMicServiceWrapper) pushToDevice(deviceIP string, indexNum int, streamName string) {
	localIP := getLocalIP()
	if localIP == "" {
		camMicLogf("无法获取本机IP，推流到设备失败")
		return
	}
	httpflvURL := fmt.Sprintf("http://%s:8083/live/%s.flv", localIP, streamName)
	deviceAddr := streamDeviceAddr(deviceIP)

	// 设置虚拟摄像头源
	setURL := fmt.Sprintf("http://%s/modifydev?cmd=4&type=webrtc&path=%s&resolution=1", deviceAddr, httpflvURL)
	httpGetToDevice(setURL)
	camMicLogf("set camera source: %s", setURL)

	// 启动虚拟摄像头
	startURL := fmt.Sprintf("http://%s/camera?cmd=start", deviceAddr)
	httpGetToDevice(startURL)
	camMicLogf("camera started: %s", startURL)
}

// StopCameraMic 停止指定会话
func (s *CameraMicServiceWrapper) StopCameraMic(id int) map[string]interface{} {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok || sess == nil {
		return map[string]interface{}{"success": true, "message": "会话不存在"}
	}

	// 停止 ffmpeg
	if sess.Cmd != nil && sess.Cmd.Process != nil {
		_ = sess.Cmd.Process.Kill()
	}

	// 停止设备虚拟摄像头
	deviceAddr := streamDeviceAddr(sess.DeviceIP)
	stopURL := fmt.Sprintf("http://%s/camera?cmd=stop", deviceAddr)
	httpGetToDevice(stopURL)

	s.mu.Lock()
	sess.Running = false
	delete(s.sessions, id)
	s.mu.Unlock()

	return map[string]interface{}{"success": true, "message": "已停止"}
}

// StopAll 停止所有会话（退出时调用）
func (s *CameraMicServiceWrapper) StopAll() {
	s.mu.Lock()
	count := 0
	for id, sess := range s.sessions {
		count++
		if sess.Cmd != nil && sess.Cmd.Process != nil {
			_ = sess.Cmd.Process.Kill()
		}
		deviceAddr := streamDeviceAddr(sess.DeviceIP)
		stopURL := fmt.Sprintf("http://%s/camera?cmd=stop", deviceAddr)
		go httpGetToDevice(stopURL)
		delete(s.sessions, id)
	}
	s.mu.Unlock()
	camMicLogf("stopped %d sessions", count)
}

// ListCameraDevices 列出可用摄像头和麦克风设备
func (s *CameraMicServiceWrapper) ListCameraDevices() map[string]interface{} {
	cameras, mics, err := s.listDshowDevices()
	if err != nil {
		return map[string]interface{}{"success": false, "message": err.Error(), "cameras": []string{}, "mics": []string{}}
	}
	return map[string]interface{}{"success": true, "cameras": cameras, "mics": mics}
}

// listDshowDevices 调用 ffmpeg 列举 DirectShow 设备
func (s *CameraMicServiceWrapper) listDshowDevices() (cameras, mics []string, err error) {
	exePath, err := s.ensureExe()
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exePath, "-list_devices", "true", "-f", "dshow", "-i", "dummy")
	cmd.Dir = filepath.Dir(exePath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	_ = cmd.Run()

	// ffmpeg 输出到 stderr，格式：[dshow @ xxx] "Device Name" (video|audio)
	scanner := bufio.NewScanner(&stderr)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	re := regexp.MustCompile(`[dshow @ [^]]*] "(.+?)" ((video|audio))`)
	for scanner.Scan() {
		matches := re.FindStringSubmatch(scanner.Text())
		if len(matches) == 3 {
			name := matches[1]
			if matches[2] == "video" {
				cameras = append(cameras, name)
			} else {
				mics = append(mics, name)
			}
		}
	}
	return cameras, mics, nil
}

// IsCameraMicRunning 查询指定设备的推流状态
func (s *CameraMicServiceWrapper) IsCameraMicRunning(deviceIP string, indexNum int) map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		if sess.DeviceIP == deviceIP && sess.IndexNum == indexNum {
			return map[string]interface{}{"running": sess.Running, "id": sess.ID, "camName": sess.CamName, "micName": sess.MicName}
		}
	}
	return map[string]interface{}{"running": false}
}

// camMicLogf 摄像头推流日志
func camMicLogf(format string, args ...interface{}) {
	fmt.Printf("[camera-mic] "+format+"\n", args...)
}
