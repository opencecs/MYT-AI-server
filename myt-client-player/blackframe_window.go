//go:build windows

package main

// blackframe_window.go — 黑帧滑动窗口判定（占比驱动，不依赖 streak 连续性）。
//
// 策略（与 ffdecode.c BGRA 路径同款，改动需两处同步）：
//   300ms 滑动窗口内黑帧覆盖时长 ≥blackHoldMsPct（25%，≥75ms）判异常 →
//   丢弃保上一帧；<25% 放行（宽容：低频黑可能是真黑屏内容，如设备熄屏）。
//
// 黑帧按连续性合并成段计「覆盖时长」（黑段长度 = 到下一帧到达为止），成对
// 交替（黑-正常-黑-正常，每段 streak 只有 1）同样累计占比超阈 → 也能拦住。
//
// 当前黑帧的时长在到达时刻未知（黑段长度只有下一帧到达才确定），按「与上一
// 帧的间隔」估算，封顶 100ms 防大间隔帧导致占比虚高。
//
// 调参位置：blackWinMs（窗口宽度）、blackHoldMsPct（丢弃阈值）、blackEstCapMs
// （黑帧时长估算封顶）、blackMaxSegs（窗口内最多黑段数）。判定不稳定的中间带
// 通过「成对交替自动累计覆盖」兜底——占比临界时半数黑帧被拦，正常帧占比同步
// 走高，最终整段超阈转为全拦（卡住不闪）。

import "time"

const (
	// blackWinMs 滑动窗口宽度（黑覆盖占比的分母）。
	blackWinMs = 300
	// blackHoldMsPct 黑覆盖占比超过该阈值（%）→ 丢弃保帧（卡住不闪）。
	// 原 50% 放行了「占比 25%~50%」的检测不稳定中间带（漏检黑帧上屏 = 闪），
	// 收紧到 25% 后中间带整体丢弃，用户看到「卡住」而不是「互相闪」。
	blackHoldMsPct = 25
	// blackEstCapMs 当前黑帧时长估算封顶。与 blackHoldMsPct 配套：孤立黑帧
	// 按「与上一帧的间隔」估算（60fps 约 16ms），封顶 50ms < 25% 阈值 75ms
	// → 稀疏单帧黑（真内容/偶发黑屏）不会被误拦；连续黑从第二帧起走精确
	// 累计（now-start），不受此封顶影响。
	blackEstCapMs = 50
	// blackMaxSegs 窗口内最多保留的黑段数（60fps 交替约 10 段，2 倍冗余）。
	blackMaxSegs = 24
)

type blackSeg struct {
	start, end time.Time // 连续黑帧段 [start, end)
}

// BlackFrameWindow 黑帧滑动窗口（SendFrame 每帧调用，仅渲染线程单线程使用）
type BlackFrameWindow struct {
	segs       []blackSeg
	lastWasBlk bool
	lastMs     time.Time // 上一帧到达时刻，零值=无（估算当前黑帧时长用）
}

// Observe 记录一帧并给出处置决定：drop=丢弃保上一帧；show=放行显示。
// black=本帧探针判定为黑。
//
// 黑段时长口径（尽量精确，估算只用于孤立黑帧）：
//
//	连续黑段延续 → 覆盖 = now-start（精确）；黑→正常 → 段闭合于本帧到达
//	（精确）；新开黑段 → 时长未知，按帧间隔估算（封顶 50ms）。
func (w *BlackFrameWindow) Observe(now time.Time, black bool) (drop, show bool) {
	// 当前帧与上一帧的间隔（仅新开黑段估算用），封顶 50ms（blackEstCapMs 注释）
	est := time.Duration(0)
	if !w.lastMs.IsZero() && now.After(w.lastMs) {
		est = now.Sub(w.lastMs)
	}
	if est > blackEstCapMs*time.Millisecond {
		est = blackEstCapMs * time.Millisecond
	}

	// 段维护
	if black {
		if w.lastWasBlk && len(w.segs) > 0 {
			// 连续黑：段延续，覆盖时长精确为 now-start
			w.segs[len(w.segs)-1].end = now
		} else {
			// 新开黑段：时长未知，按帧间隔估算
			if len(w.segs) >= blackMaxSegs {
				w.segs = w.segs[1:]
			}
			w.segs = append(w.segs, blackSeg{start: now, end: now.Add(est)})
		}
	} else if w.lastWasBlk && len(w.segs) > 0 {
		// 黑→正常：黑段精确闭合（黑帧实际显示到本帧到达）
		w.segs[len(w.segs)-1].end = now
	}
	w.lastWasBlk = black
	w.lastMs = now

	// 窗口修剪（>blackWinMs 的段丢弃，跨界段裁剪起点）
	cut := now.Add(-blackWinMs * time.Millisecond)
	for len(w.segs) > 0 && !w.segs[0].end.After(cut) {
		w.segs = w.segs[1:]
	}
	if len(w.segs) > 0 && w.segs[0].start.Before(cut) {
		w.segs[0].start = cut
	}

	if !black {
		return false, false
	}

	// 覆盖统计 + 阈值判定：黑覆盖 ≥ blackHoldMsPct% 判异常 → 丢弃保帧。
	// 检测不稳定的中间带（漏检黑帧与被拦黑帧交替）靠「正常帧累计黑覆盖」
	// 收敛：占比临界时黑帧半数被拦，黑覆盖持续走高，最终整体超阈全拦
	// （卡住不闪）。
	var cover time.Duration
	for _, s := range w.segs {
		if s.end.After(s.start) {
			cover += s.end.Sub(s.start)
		}
	}
	if cover*100 >= blackWinMs*time.Millisecond*blackHoldMsPct {
		return true, false // 高频黑占比：丢弃保上一帧（卡住不闪）
	}
	return false, true // 低频黑：放行显示（宽容）
}

// Reset 清空窗口历史（流切换/重连后重新宽容判定）
func (w *BlackFrameWindow) Reset() {
	w.segs = nil
	w.lastWasBlk = false
	w.lastMs = time.Time{}
}
