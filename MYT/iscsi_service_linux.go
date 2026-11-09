//go:build linux

package main

// isVirtualNic 是 Linux 桩（对齐 iscsi_service_darwin.go 的签名）：判断网卡是否
// 为虚拟网卡。Linux 最小实现恒返 false（不过滤），所有非回环 Up 网卡都出现在
// iSCSI 绑定的候选网卡列表中，由用户自行选择。
func isVirtualNic(name, mac string) bool {
	return false
}
