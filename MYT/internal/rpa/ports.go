package rpa

import (
	"fmt"
	"strings"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/ports"
)

// CalcRPAPort 计算RPA连接地址
// macvlan直连：容器IP + 9083
// 端口映射：宿主机IP + portBindings映射端口（或公式计算）
func CalcRPAPort(android config.AndroidInfo, deviceIP string) ports.Addr {
	if ports.IsMacvlanDirect(android.NetworkName, android.PortBindings) && android.ContainerIP != "" {
		return ports.RPADirect(android)
	}
	return ports.RPA(android, deviceIP)
}

// FormatResult 格式化工具执行结果为 JSON 字符串
func FormatResult(success bool, data map[string]interface{}) string {
	if data == nil {
		data = make(map[string]interface{})
	}
	data["success"] = success
	var parts []string
	for k, v := range data {
		parts = append(parts, fmt.Sprintf("%q:%q", k, fmt.Sprintf("%v", v)))
	}
	return "{" + strings.Join(parts, ",") + "}"
}
