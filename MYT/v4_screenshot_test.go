package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/device/screenshot"
	"github.com/moyunteng/myt/internal/v4ws"
)

// --- V4 截图接入（v4_screenshot.go）单测 ---

// v4ScreenshotTargets：只取 running 实例，并把缓存 ID "backend/id" 拆回 tobox 主键；
// IndexNum 沿用缓存条目的值（截图 key = IP_indexNum，必须与前端一致）。
func TestV4ScreenshotTargets(t *testing.T) {
	cm := newV4TestManager(t)
	cm.SetAndroidList([]config.AndroidInfo{
		{ID: "coloros/coloros-1", Name: "coloros-1", DeviceID: "10.0.0.1", IP: "10.0.0.1",
			IndexNum: 1, Status: "running"},
		{ID: "default/origin-1", Name: "origin-1", DeviceID: "10.0.0.1", IP: "10.0.0.1",
			IndexNum: 2, Status: "exited"}, // 未 running：服务端 503，不抓
		{ID: "coloros/coloros-2", Name: "coloros-2", DeviceID: "10.0.0.1", IP: "10.0.0.1",
			IndexNum: 3, Status: "running"},
	})

	targets := v4ScreenshotTargets(cm)
	if len(targets) != 2 {
		t.Fatalf("应只有 2 个 running 目标，实际 %d 个: %+v", len(targets), targets)
	}
	byIndex := map[int]screenshot.ContainerTarget{}
	for _, tg := range targets {
		byIndex[tg.IndexNum] = tg
	}
	first, ok := byIndex[1]
	if !ok {
		t.Fatalf("缺少 indexNum=1 的目标: %+v", targets)
	}
	if first.InstanceID != "coloros-1" || first.Backend != "coloros" {
		t.Errorf("实例主键拆分错误: %+v", first)
	}
	if first.DeviceIP != "10.0.0.1" {
		t.Errorf("DeviceIP 应为主机 IP: %+v", first)
	}
	if _, ok := byIndex[2]; ok {
		t.Error("exited 实例不应进入截图目标")
	}
}

// v4ScreenshotFetcher：按实例主键调 8088，返回图片字节；非 v4 模式直接拒绝。
func TestV4ScreenshotFetcherRejectsOutsideV4(t *testing.T) {
	cm := newV4TestManager(t)
	if err := cm.SetDeployMode("local"); err != nil {
		t.Fatal(err)
	}
	fetch := newV4ScreenshotFetcher(cm)
	_, _, err := fetch(nil, screenshot.ContainerTarget{
		DeviceIP: "10.0.0.1", InstanceID: "a", Backend: "be",
	})
	if err == nil {
		t.Fatal("非 v4 模式应拒绝抓图（避免切模式后残余目标打 8088）")
	}
}

// v4ScreenshotFetcher：目标不完整时快速报错（不发请求）
func TestV4ScreenshotFetcherRejectsIncompleteTarget(t *testing.T) {
	cm := newV4TestManager(t)
	fetch := newV4ScreenshotFetcher(cm)
	if _, _, err := fetch(nil, screenshot.ContainerTarget{DeviceIP: "10.0.0.1"}); err == nil {
		t.Fatal("缺少 InstanceID 应报错")
	}
	if _, _, err := fetch(nil, screenshot.ContainerTarget{InstanceID: "a"}); err == nil {
		t.Fatal("缺少 DeviceIP 应报错")
	}
}

