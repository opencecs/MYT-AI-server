package main

import (
	"context"
	"sync"
	"time"
)

// scanGuard v2 扫描/状态刷新的互斥守卫，替代原 atomic probing 标志：
//   - 同一时刻只允许一个扫描/刷新批次在跑（owner="user" 用户显式扫描，
//     owner="background" 后台扫描/定向状态刷新）
//   - 用户扫描遇到后台批次：取消对方并短暂等待其退出后接管（用户优先）
//   - 用户扫描遇到用户扫描：报 errV2ScanBusy（防刷新按钮连点）
//   - 后台批次遇到任何持有者：静默放弃，不打扰用户
//
// 取消语义：持有者把 ctx 取消函数挂到 guard 上（attachCancel），被抢占时由
// 取消方触发——UDP 发现阶段与 8-worker 探测池响应 ctx.Done 提前退出，
// 已完成批次的探测结果照常落库（见 runIdxPool 与各调用方）。
type scanGuard struct {
	mu     sync.Mutex
	owner  string             // "user" / "background" / ""
	cancel context.CancelFunc // 当前持有者的扫描 ctx 取消函数
	done   chan struct{}      // unlock 时关闭，供用户扫描等待后台批次退出
}

// tryLock 尝试获取扫描锁；失败返回当前持有者（"user"/"background"）。
func (g *scanGuard) tryLock(owner string) (ok bool, cur string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.owner != "" {
		return false, g.owner
	}
	g.owner = owner
	g.done = make(chan struct{})
	return true, ""
}

// busy 只读查询：当前是否有扫描/刷新批次持有锁。供心跳 tick 让位判断用
// （不取锁、不产生持有者状态变更，纯读）。
func (g *scanGuard) busy() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.owner != ""
}

// attachCancel 绑定当前持有者的取消函数（tryLock 成功后、启动扫描前调用）。
func (g *scanGuard) attachCancel(cancel context.CancelFunc) {
	g.mu.Lock()
	g.cancel = cancel
	g.mu.Unlock()
}

// cancelOwner 取消当前持有者的扫描 ctx（用户扫描打断后台批次用）。
func (g *scanGuard) cancelOwner() {
	g.mu.Lock()
	if g.cancel != nil {
		g.cancel()
	}
	g.mu.Unlock()
}

// waitDone 等待当前持有者释放锁，最多等 timeout（超时返回，由调用方重试 tryLock）。
func (g *scanGuard) waitDone(timeout time.Duration) {
	g.mu.Lock()
	done := g.done
	g.mu.Unlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// unlock 释放锁并触发 cancel（回收 ctx 资源；扫描已正常结束时为 no-op）。
func (g *scanGuard) unlock() {
	g.mu.Lock()
	if g.cancel != nil {
		g.cancel()
	}
	g.owner = ""
	g.cancel = nil
	if g.done != nil {
		close(g.done)
		g.done = nil
	}
	g.mu.Unlock()
}

// v2ScanPreemptWait 用户扫描等待被打断的后台批次退出的时间上限。
// 后台批次收到取消后：发现层立即退出，在途单台探测（最长 20s）被 ctx 级联
// 中断，通常远快于此上限；超时则用户扫描本次报忙，下轮再试。
const v2ScanPreemptWait = 3 * time.Second

// runIdxPool 并发探测池（8 worker，收敛原两处内联的 idxCh+WaitGroup 结构）：
// 把 [0,n) 下标分发给 worker 执行 fn。响应 ctx 取消——已分发的批次跑完照常
// 落结果，未分发的跳过。每个下标只被一个 worker 写入，无共享写竞态；汇总
// 统一在返回后由调用方单线程做。
func runIdxPool(ctx context.Context, n int, fn func(idx int)) {
	const probeWorkers = 8
	idxCh := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < probeWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range idxCh {
				select {
				case <-ctx.Done():
					return
				default:
				}
				fn(idx)
			}
		}()
	}
feed:
	for i := 0; i < n; i++ {
		select {
		case idxCh <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(idxCh)
	wg.Wait()
}
