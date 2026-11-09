//go:build windows

package main

import "testing"

// TestParseADTSParams 验证 ADTS 头采样率/声道解析——「滋滋杂音」根因场景：
// 实例实际按 44.1kHz 编码（上游协议文档误写 48kHz），必须以 ADTS 头为准建链。
func TestParseADTSParams(t *testing.T) {
	cases := []struct {
		name string
		hdr  []byte // 4 字节 ADTS 头（0xFF 0xF1 b2 b3）
		sr   int
		ch   int
	}{
		{"44100 stereo（实测实例/测试夹具）", []byte{0xFF, 0xF1, 0x50, 0x80}, 44100, 2},
		{"48000 stereo", []byte{0xFF, 0xF1, 0x4C, 0x80}, 48000, 2},
		{"44100 mono", []byte{0xFF, 0xF1, 0x50, 0x40}, 44100, 1},
		{"短帧兜底 48k/2", []byte{0xFF, 0xF1}, 48000, 2},
		{"坏 syncword 兜底 48k/2", []byte{0x00, 0xF1, 0x50, 0x80}, 48000, 2},
		{"非法 srIdx 兜底 48k/2", []byte{0xFF, 0xF1, 0xFC, 0x80}, 48000, 2},
	}
	for _, c := range cases {
		sr, ch := parseADTSParams(c.hdr)
		if sr != c.sr || ch != c.ch {
			t.Errorf("%s: got %dHz/%dch, want %dHz/%dch", c.name, sr, ch, c.sr, c.ch)
		}
	}
}
