package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/mytos"
	"github.com/moyunteng/myt/internal/ports"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// FileService 文件管理服务（双栏：云机 + 本地）
type FileService struct {
	cm        *config.Manager
	mirrorSvc *MirrorServiceWrapper
	sdkApiSvc *SdkApiServiceWrapper
	app       *application.App
}

// NewFileService 创建文件管理服务
func NewFileService(cm *config.Manager) *FileService {
	return &FileService{cm: cm}
}

// SetMirrorService 注入镜像服务（用于按镜像类型分派 mytos 客户端）
func (f *FileService) SetMirrorService(ms *MirrorServiceWrapper) { f.mirrorSvc = ms }

// SetSdkApiService 注入 SDK API 服务（用于公网模式客户端创建）
func (f *FileService) SetSdkApiService(svc *SdkApiServiceWrapper) { f.sdkApiSvc = svc }

// SetApp 设置应用实例（用于事件推送）
func (f *FileService) SetApp(app *application.App) { f.app = app }

// emitEvent 向前端发送事件
func (f *FileService) emitEvent(name string, data any) {
	if f.app != nil && f.app.Event != nil {
		f.app.Event.Emit(name, data)
	}
}

// --- 云机文件操作 ---

// ListCloudFiles 获取云机指定目录下的文件列表
func (f *FileService) ListCloudFiles(deviceIP, androidName, dirPath string) ([]mytos.CloudFileEntry, error) {
	client, err := f.getMytosClient(deviceIP, androidName)
	if err != nil {
		return nil, err
	}
	entries, err := client.ListFiles(dirPath)
	if err != nil {
		return nil, err
	}
	// 排序：文件夹在前，文件在后，各自按名称排序
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Flag != entries[j].Flag {
			return entries[i].Flag
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

// DownloadCloudFile 从云机下载文件到本地目录
func (f *FileService) DownloadCloudFile(deviceIP, androidName, cloudFilePath, saveDir string) (string, error) {
	client, err := f.getMytosClient(deviceIP, androidName)
	if err != nil {
		return "", err
	}

	data, err := client.Download(cloudFilePath)
	if err != nil {
		return "", fmt.Errorf("下载失败: %w", err)
	}

	// 确保 saveDir 存在
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		return "", fmt.Errorf("创建保存目录失败: %w", err)
	}

	fileName := filepath.Base(cloudFilePath)
	if fileName == "" || fileName == "." || fileName == "/" {
		fileName = "downloaded_file"
	}
	savePath := filepath.Join(saveDir, fileName)

	// 如果文件已存在，添加后缀
	if _, err := os.Stat(savePath); err == nil {
		ext := filepath.Ext(fileName)
		base := strings.TrimSuffix(fileName, ext)
		for i := 1; ; i++ {
			savePath = filepath.Join(saveDir, fmt.Sprintf("%s_%d%s", base, i, ext))
			if _, err := os.Stat(savePath); err != nil {
				break
			}
		}
	}

	if err := os.WriteFile(savePath, data, 0644); err != nil {
		return "", fmt.Errorf("保存文件失败: %w", err)
	}
	return savePath, nil
}

// UploadToCloud 上传本地文件到云机
// .apks/.xapk 文件走 /installapks 批量安装接口
// .apk 文件走 /upload 后再 pm install 安装（设备端无 /installapk 接口）
// 其他文件走 /upload
// autoGrantPermissions：pm install 追加 -g（自动授予所有运行时权限），
// 仅对 .apk 生效（/installapks 不支持 -g），默认 false 维持现状。
// targetDir：普通文件的目标目录（文件管理器「浏览到哪传到哪」）。设备端 /upload
// 不支持目标路径参数（multipart 仅 file 字段，固定落 /sdcard/upload），故非空时
// 上传后经容器内 mkdir -p + mv -f 挪到目标目录（覆盖同名），失败清理 upload 残留。
// 空值保持旧行为（落 /sdcard/upload）；apk/apks/xapk 为安装链，忽略 targetDir。
func (f *FileService) UploadToCloud(deviceIP, androidName, localFilePath string, autoGrantPermissions bool, targetDir string) (string, error) {
	client, err := f.getMytosClient(deviceIP, androidName)
	if err != nil {
		return "", err
	}

	ext := strings.ToLower(filepath.Ext(localFilePath))

	// 进度回调：上传阶段 0-80%，安装阶段 80-100%
	onUploadProgress := func(uploaded, total int64) {
		var pct int
		if total > 0 {
			pct = int(uploaded * 80 / total)
		}
		f.emitEvent("file:upload-progress", map[string]any{
			"deviceIP":    deviceIP,
			"androidName": androidName,
			"fileName":    filepath.Base(localFilePath),
			"progress":    pct,
			"stage":       "uploading",
			"uploaded":    uploaded,
			"totalBytes":  total,
		})
	}

	if ext == ".apks" || ext == ".xapk" {
		// /installapks 同步完成上传+安装：上传阶段经进度回调透出（复用 onUploadProgress 的 0-80% 映射），
		// 字节传完后设备端开始安装（无进度），发一条 installing 事件提示安装中
		installingNotified := false
		onApksProgress := func(uploaded, total int64) {
			onUploadProgress(uploaded, total)
			if !installingNotified && total > 0 && uploaded >= total {
				installingNotified = true
				f.emitFileProgress(deviceIP, androidName, filepath.Base(localFilePath), 80, "installing")
			}
		}
		if err := client.InstallApks(localFilePath, onApksProgress); err != nil {
			return "", fmt.Errorf("批量安装失败: %w", err)
		}
		f.emitFileProgress(deviceIP, androidName, filepath.Base(localFilePath), 100, "done")
		return "安装完成", nil
	}

	remotePath, err := client.UploadFileWithProgress(localFilePath, onUploadProgress)
	if err != nil {
		return "", fmt.Errorf("上传失败: %w", err)
	}

	// 普通文件挪到目标目录（apk/apks/xapk 是安装链，不落用户目录，忽略 targetDir）
	if targetDir != "" && ext != ".apk" && ext != ".apks" && ext != ".xapk" {
		if err := f.moveToTargetDir(deviceIP, androidName, remotePath, targetDir); err != nil {
			return remotePath, err
		}
		remotePath = joinPosixPath(targetDir, filepath.Base(localFilePath))
	}

	// APK 上传后同步安装（pm install 耗时较长，前端通过任务队列异步等待）
	if ext == ".apk" {
		f.emitEvent("file:upload-progress", map[string]any{
			"deviceIP":    deviceIP,
			"androidName": androidName,
			"fileName":    filepath.Base(localFilePath),
			"progress":    80,
			"stage":       "installing",
		})

		sdkClient := f.sdkApiSvc.newClientWithTimeout(deviceIP, 5*time.Minute)
		fileName := filepath.Base(localFilePath)
		installPath := fmt.Sprintf("/data/local/tmp/%s", fileName)

		// 复制到临时安装目录
		if _, err := sdkClient.DockerExec(androidName, fmt.Sprintf("cp '%s' '%s'", remotePath, installPath)); err != nil {
			return remotePath, fmt.Errorf("文件已上传但安装准备失败: %w", err)
		}

		// pm install 同步执行：-r 覆盖安装，-t 允许测试包；勾选自动授权时追加 -g
		// （默认不带，按 Android 标准由 App 运行时按需申请，避免过度授权）。
		installCmd := "pm install -r -t"
		if autoGrantPermissions {
			installCmd = "pm install -r -t -g"
		}
		result, err := sdkClient.DockerExec(androidName, fmt.Sprintf("%s '%s'", installCmd, installPath))
		// 清理临时文件
		_, _ = sdkClient.DockerExec(androidName, fmt.Sprintf("rm -f '%s'", installPath))
		if err != nil {
			return remotePath, fmt.Errorf("APK安装失败: %w", err)
		}
		// 安装结果判定：pm install 成功时输出 "Success"。只查 "Failure" 会放过其他形式的失败
		// （Error/空输出/模拟器非标准输出）→ 静默假成功（任务完成但应用没装上）。
		// 输出原样进日志，便于对模拟器等非标准镜像取证。
		out := strings.TrimSpace(result.Output)
		log.Printf("[APK安装] %s → %s: exit=%d output=%q", fileName, androidName, result.ExitCode, out)
		if !strings.Contains(out, "Success") {
			return remotePath, fmt.Errorf("APK安装失败: %s", out)
		}
		log.Printf("[APK安装] 安装成功: %s", fileName)

		f.emitEvent("file:upload-progress", map[string]any{
			"deviceIP":    deviceIP,
			"androidName": androidName,
			"fileName":    filepath.Base(localFilePath),
			"progress":    100,
			"stage":       "done",
		})
	}

	// 非安装文件（非 apk/apks/xapk）上传完成后发送 done 事件，推动前端进度拉满
	if ext != ".apk" && ext != ".apks" && ext != ".xapk" {
		f.emitEvent("file:upload-progress", map[string]any{
			"deviceIP":    deviceIP,
			"androidName": androidName,
			"fileName":    filepath.Base(localFilePath),
			"progress":    100,
			"stage":       "done",
		})
	}

	return remotePath, nil
}

// joinPosixPath 拼接设备端 POSIX 路径：统一去掉尾斜杠后以 / 相接，避免出现 // 或缺失分隔符
func joinPosixPath(dir, name string) string {
	return strings.TrimSuffix(dir, "/") + "/" + name
}

// shellQuote POSIX shell 单引号包裹转义：' → '\''（闭合引号 + 转义引号 + 重开引号），
// 使文件名中的单引号/空格等元字符安全嵌入 sh -c 命令串。存量 cp '%s' 风格调用点未改。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// moveToTargetDir 把已上传到 /sdcard/upload 的文件挪到云机目标目录。
// 走容器内 sd shell：mkdir -p（目标目录可能不存在，幂等）+ mv -f（覆盖同名）。
// 成功判定用输出标记（对齐 APK 链 pm install 查 "Success" 的模式——不查输出会
// 放过 mv 静默失败）。失败时清理 upload 残留，避免垃圾留在上传目录。
func (f *FileService) moveToTargetDir(deviceIP, androidName, uploadPath, targetDir string) error {
	sdkClient := f.sdkApiSvc.newClientWithTimeout(deviceIP, 30*time.Second)
	name := filepath.Base(uploadPath)
	target := joinPosixPath(targetDir, name)

	cmd := fmt.Sprintf("mkdir -p %s && mv -f %s %s && echo MYT_MV_OK",
		shellQuote(targetDir), shellQuote(uploadPath), shellQuote(target))
	result, err := sdkClient.DockerExec(androidName, cmd)
	if err != nil {
		// mv 未执行：清 upload 残留（rm -f 不关注结果，best effort）
		_, _ = sdkClient.DockerExec(androidName, fmt.Sprintf("rm -f %s", shellQuote(uploadPath)))
		return fmt.Errorf("移动文件到 %s 失败: %w", targetDir, err)
	}
	if !strings.Contains(strings.TrimSpace(result.Output), "MYT_MV_OK") {
		// mv 执行了但失败：目标可能有半成品也一并清（rm -f 两条都 best effort）
		_, _ = sdkClient.DockerExec(androidName, fmt.Sprintf("rm -f %s %s", shellQuote(uploadPath), shellQuote(target)))
		return fmt.Errorf("移动文件到 %s 失败: exit=%d output=%q", targetDir, result.ExitCode, strings.TrimSpace(result.Output))
	}
	return nil
}

// DownloadCloudFolder 递归下载云机文件夹到本地（返回下载的文件数）
func (f *FileService) DownloadCloudFolder(deviceIP, androidName, cloudDirPath, localSaveDir string) (int, error) {
	client, err := f.getMytosClient(deviceIP, androidName)
	if err != nil {
		return 0, err
	}

	if err := os.MkdirAll(localSaveDir, 0755); err != nil {
		return 0, fmt.Errorf("创建本地目录失败: %w", err)
	}

	var count int
	var recurse func(remoteDir, localDir string) error
	recurse = func(remoteDir, localDir string) error {
		entries, err := client.ListFiles(remoteDir)
		if err != nil {
			return fmt.Errorf("列出 %s 失败: %w", remoteDir, err)
		}
		for _, entry := range entries {
			localPath := filepath.Join(localDir, entry.Name)
			if entry.Flag {
				if err := os.MkdirAll(localPath, 0755); err != nil {
					continue
				}
				if err := recurse(entry.File, localPath); err != nil {
					continue
				}
			} else {
				if err := client.DownloadToFile(entry.File, localPath); err != nil {
					continue
				}
				count++
			}
		}
		return nil
	}

	if err := recurse(cloudDirPath, localSaveDir); err != nil {
		return count, err
	}
	return count, nil
}

// --- 本地文件操作 ---

// LocalFileEntry 本地文件条目
type LocalFileEntry struct {
	Name    string `json:"name"`    // 文件名
	Path    string `json:"path"`    // 完整路径
	IsDir   bool   `json:"isDir"`   // 是否文件夹
	Size    int64  `json:"size"`    // 文件大小(字节)
	ModTime int64  `json:"modTime"` // 修改时间(Unix秒)
}

// ListLocalDir 列出本地目录下的文件
func (f *FileService) ListLocalDir(dirPath string) ([]LocalFileEntry, error) {
	if dirPath == "" {
		// 默认：用户主目录
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("获取主目录失败: %w", err)
		}
		dirPath = home
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("读取目录失败: %w", err)
	}

	var files []LocalFileEntry
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue // 跳过无权限等异常
		}
		files = append(files, LocalFileEntry{
			Name:    entry.Name(),
			Path:    filepath.Join(dirPath, entry.Name()),
			IsDir:   entry.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
		})
	}

	// 排序：文件夹在前，文件在后，各自按名称排序
	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})
	return files, nil
}

