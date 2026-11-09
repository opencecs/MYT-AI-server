package rpa

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// PkgData 协议数据包
type PkgData struct {
	buf    *bytes.Buffer
	offset int
}

// NewPkgData 创建PkgData
// 如果data为nil，创建新的空包；否则解析已有数据
func NewPkgData(data []byte) *PkgData {
	if data == nil {
		buf := bytes.NewBuffer(nil)
		// 写入Magic
		writeInt(buf, Magic)
		return &PkgData{buf: buf, offset: 4}
	}
	return &PkgData{buf: bytes.NewBuffer(data), offset: 4}
}

// WrapInt 包装整数
// C++实现: 把int作为data包装，格式为: len(4) + data(4) = 8字节
func (p *PkgData) WrapInt(val int) {
	// 写长度 4
	writeInt(p.buf, 4)
	// 写数据
	writeInt(p.buf, val)
}

// WrapData 包装数据
func (p *PkgData) WrapData(data []byte) {
	if data == nil {
		writeInt(p.buf, 0)
		return
	}
	writeInt(p.buf, len(data))
	p.buf.Write(data)
}

// WrapString 包装字符串
func (p *PkgData) WrapString(s string) {
	p.WrapData([]byte(s))
}

// UnwrapInt 解包整数
// C++实现: 先读长度(应该是4)，再读数据
func (p *PkgData) UnwrapInt() (int, error) {
	// 先读长度
	if p.offset+4 > p.buf.Len() {
		return 0, fmt.Errorf("no enough data for length")
	}
	data := p.buf.Bytes()[p.offset:]
	dataLen := readInt(data)
	p.offset += 4

	if dataLen != 4 {
		return 0, fmt.Errorf("invalid int data length: %d", dataLen)
	}

	// 再读数据
	if p.offset+4 > p.buf.Len() {
		return 0, fmt.Errorf("no enough data for int value")
	}
	data = p.buf.Bytes()[p.offset:]
	val := readInt(data)
	p.offset += 4

	return val, nil
}

// UnwrapData 解包数据
// C++实现: 每次调用都先检查Magic头，再从offset读取长度和数据
func (p *PkgData) UnwrapData() ([]byte, error) {
	// 检查Magic（与C++ UnWrapData一致）
	if p.buf.Len() < 4 {
		return nil, fmt.Errorf("no enough data for magic")
	}
	magic := readInt(p.buf.Bytes())
	if magic != Magic {
		return nil, fmt.Errorf("invalid magic: 0x%08x", uint32(magic))
	}

	// 读4字节长度
	if p.offset+4 > p.buf.Len() {
		return nil, fmt.Errorf("no enough data for length")
	}
	dataLen := readInt(p.buf.Bytes()[p.offset:])
	p.offset += 4

	if dataLen < 0 {
		return nil, fmt.Errorf("invalid data length: %d", dataLen)
	}
	if dataLen == 0 {
		return nil, nil
	}
	if p.offset+dataLen > p.buf.Len() {
		return nil, fmt.Errorf("no enough data for %d bytes", dataLen)
	}
	data := p.buf.Bytes()[p.offset : p.offset+dataLen]
	p.offset += dataLen
	return data, nil
}

// UnwrapString 解包字符串
func (p *PkgData) UnwrapString() (string, error) {
	data, err := p.UnwrapData()
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Bytes 获取字节数据
func (p *PkgData) Bytes() ([]byte, error) {
	return p.buf.Bytes(), nil
}

// writeInt 写入整数（小端序）
func writeInt(buf *bytes.Buffer, val int) {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(val))
	buf.Write(b)
}

// readInt 读取整数（小端序）
func readInt(data []byte) int {
	if len(data) < 4 {
		return 0
	}
	return int(binary.LittleEndian.Uint32(data))
}
