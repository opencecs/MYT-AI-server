package util

import (
	"encoding/json"
	"testing"
)

// FlexInt 必须兼容云 API 的四种形态：number / "number" / null / ""，
// 否则 get_mirror_list 响应里 spid:"0" 一类字段会让整个镜像列表解析失败。
func TestFlexIntUnmarshal(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want FlexInt
	}{
		{"数字字面量", `{"v": 1}`, 1},
		{"字符串数字", `{"v": "0"}`, 0},
		{"字符串数字2", `{"v": "13"}`, 13},
		{"null", `{"v": null}`, 0},
		{"空字符串", `{"v": ""}`, 0},
		{"浮点形态", `{"v": "2.0"}`, 2},
		{"非法内容按0", `{"v": "abc"}`, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var m struct {
				V FlexInt `json:"v"`
			}
			if err := json.Unmarshal([]byte(c.raw), &m); err != nil {
				t.Fatalf("unmarshal 失败: %v", err)
			}
			if m.V != c.want {
				t.Fatalf("got %d want %d", m.V, c.want)
			}
		})
	}
}

// 镜像列表真实响应形态（V1/V2 混合）：type/sort/spid 为字符串数字也要能整包解析
func TestMirrorItemWithFlexInt(t *testing.T) {
	raw := `{"id":"566","name":"P17","url":"registry.example/x","ttype":"p1_v3","ttype2":["p1_v3"],"os_ver":"and17","sys_ver":"5","sort":"7","type":"1","state":"1","spid":"0"}`
	var m MirrorItem
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("镜像解析失败: %v", err)
	}
	if m.Type != 1 || m.Sort != 7 || m.Spid != 0 || m.State != 1 {
		t.Fatalf("字段值异常: type=%d sort=%d spid=%d state=%d", m.Type, m.Sort, m.Spid, m.State)
	}
}
