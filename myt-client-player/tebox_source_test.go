package main

import (
	"bytes"
	"testing"
)

// TestSplitAnnexB 验证 Annex-B → NALU 拆分（tebox 帧协议 payload 进入解码管线的关键转换）。
func TestSplitAnnexB(t *testing.T) {
	sps := []byte{0x67, 0x42, 0xC0, 0x1F}
	pps := []byte{0x68, 0xCE, 0x3C, 0x80}
	idr := []byte{0x65, 0x88, 0x84, 0x00, 0x10}
	p := []byte{0x41, 0x9A, 0x02, 0x05}

	cases := []struct {
		name string
		in   []byte
		want [][]byte
	}{
		{
			name: "4字节start code多NALU",
			in:   concat([]byte{0, 0, 0, 1}, sps, []byte{0, 0, 0, 1}, pps, []byte{0, 0, 0, 1}, idr),
			want: [][]byte{sps, pps, idr},
		},
		{
			name: "3字节start code",
			in:   concat([]byte{0, 0, 1}, p, []byte{0, 0, 1}, idr),
			want: [][]byte{p, idr},
		},
		{
			name: "下一start code前零填充归属分隔",
			in:   concat([]byte{0, 0, 0, 1}, pps, []byte{0, 0, 0, 0, 0, 1}, idr),
			want: [][]byte{pps, idr},
		},
		{
			name: "无start code返回空",
			in:   idr,
			want: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := splitAnnexB(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("got %d nalus, want %d: %x", len(got), len(c.want), got)
			}
			for i := range got {
				if !bytes.Equal(got[i], c.want[i]) {
					t.Fatalf("nalu[%d] = %x, want %x", i, got[i], c.want[i])
				}
			}
		})
	}
}

// TestSplitAnnexBCopyIndependence 拆分结果必须是独立拷贝（改写不影响入参）。
func TestSplitAnnexBCopyIndependence(t *testing.T) {
	sps := []byte{0x67, 0x01, 0x02}
	in := concat([]byte{0, 0, 0, 1}, sps)
	got := splitAnnexB(in)
	got[0][0] = 0xFF
	if in[4] != 0x67 {
		t.Fatalf("splitAnnexB 未拷贝，入参被改写")
	}
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
