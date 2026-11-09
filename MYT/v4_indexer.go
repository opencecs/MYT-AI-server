package main

import (
	"sort"
	"sync"
)

// --- V4 实例序号（indexNum）派生 ---
//
// 为什么需要：V4 没有「坑位号」概念，但既有截图/投屏链路按 (IP, indexNum) 取图
// （screenshot key = "IP_indexNum"）。因此必须为每个实例派生一个主机内唯一、且
// **尽量稳定**的序号。
//
// 唯一权威实现放这里，两条写入路径共用：
//   - WS 快照（v4EventAdapter.OnSnapshot）
//   - REST 补拉（v4ListInstances / v4EventAdapter 的未知实例补齐）
//
// 两处若各写一套派生规则，同一个实例在两条路径下会拿到不同序号 → 截图 key 抖动、
// 画面错位。分配规则：
//   - 已分配过的 key 沿用原序号（不漂移）
//   - 新 key 按字典序分配「未占用的最小序号」
//
// 新增实例不会打乱既有实例的序号；删除实例后其序号空出，可被后续新实例复用。
type v4Indexer struct {
	mu   sync.Mutex
	byIP map[string]map[string]int
}

func newV4Indexer() *v4Indexer {
	return &v4Indexer{byIP: make(map[string]map[string]int)}
}

// Assign 为某主机的一批实例 key 分配序号，返回 key → indexNum（1 起）。
func (x *v4Indexer) Assign(ip string, keys []string) map[string]int {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.assignLocked(ip, keys)
}

func (x *v4Indexer) assignLocked(ip string, keys []string) map[string]int {
	prev := x.byIP[ip]
	next := make(map[string]int, len(keys))
	used := make(map[int]bool, len(keys))

	// 1) 沿用已分配的序号
	fresh := make([]string, 0, len(keys))
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		if n, ok := prev[k]; ok {
			next[k] = n
			used[n] = true
			continue
		}
		fresh = append(fresh, k)
	}

	// 2) 新实例按字典序取最小空位（顺序稳定 → 同一批实例每次得到同一结果）
	sort.Strings(fresh)
	slot := 1
	for _, k := range fresh {
		for used[slot] {
			slot++
		}
		next[k] = slot
		used[slot] = true
	}

	x.byIP[ip] = next
	return next
}

// DropAll 清空全部映射（模式切换离开 v4 时调用：各模式容器缓存互相隔离，
// 切回来时序号重新分配与已清空的 v4 桶天然一致）。
func (x *v4Indexer) DropAll() {
	x.mu.Lock()
	x.byIP = make(map[string]map[string]int)
	x.mu.Unlock()
}
