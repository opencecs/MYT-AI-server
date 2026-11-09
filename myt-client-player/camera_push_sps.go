//go:build windows

package main

// ─── H.264 SPS 位读取 + VUI 剥离 ───
// 用途：legacy 模式（WP 复刻）下剥掉 x264 写入的 full-range 色彩 VUI，
// 使 SPS 与 WindowsPusher 的输出一致（WP 的 x264 不写任何色彩 VUI）。

// spsReader 按 MSB-first 读 H.264 位流
type spsReader struct {
	d   []byte
	pos int
	n   int
}

func (r *spsReader) u(x int) int {
	v := 0
	for i := 0; i < x; i++ {
		if r.pos >= r.n {
			return 0
		}
		v = (v << 1) | int((r.d[r.pos>>3]>>(7-(r.pos&7)))&1)
		r.pos++
	}
	return v
}

func (r *spsReader) ue() int {
	z := 0
	for r.u(1) == 0 {
		z++
		if z > 30 {
			return -1
		}
	}
	if z == 0 {
		return 0
	}
	return (1 << z) - 1 + r.u(z)
}

// stripSpsVui 剥掉 SPS 的色彩 VUI（把 vui_parameters_present_flag 位置 0，
// 前段字段原位保留）。返回剥好的 SPS（含防竞争转义）；
// poc_type==1 等不确定结构返回原样。
// 位级已验证：剥出的 SPS 可被 ffprobe/解码器正常解析（2026-08-28）。
func stripSpsVui(sps []byte) []byte {
	rbsp := rbspUnescape(sps)
	r := &spsReader{d: rbsp, n: len(rbsp) * 8}
	r.u(8) // profile_idc
	r.u(8) // constraint flags + reserved
	r.u(8) // level_idc
	r.ue() // sps_id
	r.ue() // log2_max_frame_num
	switch p := r.ue(); p {
	case 0:
		r.ue() // log2_max_pic_order_cnt_lsb_minus4
	case 1:
		return sps // poc==1 结构分叉，保守不剥
	default:
	}
	r.ue()           // max_num_ref_frames
	r.u(1)           // gaps_in_frame_num
	r.ue()           // pic_width_in_mbs_minus1
	r.ue()           // pic_height_in_map_units_minus1
	if r.u(1) == 0 { // frame_mbs_only_flag
		r.u(1) // mb_adaptive_frame_field
	}
	r.u(1)           // direct_8x8_inference
	if r.u(1) == 1 { // frame_cropping
		r.ue()
		r.ue()
		r.ue()
		r.ue()
	}
	// 此时 r.pos = vui_parameters_present_flag 的位偏移。
	// 前段原位保留 + vui=0 + rbsp_stop + 字节对齐
	pos := r.pos
	bits := make([]int, 0, pos+8)
	for i := 0; i < pos; i++ {
		bits = append(bits, int((rbsp[i/8]>>uint(7-i%8))&1))
	}
	bits = append(bits, 0) // vui_parameters_present_flag = 0
	bits = append(bits, 1) // rbsp_stop_one_bit
	for len(bits)%8 != 0 {
		bits = append(bits, 0)
	}
	out := make([]byte, len(bits)/8)
	for i, b := range bits {
		if b == 1 {
			out[i/8] |= 1 << uint(7-i%8)
		}
	}
	return rbspEscape(out)
}

// rbspUnescape 去除 emulation_prevention_three_byte（00 00 03 → 00 00），得到纯 RBSP 供解析。
func rbspUnescape(nal []byte) []byte {
	out := make([]byte, 0, len(nal))
	for i := 0; i < len(nal); i++ {
		if i+2 < len(nal) && nal[i] == 0 && nal[i+1] == 0 && nal[i+2] == 3 {
			out = append(out, 0, 0)
			i += 2 // 跳过 03（循环 i++ 再前进一次）
			continue
		}
		out = append(out, nal[i])
	}
	return out
}

// rbspEscape 对 RBSP 施加 emulation_prevention_three_byte，得到合法的 SPS NAL 负载。
func rbspEscape(rbsp []byte) []byte {
	out := make([]byte, 0, len(rbsp)+8)
	zeros := 0
	for _, b := range rbsp {
		if zeros >= 2 && b <= 3 {
			out = append(out, 3)
			zeros = 0
		}
		out = append(out, b)
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return out
}
