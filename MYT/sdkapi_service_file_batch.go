package main

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/moyunteng/myt/internal/config"
)

// --- 同设备多容器批量上传（共享目录去重） ---
// 依据（2026-08-31 192.0.2.202 r1q_v3 实测）：同一物理设备各容器的 /sdcard/upload
// 是同一底层存储的挂载视图（sdcardfs/fuse），首容器上传后其余容器即可见。
// 因此同一文件对同一设备只做一次网络传输，其余容器从共享路径复制/安装。
// 异构设备若不共享，探测/cp 失败时自动回退逐容器网络上传，行为与旧版一致。

// BatchUploadFailure 单容器失败明细
type BatchUploadFailure struct {
	AndroidName string `json:"androidName"`
	Error       string `json:"error"`
}

// BatchUploadResult 批量上传结果：成功容器列表与失败明细
type BatchUploadResult struct {
	Installed []string             `json:"installed"`
	Failed    []BatchUploadFailure `json:"failed"`
}

// UploadToCloudBatch 批量上传本地文件到同一设备的多个云机。
// 仅首容器走网络上传，其余容器复用共享目录 /sdcard/upload 中的文件：
//   - .apk：cp 到容器私有 /data/local/tmp 后 pm install（不再传输）
//   - 非 APK 普通文件：共享可见即完成（该设备所有云机都能看到该文件）
//   - .apks/.xapk：MYTOS /installapks 只收 multipart 文件流，无法从路径安装，逐容器完整上传
// autoGrantPermissions：pm install 追加 -g（自动授予所有运行时权限），仅对 .apk 生效。
//
// 死容器隔离（2026-11-06）：入口先按可操作状态（runnableContainerStatus，同 getAndroid
// 护栏口径）挑出关机容器直接记失败明细——既不作为首容器（否则拨号拒绝毒整组），也不进
// 后续共享复制流程。首容器上传失败时改从剩余容器重选首容器重试，而不是整组报废：
// 共享目录方案里「首容器」只是传输载体，任何一台可操作容器都可以承担。
func (f *FileService) UploadToCloudBatch(deviceIP string, androidNames []string, localFilePath string, autoGrantPermissions bool) (*BatchUploadResult, error) {
	if len(androidNames) == 0 {
		return nil, fmt.Errorf("容器列表为空")
	}
	ext := strings.ToLower(filepath.Ext(localFilePath))
	fileName := filepath.Base(localFilePath)

	result := &BatchUploadResult{}

	// 入口隔离：非可操作容器直接记失败明细（带明确「已关机」原因），不进任何传输流程
	var runnable []string
	for _, name := range androidNames {
		a, err := f.getAndroidLoose(deviceIP, name)
		if err == nil && runnableContainerStatus(a.Status) {
			runnable = append(runnable, name)
			continue
		}
		errMsg := fmt.Sprintf("云机 %s 已关机，无法上传", name)
		if err != nil {
			errMsg = err.Error() // 容器不在缓存等，沿用 getAndroid 的明确错误
		}
		f.emitFileProgress(deviceIP, name, fileName, 100, "failed")
		result.Failed = append(result.Failed, BatchUploadFailure{AndroidName: name, Error: errMsg})
	}
	if len(runnable) == 0 {
		// 全部不可操作：没有可传目标，直接返回失败明细（不报组级错误）
		return result, nil
	}

	// .apks/.xapk：设备端接口只收文件流，逐容器完整处理（与单容器上传行为一致）
	if ext == ".apks" || ext == ".xapk" {
		for _, name := range runnable {
			if _, err := f.UploadToCloud(deviceIP, name, localFilePath, autoGrantPermissions, ""); err != nil {
				f.emitFileProgress(deviceIP, name, fileName, 100, "failed")
				result.Failed = append(result.Failed, BatchUploadFailure{AndroidName: name, Error: err.Error()})
			} else {
				result.Installed = append(result.Installed, name)
			}
		}
		return result, nil
	}

	// 首容器：完整网络上传 + 安装（进度事件 0-80% 由 UploadToCloud 内部发出）。
	// 首容器失败 → 从剩余容器重选首容器重试（共享目录方案的首容器只是传输载体，
	// 换一台即可；全部失败才放弃）。失败过的容器不再重试。
	var remotePath string
	var uploadedErr error
	rest := runnable
	for {
		first := rest[0]
		remotePath, uploadedErr = f.UploadToCloud(deviceIP, first, localFilePath, autoGrantPermissions, "")
		if uploadedErr == nil {
			result.Installed = append(result.Installed, first)
			rest = rest[1:]
			break
		}
		f.emitFileProgress(deviceIP, first, fileName, 100, "failed")
		result.Failed = append(result.Failed, BatchUploadFailure{AndroidName: first, Error: uploadedErr.Error()})
		rest = rest[1:]
		if len(rest) == 0 {
			// 所有候选首容器都失败：组内已无可承担网络上传的容器，整组失败收场
			return result, nil
		}
	}

	// 其余容器：复用共享目录（不再走网络传输）
	for _, name := range rest {
		if ext == ".apk" {
			// cp 失败 = 共享不可见（异构设备），回退网络上传
			if err := f.installApkFromShared(deviceIP, name, fileName, remotePath, autoGrantPermissions); err != nil {
				if _, err2 := f.UploadToCloud(deviceIP, name, localFilePath, autoGrantPermissions, ""); err2 != nil {
					f.emitFileProgress(deviceIP, name, fileName, 100, "failed")
					result.Failed = append(result.Failed, BatchUploadFailure{AndroidName: name, Error: err2.Error()})
				} else {
					result.Installed = append(result.Installed, name)
				}
			} else {
				result.Installed = append(result.Installed, name)
			}
			continue
		}
		// 非 APK 普通文件：共享可见即完成（一份文件全设备可见）；不可见则回退网络上传
		if f.sharedFileVisible(deviceIP, name, remotePath) {
			f.emitFileProgress(deviceIP, name, fileName, 100, "done")
			result.Installed = append(result.Installed, name)
		} else if _, err := f.UploadToCloud(deviceIP, name, localFilePath, autoGrantPermissions, ""); err != nil {
			f.emitFileProgress(deviceIP, name, fileName, 100, "failed")
			result.Failed = append(result.Failed, BatchUploadFailure{AndroidName: name, Error: err.Error()})
		} else {
			result.Installed = append(result.Installed, name)
		}
	}
	return result, nil
}