// v4ScreenshotInvalidator：状态变化 → 清该实例截图缓存（key=IP_indexNum）并触发重抓。
// 缓存里查不到该容器时退化为整机失效（防残留旧图）。
func TestV4ScreenshotInvalidator(t *testing.T) {
	cm := newV4TestManager(t)
	cm.SetAndroidList([]config.AndroidInfo{
		{ID: "be/a", Name: "a", DeviceID: "10.0.0.1", IP: "10.0.0.1", IndexNum: 1, Status: "running"},
		{ID: "be/b", Name: "b", DeviceID: "10.0.0.1", IP: "10.0.0.1", IndexNum: 2, Status: "running"},
	})
	// 用真实抓图通道把两张图灌进缓存（key 由服务按 IP_indexNum 生成，不额外暴露测试钩子）
	svc := screenshot.NewService()
	svc.SetSelectedDevices([]string{"10.0.0.1"})
	svc.SetContainerGetter(func() []screenshot.ContainerTarget { return v4ScreenshotTargets(cm) })
	var fail atomic.Bool
	svc.SetTargetFetcher(func(_ context.Context, tg screenshot.ContainerTarget) ([]byte, string, error) {
		if fail.Load() {
			// 失效回调会 TriggerNow 异步重抓：这里让重抓失败，断言才不会被重抓写回干扰
			return nil, "", errors.New("模拟 503")
		}
		return []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, byte(tg.IndexNum)}, "image/png", nil
	})
	svc.RefreshNow()
	if got := svc.GetScreenshots([]string{"10.0.0.1_1", "10.0.0.1_2"}); len(got) != 2 {
		t.Fatalf("预置截图失败，实际 %d 张: %v", len(got), got)
	}
	fail.Store(true)

	invalidate := v4ScreenshotInvalidator(cm, svc)
	invalidate("10.0.0.1", "be/a")

	if _, ok := svc.GetScreenshots([]string{"10.0.0.1_1"})["10.0.0.1_1"]; ok {
		t.Error("被失效实例的截图缓存应被清除")
	}
	if _, ok := svc.GetScreenshots([]string{"10.0.0.1_2"})["10.0.0.1_2"]; !ok {
		t.Error("同主机其他实例的截图不应受影响")
	}

	// 缓存查不到容器（刚删除）→ 整机失效
	invalidate("10.0.0.1", "be/gone")
	if got := svc.GetScreenshots([]string{"10.0.0.1_2"}); len(got) != 0 {
		t.Errorf("未知容器应退化为整机失效，剩余: %v", got)
	}
}

// v4EventAdapter：状态真的变化时才触发截图失效；重复推同一状态不清缓存
// （快照重发/重连会重复推同状态，无变化时清缓存等于每次白抓一轮图）。
func TestV4AdapterScreenshotInvalidateOnStateChange(t *testing.T) {
	cm := newV4TestManager(t)
	idx := newV4Indexer()
	ad := newV4EventAdapter(cm, idx, nil)
	var got []string
	ad.SetScreenshotInvalidator(func(hostIP, containerID string) {
		got = append(got, hostIP+"/"+containerID)
	})
	// 预置：be/a running
	cm.SetAndroidList([]config.AndroidInfo{
		{ID: "be/a", Name: "a", DeviceID: "10.0.0.1", IP: "10.0.0.1", IndexNum: 1, Status: "running"},
	})

	// 同状态重复推：不应失效
	ad.OnInstanceState("10.0.0.1", &v4ws.EventFrame{Action: "state"}, v4ws.InstanceItem{
		Backend: "be", ID: "a", State: "running",
	})
	if len(got) != 0 {
		t.Errorf("状态未变化不应触发截图失效，实际: %v", got)
	}

	// 状态变化 running → stopped(exited)：应失效
	ad.OnInstanceState("10.0.0.1", &v4ws.EventFrame{Action: "state"}, v4ws.InstanceItem{
		Backend: "be", ID: "a", State: "stopped",
	})
	if len(got) != 1 {
		t.Fatalf("状态变化应触发 1 次截图失效，实际: %v", got)
	}
	if got[0] != "10.0.0.1/be/a" {
		t.Errorf("失效目标不符: %v", got)
	}
}

// 保险：v4ShotLimiter 并发上限（同主机最多 N 路同时进行）
func TestV4ShotLimiterConcurrency(t *testing.T) {
	lim := &v4ShotLimiter{}
	releases := make([]func(), 0, v4ShotPerHostConcurrency)
	for i := 0; i < v4ShotPerHostConcurrency; i++ {
		release, err := lim.acquire(nil, "10.0.0.1")
		if err != nil {
			t.Fatalf("第 %d 路取并发位失败: %v", i+1, err)
		}
		releases = append(releases, release)
	}
	// 第 N+1 路必须等待（用带超时的 ctx 验证阻塞语义）
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if _, err := lim.acquire(ctx, "10.0.0.1"); err == nil {
		t.Fatal("超过并发上限应等待超时，实际立即获得")
	}
	// 另一台主机不受影响（按主机隔离）
	other, err := lim.acquire(nil, "10.0.0.2")
	if err != nil {
		t.Fatalf("不同主机应各自独立限流: %v", err)
	}
	other()
	for _, r := range releases {
		r()
	}
	// 释放后可再次获取
	again, err := lim.acquire(nil, "10.0.0.1")
	if err != nil {
		t.Fatalf("释放后应能再次获取: %v", err)
	}
	again()
}
