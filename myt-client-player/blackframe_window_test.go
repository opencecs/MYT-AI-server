//go:build windows

package main

// blackframe_window_test.go — 黑帧滑动窗口判定单测（模拟帧序列，时间显式推进）。

import (
	"testing"
	"time"
)

func newBlackTestWindow() *BlackFrameWindow {
	return &BlackFrameWindow{}
}

// 场景 1：成对交替（黑-正常-黑-正常，每段 streak=1）→ 黑占比 >25% → 拦截
func TestBlackWindowPairedAlternating(t *testing.T) {
	w := newBlackTestWindow()
	base := time.Now()
	dropped := 0
	shown := 0
	for i := 1; i <= 10; i++ {
		now := base.Add(time.Duration(i*50) * time.Millisecond)
		black := i%2 == 1 // 奇数帧黑
		drop, show := w.Observe(now, black)
		if drop && show {
			t.Fatalf("drop 与 show 不能同时为 true")
		}
		if black {
			if drop {
				dropped++
			} else if show {
				shown++
			}
		}
	}
	// 前期占比未超阈的黑帧放行（最多 1~2 帧），随后全部拦截
	if dropped == 0 {
		t.Fatalf("交替黑帧应触发丢弃")
	}
	if shown > 2 {
		t.Fatalf("交替场景放行的黑帧过多: %d", shown)
	}
}

// 场景 2：单帧黑，间隔 5 秒（低频）→ 放行显示（宽容：真黑屏内容）
func TestBlackWindowSingleRareBlackShows(t *testing.T) {
	w := newBlackTestWindow()
	base := time.Now()
	// 窗口内先铺几帧正常帧（建立 lastMs 与正常节奏）
	for i := 1; i <= 3; i++ {
		w.Observe(base.Add(time.Duration(i*16)*time.Millisecond), false)
	}
	// 5 秒后的单帧黑
	drop, show := w.Observe(base.Add(5*time.Second), true)
	if drop || !show {
		t.Fatalf("低频单帧黑应放行显示: drop=%v show=%v", drop, show)
	}
}

// 场景 1b：稀疏低占比黑（真内容/偶发黑屏）→ 放行，不误拦。
// 正常帧与孤立黑帧交替、黑帧间隔远超窗口：孤立黑帧按帧间隔估算封顶
// 50ms < 25% 阈值 75ms，且窗口 300ms 早已排空，覆盖不会累计。
func TestBlackWindowSparseBlackShows(t *testing.T) {
	w := newBlackTestWindow()
	base := time.Now()
	// 正常帧 16ms 节奏 + 每 2 秒一帧孤立黑
	for i := 0; i < 5; i++ {
		blackAt := base.Add(time.Duration(i*2)*time.Second + 16*time.Millisecond)
		for j := 0; j < 125; j++ { // 黑帧前的正常帧（铺满 2s 间隔）
			t := base.Add(time.Duration(i*2)*time.Second + time.Duration(j)*16*time.Millisecond)
			if !t.Before(blackAt) {
				break
			}
			w.Observe(t, false)
		}
		drop, show := w.Observe(blackAt, true)
		if drop || !show {
			t.Fatalf("稀疏孤立黑帧应放行: i=%d drop=%v show=%v", i, drop, show)
		}
		w.Observe(blackAt.Add(16*time.Millisecond), false) // 黑后正常帧闭合黑段
	}
}

// 场景 3：连续黑帧 → 合并成段，覆盖时长快速超阈 → 拦截
func TestBlackWindowConsecutiveBlackDrops(t *testing.T) {
	w := newBlackTestWindow()
	base := time.Now()
	for i := 1; i <= 3; i++ {
		w.Observe(base.Add(time.Duration(i*16)*time.Millisecond), false)
	}
	anyDrop := false
	for i := 4; i <= 15; i++ { // 12 帧连续黑 @16ms，覆盖 192ms 超阈
		now := base.Add(time.Duration(i*16) * time.Millisecond)
		drop, _ := w.Observe(now, true)
		if drop {
			anyDrop = true
		}
	}
	if !anyDrop {
		t.Fatalf("连续黑帧应触发丢弃")
	}
}

// 场景 4：交替拦截后黑流停止 → 正常帧恢复，窗口排空后单帧黑重新放行
func TestBlackWindowRecoveryAfterStop(t *testing.T) {
	w := newBlackTestWindow()
	base := time.Now()
	// 交替 10 帧（拦截期）
	for i := 1; i <= 10; i++ {
		w.Observe(base.Add(time.Duration(i*50)*time.Millisecond), i%2 == 1)
	}
	// 1 秒后连续正常帧
	for i := 11; i <= 40; i++ {
		drop, show := w.Observe(base.Add(time.Duration(i*50)*time.Millisecond), false)
		if drop || show {
			t.Fatalf("正常帧不应触发任何处置: drop=%v show=%v", drop, show)
		}
	}
	// 窗口已排空：新单帧黑应重新放行（恢复宽容）
	drop, show := w.Observe(base.Add(3*time.Second), true)
	if drop || !show {
		t.Fatalf("窗口排空后低频单帧黑应放行: drop=%v show=%v", drop, show)
	}
}

// 场景 6：25%~50% 占比的检测不稳定中间带 → 丢弃保帧（收紧后不再放行，
// 「宁可卡住，不要闪」：中间带放行会让漏检黑帧上屏 = 闪）。
func TestBlackWindowMidBandDrops(t *testing.T) {
	w := newBlackTestWindow()
	base := time.Now()
	// 黑帧 40ms/帧、正常帧 80ms/帧节奏：黑覆盖约 1/3（≈33%，落在 25%~50% 中间带）
	dropped := 0
	for i := 1; i <= 15; i++ {
		now := base.Add(time.Duration(i*40) * time.Millisecond)
		black := i%3 != 0 // 2 黑 1 正
		drop, _ := w.Observe(now, black)
		if black && drop {
			dropped++
		}
	}
	if dropped == 0 {
		t.Fatalf("25%%~50%% 中间带黑帧应丢弃保帧（卡住不闪），实际全部放行")
	}
}

// 场景 7：Reset 清空历史
func TestBlackWindowReset(t *testing.T) {
	w := newBlackTestWindow()
	base := time.Now()
	for i := 1; i <= 10; i++ {
		w.Observe(base.Add(time.Duration(i*50)*time.Millisecond), i%2 == 1)
	}
	w.Reset()
	drop, show := w.Observe(base.Add(time.Second), true)
	if drop || !show {
		t.Fatalf("Reset 后单帧黑应放行: drop=%v show=%v", drop, show)
	}
}
