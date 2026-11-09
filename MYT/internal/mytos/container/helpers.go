package container

import "encoding/base64"

// EncodeBase64 将字节数据编码为 base64 字符串
func EncodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}
