package v4term

import "encoding/base64"

// base64Encode base64 编码（Basic Auth 值）
func base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}
