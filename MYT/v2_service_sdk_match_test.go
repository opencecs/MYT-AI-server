package main

import (
	"testing"

	"github.com/moyunteng/myt/internal/v2host"
)

// TestMatchSuperSdkContainer 超级 SDK 容器匹配：
// 实测主机（192.0.2.164）上 myt_vpc_plugin 与 myt_sdk 都带 myt_plugin_name
// label 且容器顺序不定，「取第一个带 label 的」会错拿 vpc 插件的版本。
func TestMatchSuperSdkContainer(t *testing.T) {
	containers := []v2host.ContainerInfo{
		{Names: []string{"/myt_vpc_plugin"}, Labels: map[string]string{"myt_plugin_name": "myt_vpc_plugin", "myt_plugin_ver": "1.1.4"}},
		{Names: []string{"/myt_sdk"}, Labels: map[string]string{"myt_plugin_name": "myt_sdk", "myt_plugin_ver": "1.0"}},
		{Names: []string{"/1791883529625_8_1"}, Labels: map[string]string{"idx": "8"}},
	}

	t.Run("云端插件名精确匹配优先", func(t *testing.T) {
		c := matchSuperSdkContainer(containers, "myt_sdk")
		if c == nil || c.Labels["myt_plugin_ver"] != "1.0" {
			t.Fatalf("应匹配 myt_sdk（1.0），得到 %+v", c)
		}
	})

	t.Run("云端缺失时按myt_sdk容器名兜底", func(t *testing.T) {
		c := matchSuperSdkContainer(containers, "")
		if c == nil || c.Labels["myt_plugin_ver"] != "1.0" {
			t.Fatalf("应跳过 vpc 插件匹配 myt_sdk（1.0），得到 %+v", c)
		}
	})

	t.Run("云端匹配不到时不落回其他插件", func(t *testing.T) {
		if c := matchSuperSdkContainer(containers, "myt_not_exist"); c != nil {
			t.Fatalf("精确匹配失败应返回 nil，得到 %+v", c)
		}
	})

	t.Run("未装超级SDK时返回nil", func(t *testing.T) {
		if c := matchSuperSdkContainer(containers[:1], ""); c != nil {
			t.Fatalf("只有 vpc 插件时应返回 nil，得到 %+v", c)
		}
	})
}
