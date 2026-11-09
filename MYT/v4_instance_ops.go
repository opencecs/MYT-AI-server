package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/v4host"
	"github.com/moyunteng/myt/internal/v4ws"
)

// --- V4 实例操作（8088 REST）公共部分 ---
//
// 供三处复用：
//   - v4ws_adapter.go 的 REST 补拉（WS 断连/未知实例）
//   - sdkapi_service_v4.go 的列表与启停/删除/重置分支
//   - setup.go 注入 androidlist 的 v4 查询回调
//
// 凭据：tobox 默认 admin/myt（tebox.auth 配置可改，客户端暂不提供设置入口，
// 与 /health 探测一致——后续接设置项时改 v4Creds 即可）。

// v4Creds 返回 V4 Basic Auth 凭据
func v4Creds() (string, string) {
	return v4host.DefaultUser, v4host.DefaultPassword
}

// newV4InstanceClient 创建 V4 8088 REST 客户端（ip 可为纯 IP 或 ip:8088）
func newV4InstanceClient(ip string) *v4host.InstanceClient {
	user, password := v4Creds()
	return v4host.NewInstanceClient(ip, user, password)
}

// v4InstancesToCache 把 REST 列表转成 config 容器缓存条目。
//
// indexOf 由共享 v4Indexer 派生（与 WS 适配层同源），保证「REST 补拉」与
// 「WS 快照」写入的截图 key（IP_indexNum）一致。
func v4InstancesToCache(instances []v4host.Instance, ip string, indexOf map[string]int) []config.AndroidInfo {
	list := make([]config.AndroidInfo, 0, len(instances))
	for _, it := range instances {
		key := v4ws.InstanceKey(it.Backend, it.ID)
		indexNum := indexOf[key]
		if indexNum <= 0 {
			indexNum = 1
		}
		list = append(list, v4CacheItem(v4ItemFields{
			Key: key, Name: it.Name, FallbackID: it.ID, State: it.State, Variant: it.Variant,
			MemoryMB: it.MemoryMB, DiskUsedMB: it.DiskUsedMB,
		}, ip, indexNum))
	}
	return list
}

// v4ItemFields 实例转缓存的字段集合（v4ws.InstanceItem 与 v4host.Instance 同构，
// 二者都先收敛到本结构再转换，避免两份几乎相同的赋值代码演化分叉）。
type v4ItemFields struct {
	Key        string
	Name       string
	FallbackID string
	State      string
	Variant    string
	MemoryMB   int32
	DiskUsedMB int32
	// Profile 设备档案（仅 WS snapshot 下发；REST 列表无此字段，恒 nil）
	Profile *config.AndroidProfile
}

// v4CacheItem 统一转换：字段来源收敛后落成 config.AndroidInfo。
// indexNum 决定截图 key（IP_indexNum），调用方负责派生。
func v4CacheItem(f v4ItemFields, ip string, indexNum int) config.AndroidInfo {
	if indexNum <= 0 {
		indexNum = 1
	}
	name := f.Name
	if name == "" {
		name = f.FallbackID
	}
	return config.AndroidInfo{
		ID:        f.Key,
		Name:      name,
		Status:    v4StateToStatus(f.State),
		Image:     f.Variant,
		IndexNum:  indexNum,
		DeviceID:  ip,
		IP:        ip,
		ModelPath: f.Variant,
		// V4 无实时用量字段：内存按配额填充（UI 进度条 100% 而非 0%，比空白更诚实）
		MemUsage:  int64(f.MemoryMB) << 20,
		MemLimit:  int64(f.MemoryMB) << 20,
		DiskUsage: int64(f.DiskUsedMB) << 20,
		Profile:   f.Profile,
	}
}

// v4ResolveRef 把前端传入的容器标识（容器名 / 缓存 ID）解析为 REST 操作参数 {id, backend}。
//
// 前端所有实例操作（StartAndroid/StopAndroid/DeleteAndroid）传的是 android.name
// （与 V3 一致）。V4 的 REST 主键是 (backend, id)，缓存条目的 ID 是 "backend/id"，
// 因此按 (DeviceID=主机IP, Name=name) 反查缓存拿 ID 再拆解。
// 反查失败（缓存滞后）时把 name 当裸 id、backend 留空（走服务端默认后端）。
func v4ResolveRef(cm *config.Manager, hostIP, nameOrID string) (v4host.InstanceRef, error) {
	if nameOrID == "" {
		return v4host.InstanceRef{}, fmt.Errorf("实例标识为空")
	}
	if cm != nil {
		for _, a := range cm.GetAndroidsByDevice(hostIP) {
			if a.Name != nameOrID && a.ID != nameOrID {
				continue
			}
			backend, id := v4ws.SplitInstanceKey(a.ID)
			return v4host.InstanceRef{ID: id, Backend: backend}, nil
		}
	}
	// 缓存未命中：按裸 id 处理（后端默认）
	backend, id := v4ws.SplitInstanceKey(nameOrID)
	return v4host.InstanceRef{ID: id, Backend: backend}, nil
}

// v4InstanceKeys 实例列表的联合主键集合（序号派生用）
func v4InstanceKeys(instances []v4host.Instance) []string {
	keys := make([]string, 0, len(instances))
	for _, it := range instances {
		keys = append(keys, v4ws.InstanceKey(it.Backend, it.ID))
	}
	return keys
}

// v4ListInstances REST 拉列表（indexNum 由共享 indexer 派生），返回可直接合并进缓存的条目。
func v4ListInstances(ctx context.Context, idx *v4Indexer, hostIP string) ([]config.AndroidInfo, error) {
	res, err := newV4InstanceClient(hostIP).List(ctx)
	if err != nil {
		return nil, err
	}
	return v4InstancesToCache(res.Instances, hostIP, idx.Assign(hostIP, v4InstanceKeys(res.Instances))), nil
}

// v4HostsOf 从设备列表提取 V4 WS 对账用的主机地址（纯 IP）。
// V4 以 IP 为设备 key（/health 无标识字段），DeviceCache.DeviceID 与 IP 同值；
// 无 IP 的条目（公网残留）跳过。
func v4HostsOf(devices []config.DeviceCache) []string {
	hosts := make([]string, 0, len(devices))
	for _, d := range devices {
		ip := d.IP
		if ip == "" {
			ip = d.DeviceID
		}
		if ip == "" {
			continue
		}
		hosts = append(hosts, v4HostIP(ip))
	}
	return hosts
}

// v4HostIP 从可能的 "ip:port" 或 "ip" 归一为纯 IP（V4 缓存 DeviceID/IP 字段都用纯 IP）
func v4HostIP(ident string) string {
	ident = strings.TrimSpace(ident)
	if i := strings.LastIndex(ident, ":"); i > 0 {
		return ident[:i]
	}
	return ident
}
