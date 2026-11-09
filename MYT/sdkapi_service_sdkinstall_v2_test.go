package main

import (
	"errors"
	"testing"
)

// TestIsUploadUnsupportedErr 上传失败降级判定：只有「设备不支持上传接口」类
// 错误（老版本 SDK 404/405/501）才降级 SSH 部署；认证失败/超时/网络错误不降级。
func TestIsUploadUnsupportedErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"404不支持", errors.New("设备不支持上传接口(HTTP 404)，可能为老版本 SDK: Not Found"), true},
		{"纯404", errors.New("上传SDK失败: 404 page not found"), true},
		{"405", errors.New("设备不支持上传接口(HTTP 405)"), true},
		{"认证失败", errors.New("认证失败，请检查设备密码"), false},
		{"超时", errors.New("上传SDK失败: context deadline exceeded"), false},
	}
	for _, c := range cases {
		if got := isUploadUnsupportedErr(c.err); got != c.want {
			t.Errorf("%s: isUploadUnsupportedErr = %v, want %v", c.name, got, c.want)
		}
	}
}
