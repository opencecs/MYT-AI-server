package taskqueue

import (
	"fmt"
	"strconv"
)

// SDK 指定版本安装任务（batch）。
//
// 与 sdkUpgrade（触发设备自拉云端）不同：sdkInstall 由客户端下载指定版本 zip
// → 上传到设备 /server/upgrade/upload → 轮询确认设备自报版本 == 目标版本，
// 版本未变化视为该台失败（用户拍板：版本变化才算成功）。
// version 随每个 target 携带（前端同一任务里各 target 的版本一致），
// handler 逐台闭包捕获，与现有 batch 注册模式保持一致（无额外接口方法）。
//
// 版本统一按 string 存储（JSON 原始字符串）——v2_box_sdk 版本号是 string 且
// 可能非纯数字；handler 按部署模式分流：v2 → InstallV2SdkVersion（string 直用），
// 其他模式 → InstallSdkVersion（V3，Atoi 转 int）。
func init() {
	registerBatch("sdkInstall", func(sdk SdkApiProvider, t map[string]any) error {
		version := str(t, "version")
		if version == "" {
			// 兼容历史任务 payload 中 version 为数字（JSON 反序列化为 float64）的情况
			if f, ok := t["version"].(float64); ok && f > 0 {
				version = strconv.FormatFloat(f, 'f', -1, 64)
			}
		}
		if version == "" {
			return fmt.Errorf("任务缺少有效的 SDK 版本号")
		}
		return sdk.InstallSdkVersionAny(str(t, "deviceIp"), version)
	})
}
