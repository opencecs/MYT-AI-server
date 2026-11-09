// Package adbterm 实现最小 ADB 协议客户端，为 V2 云机容器提供真交互式终端。
//
// 背景：V2 体系的 81 /and_api/v1/shell 是一次性 HTTP exec（命令结束才返回输出），
// 无法承载 logcat / top 这类流式命令；而容器的 adb 端口（容器 5555 → 主机 5000+index）
// 支持原生 ADB 协议——实测握手无需认证（adbd 报 authentication not required）、
// root 权限、支持 shell_v2（stdout/stderr 分离 + 退出码 + 窗口大小）。
package adbterm

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
)

// ADB 协议命令（小端 ASCII 魔数）
const (
	cmdCNXN = 0x4e584e43 // "CNXN" 连接/版本协商
	cmdAUTH = 0x48545541 // "AUTH" 认证请求
	cmdOPEN = 0x4e45504f // "OPEN" 打开服务流
	cmdOKAY = 0x59414b4f // "OKAY" 确认
	cmdWRTE = 0x45545257 // "WRTE" 数据
	cmdCLSE = 0x45534c43 // "CLSE" 关闭流
)

// shell_v2 子协议消息类型（WRTE 载荷首字节）
const (
	shStdin      = 0 // 客户端 → 设备：标准输入
	shStdout     = 1 // 设备 → 客户端：标准输出
	shStderr     = 2 // 设备 → 客户端：标准错误
	shExit       = 3 // 设备 → 客户端：退出码
	shCloseStdin = 4 // 设备 → 客户端：不再接受输入
	shWindowSize = 5 // 客户端 → 设备：窗口大小
)

const (
	// adbVersion 协议版本（0x01000001 = 1.0.31）
	adbVersion = 0x01000001
	// maxPayload 单包最大数据长度
	maxPayload = 256 * 1024
	// headerLen ADB 包头长度
	headerLen = 24
)

// packet ADB 协议包
type packet struct {
	cmd  uint32
	arg0 uint32
	arg1 uint32
	data []byte
}

// readPacket 读取一个完整包（24 字节头 + 载荷）
func readPacket(r io.Reader) (*packet, error) {
	head := make([]byte, headerLen)
	if _, err := io.ReadFull(r, head); err != nil {
		return nil, err
	}
	size := binary.LittleEndian.Uint32(head[12:])
	if size > maxPayload {
		return nil, fmt.Errorf("adb 包载荷异常: %d", size)
	}
	p := &packet{
		cmd:  binary.LittleEndian.Uint32(head[0:]),
		arg0: binary.LittleEndian.Uint32(head[4:]),
		arg1: binary.LittleEndian.Uint32(head[8:]),
	}
	if size > 0 {
		p.data = make([]byte, size)
		if _, err := io.ReadFull(r, p.data); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// writePacket 写入一个完整包
func writePacket(w io.Writer, cmd, arg0, arg1 uint32, data []byte) error {
	head := make([]byte, headerLen)
	binary.LittleEndian.PutUint32(head[0:], cmd)
	binary.LittleEndian.PutUint32(head[4:], arg0)
	binary.LittleEndian.PutUint32(head[8:], arg1)
	binary.LittleEndian.PutUint32(head[12:], uint32(len(data)))
	binary.LittleEndian.PutUint32(head[16:], crc32.ChecksumIEEE(data))
	binary.LittleEndian.PutUint32(head[20:], cmd^0xffffffff)
	if _, err := w.Write(head); err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	_, err := w.Write(data)
	return err
}

// shellMessage shell_v2 消息编码（1 字节类型 + 4 字节长度 + 数据）
func shellMessage(id byte, data []byte) []byte {
	out := make([]byte, 5+len(data))
	out[0] = id
	binary.LittleEndian.PutUint32(out[1:], uint32(len(data)))
	copy(out[5:], data)
	return out
}

// parseShellMessage 解析 shell_v2 消息；返回类型与数据
func parseShellMessage(data []byte) (byte, []byte, bool) {
	if len(data) < 5 {
		return 0, nil, false
	}
	size := binary.LittleEndian.Uint32(data[1:])
	if int(size) > len(data)-5 {
		return 0, nil, false
	}
	return data[0], data[5 : 5+size], true
}

// windowSize 窗口大小消息数据（rows/cols/xpix/ypix 各 2 字节小端）
func windowSize(rows, cols int) []byte {
	out := make([]byte, 8)
	binary.LittleEndian.PutUint16(out[0:], uint16(rows))
	binary.LittleEndian.PutUint16(out[2:], uint16(cols))
	return out
}

// cnxnPayload 构造 CNXN 载荷（版本 + 最大包长 + 身份串 + 特性串）
func cnxnPayload(features string) []byte {
	out := make([]byte, 0, 64+len(features))
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, adbVersion)
	out = append(out, buf...)
	binary.LittleEndian.PutUint32(buf, maxPayload)
	out = append(out, buf...)
	out = append(out, []byte("host::\x00")...)
	out = append(out, []byte(features+"\x00")...)
	return out
}
