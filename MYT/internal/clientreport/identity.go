package clientreport

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// clientIDBytes 生成 12 位十六进制标识所需的随机字节数
const clientIDBytes = 6

// newClientID 生成 12 位大写十六进制客户端标识（例：A1B2C3D4E5F6）。
//
// 用 crypto/rand 而非时间/机器信息派生：标识只需唯一且稳定（生成后落盘复用），
// 不需要承载任何可被反推的机器特征。
func newClientID() (string, error) {
	buf := make([]byte, clientIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(buf)), nil
}
