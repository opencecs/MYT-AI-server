package main

import (
	"context"
	"fmt"
	"log"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/v4ws"
)

// --- V4 模式的实例操作分支（8088 REST）---
//
// V4 的 REST 主键是 (backend, id) 联合主键，与 V3 的「容器名 name」不同：
// 前端沿用既有签名（deviceIP + name），这里按 (DeviceID=主机IP, Name) 反查缓存
// 解析出 {id, backend}（见 v4ResolveRef）。
//
// 已接入：列表 / 启动 / 停止 / 删除 / 重置（重启语义）/ 剪贴板 / 设备档案
// 未接入（V4 SDK 无对应端点或语义待定，调用返回明确错误，见各方法 TODO）：
//   创建实例、改名、移动坑位、标签、机型列表、终端 exec、镜像操作
// （剪贴板与设备档案见 v4_clipboard.go + 本文件末尾三个 v4 方法，2026-10 接入）

// v4OpContext 操作上下文（统一超时）
func v4OpContext() context.Context { return context.Background() }

// v4DeviceIP 把前端传入的标识解析为 V4 主机纯 IP。
func (s *SdkApiServiceWrapper) v4DeviceIP(ident string) string {
	return v4HostIP(s.resolveDeviceIP(ident))
}

// listAndroidsV4 V4 列表分支：REST /android → config.AndroidInfo（含 indexNum 派生）。
func (s *SdkApiServiceWrapper) listAndroidsV4(deviceIP string) ([]config.AndroidInfo, error) {
	ip := s.v4DeviceIP(deviceIP)
	list, err := v4ListInstances(v4OpContext(), s.v4Idx, ip)
	if err != nil {
		return nil, err
	}
	return list, nil
}

// startAndroidV4 POST /android/start
func (s *SdkApiServiceWrapper) startAndroidV4(deviceIP, name string) error {
	ip := s.v4DeviceIP(deviceIP)
	ref, err := v4ResolveRef(s.cm, ip, name)
	if err != nil {
		return err
	}
	if err := newV4InstanceClient(ip).Start(v4OpContext(), ref); err != nil {
		return err
	}
	s.notifyAndroidsChanged([]string{ip})
	return nil
}

// stopAndroidV4 POST /android/stop
func (s *SdkApiServiceWrapper) stopAndroidV4(deviceIP, name string) error {
	ip := s.v4DeviceIP(deviceIP)
	ref, err := v4ResolveRef(s.cm, ip, name)
	if err != nil {
		return err
	}
	if err := newV4InstanceClient(ip).Stop(v4OpContext(), ref); err != nil {
		return err
	}
	s.notifyAndroidsChanged([]string{ip})
	return nil
}

// deleteAndroidV4 POST /android/delete（连数据一起删除，不可恢复）
func (s *SdkApiServiceWrapper) deleteAndroidV4(deviceIP, name string) error {
	ip := s.v4DeviceIP(deviceIP)
	ref, err := v4ResolveRef(s.cm, ip, name)
	if err != nil {
		return err
	}
	if err := newV4InstanceClient(ip).Delete(v4OpContext(), ref); err != nil {
		return err
	}
	// 乐观移除缓存条目（WS 的 deleted 事件是权威源，但事件可能滞后/断连）
	if containerID := v4ws.InstanceKey(ref.Backend, ref.ID); containerID != "" && s.cm != nil {
		if s.cm.RemoveAndroidByContainer(ip, containerID) {
			s.emitEvent("androidlist:updated", s.cm.GetAndroidList())
		}
	}
	s.notifyAndroidsChanged([]string{ip})
	return nil
}

// restartAndroidV4 V4 无「容器 restart」端点，用 /android/reset（重启底层环境，
// wipe_data=false 不清数据）承接「重启」语义。
// TODO(v4): 若产品要求「关机再开机」语义，改为 Stop + Start 两段调用。
func (s *SdkApiServiceWrapper) restartAndroidV4(deviceIP, name string) error {
	ip := s.v4DeviceIP(deviceIP)
	ref, err := v4ResolveRef(s.cm, ip, name)
	if err != nil {
		return err
	}
	if err := newV4InstanceClient(ip).Reset(v4OpContext(), ref, false); err != nil {
		return err
	}
	s.notifyAndroidsChanged([]string{ip})
	return nil
}

