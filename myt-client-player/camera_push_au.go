//go:build windows

package main

// H.264 Annex-B 裸流的 NAL/AU 拆分：自研推流器把 ffmpeg h264 pipe 输出
// 切分为 TLV 协议要求的 SPS/PPS 消息与帧消息（AU 级）。

// auSplitter 把 ffmpeg h264 pipe 裸流按 NAL 拆分为 SPS/PPS 消息与帧消息（AU 级）。
// baseline profile 每帧仅 1 个 slice NAL：SPS/PPS 成对到达即发一条 SPS/PPS 消息；
// 其余 NAL（AUD/SEI）累积为 AU 前缀，遇 slice NAL(1/5) 整 AU 发一条帧消息。
type auSplitter struct {
	buf     []byte
	pending []byte // 当前 AU 前缀（含起始码）
	sps     []byte // 待配对的 SPS（含起始码）
}

func (s *auSplitter) feed(b []byte, emitSps, emitFrame func([]byte)) {
	s.buf = append(s.buf, b...)
	for {
		scIdx, scLen := findStartCode(s.buf, 0)
		if scIdx < 0 {
			s.buf = s.buf[keepStartCodeTail(s.buf):]
			break
		}
		if scIdx > 0 {
			s.buf = s.buf[scIdx:]
		}
		next, _ := findStartCode(s.buf, scLen)
		if next < 0 {
			break // 当前 NAL 未收全，等更多数据
		}
		nalType := s.buf[scLen] & 0x1f
		nalWithSC := s.buf[:next]
		switch nalType {
		case 7: // SPS
			s.sps = append(s.sps[:0:0], nalWithSC...)
		case 8: // PPS → 与 SPS 配对发出
			if s.sps != nil {
				block := append(append([]byte(nil), s.sps...), nalWithSC...)
				s.sps = nil
				emitSps(block)
			}
		case 1, 5: // slice → 当前 AU 完成
			block := append(append([]byte(nil), s.pending...), nalWithSC...)
			s.pending = s.pending[:0]
			if len(block) > 5 {
				emitFrame(block)
			}
		default: // AUD/SEI 等 → AU 前缀
			s.pending = append(s.pending, nalWithSC...)
		}
		s.buf = s.buf[next:]
	}
}

// flush 在流结束时处理残留缓冲中的最后一个 NAL（feed 只在看到下一个起始码时
// 才处理当前 NAL，EOF 时最后一个 NAL 需要显式 flush）。
func (s *auSplitter) flush(emitSps, emitFrame func([]byte)) {
	if len(s.buf) == 0 {
		return
	}
	scIdx, scLen := findStartCode(s.buf, 0)
	if scIdx < 0 || scLen+1 > len(s.buf) {
		s.buf = nil
		return
	}
	s.buf = s.buf[scIdx:]
	nalType := s.buf[scLen] & 0x1f
	switch nalType {
	case 7:
		s.sps = append(s.sps[:0:0], s.buf...)
	case 8:
		if s.sps != nil {
			block := append(append([]byte(nil), s.sps...), s.buf...)
			s.sps = nil
			emitSps(block)
		}
	case 1, 5:
		block := append(append([]byte(nil), s.pending...), s.buf...)
		s.pending = s.pending[:0]
		if len(block) > 5 {
			emitFrame(block)
		}
	default:
		s.pending = append(s.pending, s.buf...)
	}
	s.buf = nil
}

// findStartCode 在 b[off:] 中找 000001/00000001 起始码，返回位置与长度。
func findStartCode(b []byte, off int) (int, int) {
	for i := off; i+2 < len(b); i++ {
		if b[i] != 0 || b[i+1] != 0 {
			continue
		}
		if b[i+2] == 1 {
			return i, 3
		}
		if i+3 < len(b) && b[i+2] == 0 && b[i+3] == 1 {
			return i, 4
		}
	}
	return -1, 0
}

// keepStartCodeTail 无起始码命中时，保留可能是半截起始码的尾部零字节（最多 3 个）。
func keepStartCodeTail(b []byte) int {
	n := len(b)
	k := n
	for k > 0 && k > n-3 {
		if b[k-1] == 0 {
			k--
		} else {
			break
		}
	}
	return k
}
