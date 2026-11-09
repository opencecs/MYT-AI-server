package auth

import "testing"

func TestParseCancelUserResponse(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode string
		wantUid  string
		wantNum  int
		wantErr  bool
	}{
		{
			name:     "data为对象",
			body:     `{"code":"200","msg":"ok","data":{"uid":"123","host_num":3}}`,
			wantCode: "200", wantUid: "123", wantNum: 3,
		},
		{
			name:     "data为带元素数组",
			body:     `{"code":"200","msg":"ok","data":[{"uid":"456","host_num":1}]}`,
			wantCode: "200", wantUid: "456", wantNum: 1,
		},
		{
			name:     "data为空数组-仍算成功",
			body:     `{"code":"200","msg":"ok","data":[]}`,
			wantCode: "200",
		},
		{
			name:     "data为false-仍算成功",
			body:     `{"code":"200","msg":"ok","data":false}`,
			wantCode: "200",
		},
		{
			name:     "失败码-透传code与msg",
			body:     `{"code":"40001","msg":"验证码错误","data":[]}`,
			wantCode: "40001",
		},
		{
			name:    "整体不是JSON",
			body:    `not-json`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCancelUserResponse([]byte(tt.body))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Code != tt.wantCode || got.Uid != tt.wantUid || got.HostNum != tt.wantNum {
				t.Fatalf("got %+v, want code=%s uid=%s num=%d", got, tt.wantCode, tt.wantUid, tt.wantNum)
			}
		})
	}
}