// resetAndroidV4 POST /android/reset（wipeData=true 时恢复出厂），start=true 时随后开机。
func (s *SdkApiServiceWrapper) resetAndroidV4(deviceIP, name string, wipeData, start bool) error {
	ip := s.v4DeviceIP(deviceIP)
	ref, err := v4ResolveRef(s.cm, ip, name)
	if err != nil {
		return err
	}
	if err := newV4InstanceClient(ip).Reset(v4OpContext(), ref, wipeData); err != nil {
		return err
	}
	// V4 的 /android/reset 只重建环境，不负责开机：调用方要求 start 时补一次 start
	// （V3 的 ResetAndroid 语义是「重置后可选开机」，两端行为对齐）。
	if start {
		if err := newV4InstanceClient(ip).Start(v4OpContext(), ref); err != nil {
			s.notifyAndroidsChanged([]string{ip})
			return err
		}
	}
	s.notifyAndroidsChanged([]string{ip})
	return nil
}

// v4NotSupported V4 尚未接入的操作统一错误（前端 toast 可见，避免静默失败）。
func v4NotSupported(op string) error {
	log.Printf("[sdkapi] V4 模式不支持的操作: %s", op)
	return fmt.Errorf("tebox 模式暂不支持「%s」", op)
}

// --- V4 剪贴板与设备档案（POST /android/clipboard、/android/configure）---

// setAndroidClipboardV4 设置云机剪贴板（POST /android/clipboard）。
// paste=true 时云机把文本直接粘贴到当前焦点输入框。成功不推列表事件
// （剪贴板不影响实例列表/截图），只记日志。
func (s *SdkApiServiceWrapper) setAndroidClipboardV4(deviceIP, name, text string, paste bool) error {
	ip := s.v4DeviceIP(deviceIP)
	ref, err := v4ResolveRef(s.cm, ip, name)
	if err != nil {
		return err
	}
	if err := newV4InstanceClient(ip).SetClipboard(v4OpContext(), ref, text, paste); err != nil {
		return err
	}
	// 记下主动设置的文本（按实例），供 WS 回灌路径做防回声比对
	if s.v4Clip != nil {
		s.v4Clip.remember(ip, v4ws.InstanceKey(ref.Backend, ref.ID), text)
	}
	log.Printf("[sdkapi] V4 设置剪贴板成功: 主机=%s 实例=%s paste=%v %d 字符",
		ip, v4ws.InstanceKey(ref.Backend, ref.ID), paste, len([]rune(text)))
	return nil
}

// getAndroidProfileV4 读云机设备档案：WS snapshot 下发时已落缓存
// （config.AndroidInfo.Profile），直接反查，不额外打 REST。
func (s *SdkApiServiceWrapper) getAndroidProfileV4(deviceIP, name string) (*config.AndroidProfile, error) {
	ip := s.v4DeviceIP(deviceIP)
	if s.cm != nil {
		for _, a := range s.cm.GetAndroidsByDevice(ip) {
			if a.Name == name || a.ID == name {
				return a.Profile, nil
			}
		}
	}
	return nil, fmt.Errorf("未找到云机 %s@%s（档案随 WS 快照下发，请待设备连接后重试）", name, ip)
}

// configureAndroidProfileV4 设置云机设备档案（POST /android/configure，覆盖式写入）。
// 成功后把 profile 写回缓存条目（REST 列表不带 profile，WS 下一轮快照会再校准），
// 并触发列表推送让弹窗/预填拿到新值。
func (s *SdkApiServiceWrapper) configureAndroidProfileV4(deviceIP, name string, profile *config.AndroidProfile) error {
	if profile == nil {
		return fmt.Errorf("设备档案为空")
	}
	ip := s.v4DeviceIP(deviceIP)
	ref, err := v4ResolveRef(s.cm, ip, name)
	if err != nil {
		return err
	}
	if err := newV4InstanceClient(ip).Configure(v4OpContext(), ref, *profile); err != nil {
		return err
	}
	// 写回缓存（乐观更新；WS 下一轮 snapshot 是权威校准）
	containerID := v4ws.InstanceKey(ref.Backend, ref.ID)
	if s.cm != nil && s.cm.PatchAndroidProfile(ip, containerID, profile) {
		s.emitEvent("androidlist:updated", s.cm.GetAndroidList())
	}
	s.notifyAndroidsChanged([]string{ip})
	log.Printf("[sdkapi] V4 设置设备档案成功: 主机=%s 实例=%s", ip, containerID)
	return nil
}