// getAndroidLoose 查配置缓存中的云机但不做状态校验（状态判定由调用方按需做）。
// 批量入口需要读状态来分组，直接复用 getAndroid 的查找逻辑会因护栏提前报错。
func (f *FileService) getAndroidLoose(deviceIP, name string) (*config.AndroidInfo, error) {
	if f.cm == nil {
		return nil, fmt.Errorf("配置管理器未初始化")
	}
	for _, a := range f.cm.GetAndroidList() {
		if a.Name == name && (a.IP == deviceIP || a.DeviceID == deviceIP) {
			return &a, nil
		}
	}
	return nil, fmt.Errorf("未找到云机 %s@%s", name, deviceIP)
}

// installApkFromShared 从共享目录复制 APK 到容器私有目录并安装（零网络传输）。
// cp 失败（共享不可见）时返回错误，调用方回退网络上传。
func (f *FileService) installApkFromShared(deviceIP, androidName, fileName, sharedPath string, autoGrantPermissions bool) error {
	sdkClient := f.sdkApiSvc.newClientWithTimeout(deviceIP, 5*time.Minute)
	installPath := fmt.Sprintf("/data/local/tmp/%s", fileName)
	f.emitFileProgress(deviceIP, androidName, fileName, 80, "installing")
	if _, err := sdkClient.DockerExec(androidName, fmt.Sprintf("cp '%s' '%s'", sharedPath, installPath)); err != nil {
		return fmt.Errorf("共享目录复制失败: %w", err)
	}
	// pm install 同步执行：-r 覆盖安装，-t 允许测试包；勾选自动授权时追加 -g
	// （与 UploadToCloud 单容器路径一致，默认不带 -g）
	installCmd := "pm install -r -t"
	if autoGrantPermissions {
		installCmd = "pm install -r -t -g"
	}
	result, err := sdkClient.DockerExec(androidName, fmt.Sprintf("%s '%s'", installCmd, installPath))
	_, _ = sdkClient.DockerExec(androidName, fmt.Sprintf("rm -f '%s'", installPath))
	if err != nil {
		return fmt.Errorf("APK安装失败: %w", err)
	}
	// 安装结果判定：与 UploadToCloud 一致，要求输出含 "Success"，放过非标准失败输出会静默假成功
	out := strings.TrimSpace(result.Output)
	log.Printf("[APK安装] %s → %s(共享): exit=%d output=%q", fileName, androidName, result.ExitCode, out)
	if !strings.Contains(out, "Success") {
		return fmt.Errorf("APK安装失败: %s", out)
	}
	log.Printf("[APK安装] 安装成功(共享): %s → %s", fileName, androidName)
	f.emitFileProgress(deviceIP, androidName, fileName, 100, "done")
	return nil
}

// sharedFileVisible 探测共享路径在指定容器内是否可见（异构设备可能不共享 /sdcard/upload）
func (f *FileService) sharedFileVisible(deviceIP, androidName, sharedPath string) bool {
	client := f.sdkApiSvc.newClientWithTimeout(deviceIP, 15*time.Second)
	resp, err := client.DockerExec(androidName, fmt.Sprintf("test -f '%s' && echo MYTOK", sharedPath))
	if err != nil {
		return false
	}
	return strings.Contains(resp.Output, "MYTOK")
}

// emitFileProgress 发送文件进度事件（与 UploadToCloud 内部事件同构，驱动任务队列设备维度进度）
func (f *FileService) emitFileProgress(deviceIP, androidName, fileName string, progress int, stage string) {
	f.emitEvent("file:upload-progress", map[string]any{
		"deviceIP":    deviceIP,
		"androidName": androidName,
		"fileName":    fileName,
		"progress":    progress,
		"stage":       stage,
	})
}