// GetDefaultLocalDir 获取默认本地目录（用户主目录）
func (f *FileService) GetDefaultLocalDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		if runtime.GOOS == "windows" {
			return "C:\\Users"
		}
		return os.Getenv("HOME")
	}
	return home
}

// --- 辅助方法 ---

// getMytosClient 根据设备IP和实例名创建 MYTOS 客户端
func (f *FileService) getMytosClient(deviceIP, androidName string) (mytos.Client, error) {
	androids, err := f.getAndroid(deviceIP, androidName)
	if err != nil {
		return nil, err
	}

	// 公网模式：走网关透传
	if f.cm != nil && f.cm.GetDeployMode() == "public" && f.sdkApiSvc != nil {
		token, err := f.sdkApiSvc.getPublicToken()
		if err != nil {
			return nil, fmt.Errorf("获取公网 token 失败: %w", err)
		}
		return newPublicMytosClientForAndroid(f.mirrorSvc, androids, deviceIP, token), nil
	}

	var addr ports.Addr
	if ports.IsMacvlanDirect(androids.NetworkName, androids.PortBindings) && androids.ContainerIP != "" {
		addr = ports.MYTOSDirect(*androids)
	} else {
		// 主机地址用 android.IP：v2/public 下传入的 deviceIP 可能是序列号，不能当主机名连
		addr = ports.MYTOS(*androids, androids.IP)
	}

	return newMytosClientForAndroid(f.mirrorSvc, androids, addr.Host, addr.Port), nil
}

// getAndroid 根据设备标识与容器名查配置缓存中的云机。
// 状态护栏：命中后校验容器状态为可操作（running/starting/restarting/resetting），
// 关机（exited/dead/stopped/created）容器按缓存端口拨号必然失败（dial refused），
// 返回明确错误而非让调用方拿到一个注定拨不通的客户端。
// 注意：本护栏作用于 getMytosClient 的全部入口（单发上传/下载/列目录等）。
func (f *FileService) getAndroid(deviceIP, name string) (*config.AndroidInfo, error) {
	a, err := f.getAndroidLoose(deviceIP, name)
	if err != nil {
		return nil, err
	}
	if !runnableContainerStatus(a.Status) {
		return nil, fmt.Errorf("云机 %s 已关机（状态 %s），无法进行文件操作", a.Name, a.Status)
	}
	return a, nil
}

// runnableContainerStatus 容器状态是否可操作（与前端 isRunnableStatus 口径同源：
// running/starting/restarting/resetting；paused 归一 starting）。
func runnableContainerStatus(status string) bool {
	switch status {
	case "running", "starting", "restarting", "resetting", "paused":
		return true
	default:
		return false
	}
}
