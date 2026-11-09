package adbterm

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestPacketRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte("hello adb")
	if err := writePacket(&buf, cmdWRTE, 1, 2, payload); err != nil {
		t.Fatalf("writePacket error: %v", err)
	}
	// 包头 24 字节 + 载荷
	if buf.Len() != headerLen+len(payload) {
		t.Fatalf("包长度异常: got %d want %d", buf.Len(), headerLen+len(payload))
	}
	p, err := readPacket(&buf)
	if err != nil {
		t.Fatalf("readPacket error: %v", err)
	}
	if p.cmd != cmdWRTE || p.arg0 != 1 || p.arg1 != 2 {
		t.Fatalf("包头字段异常: cmd=%#x arg0=%d arg1=%d", p.cmd, p.arg0, p.arg1)
	}
	if string(p.data) != string(payload) {
		t.Fatalf("载荷异常: got %q", string(p.data))
	}
}

func TestReadPacketRejectsOversize(t *testing.T) {
	head := make([]byte, headerLen)
	binary.LittleEndian.PutUint32(head[0:], cmdWRTE)
	binary.LittleEndian.PutUint32(head[12:], maxPayload+1)
	if _, err := readPacket(bytes.NewReader(head)); err == nil {
		t.Fatal("超长载荷应报错")
	}
}

func TestShellMessageRoundTrip(t *testing.T) {
	msg := shellMessage(shStdin, []byte("ls -l\n"))
	if len(msg) != 5+6 {
		t.Fatalf("消息长度异常: %d", len(msg))
	}
	id, data, ok := parseShellMessage(msg)
	if !ok || id != shStdin || string(data) != "ls -l\n" {
		t.Fatalf("解析异常: id=%d data=%q ok=%v", id, data, ok)
	}
	// 空数据消息（如 close stdin）
	if _, _, ok := parseShellMessage(shellMessage(shCloseStdin, nil)); !ok {
		t.Fatal("空载荷消息应可解析")
	}
}

func TestParseShellMessageRejectsTruncated(t *testing.T) {
	if _, _, ok := parseShellMessage([]byte{shStdout, 0x10}); ok {
		t.Fatal("截断消息不应解析成功")
	}
	// 声明长度超出实际数据
	bad := []byte{shStdout, 9, 0, 0, 0, 'a'}
	if _, _, ok := parseShellMessage(bad); ok {
		t.Fatal("长度越界不应解析成功")
	}
}

// TestHandleShellPayloadMultipleMessages 设备会把多条 shell_v2 消息打包进同一个
// WRTE 载荷（回显 + 输出 + 提示符），必须逐条解析——只取第一条会丢换行与后续输出。
func TestHandleShellPayloadMultipleMessages(t *testing.T) {
	var got []byte
	var exitCode = -1
	s := &Session{
		v2: true,
		onData: func(stream byte, data []byte) {
			got = append(got, data...)
		},
		onExit: func(code int) { exitCode = code },
	}
	// 真实抓包形态：回显被拆成多条 stdout 消息（"ec"+"ho "+"A;"+" "+"e"+"ch"+"o"+" "+"B\r\r\n"），
	// 随后是输出与提示符，最后是退出码
	payload := []byte{}
	for _, part := range []string{"ec", "ho ", "A;", " ", "e", "ch", "o", " ", "B\r\r\n", "A\r\nB\r\n", "HNLGE:/ # "} {
		payload = append(payload, shellMessage(shStdout, []byte(part))...)
	}
	payload = append(payload, shellMessage(shExit, []byte{0})...)
	s.handleShellPayload(payload)

	want := "echo A; echo B\r\r\nA\r\nB\r\nHNLGE:/ # "
	if string(got) != want {
		t.Fatalf("输出不完整:\n got %q\nwant %q", string(got), want)
	}
	if exitCode != 0 {
		t.Fatalf("退出码异常: %d", exitCode)
	}
}

// TestHandleShellPayloadSplitsStreams 多消息下的 stdout/stderr 分流与残留尾巴处理
func TestHandleShellPayloadSplitsStreams(t *testing.T) {
	var out, errOut []byte
	s := &Session{
		v2: true,
		onData: func(stream byte, data []byte) {
			if stream == shStdout {
				out = append(out, data...)
			} else {
				errOut = append(errOut, data...)
			}
		},
		onExit: func(code int) {},
	}
	payload := append(shellMessage(shStdout, []byte("out\n")), shellMessage(shStderr, []byte("err\n"))...)
	// 故意留 3 字节不完整尾巴，不应 panic
	payload = append(payload, 0x01, 0x05, 0x00)
	s.handleShellPayload(payload)

	if string(out) != "out\n" || string(errOut) != "err\n" {
		t.Fatalf("分流异常: out=%q err=%q", string(out), string(errOut))
	}
}

func TestWindowSizeEncoding(t *testing.T) {
	got := windowSize(30, 100)
	if len(got) != 8 {
		t.Fatalf("窗口大小消息应为 8 字节: %d", len(got))
	}
	if rows := binary.LittleEndian.Uint16(got[0:]); rows != 30 {
		t.Fatalf("rows 异常: %d", rows)
	}
	if cols := binary.LittleEndian.Uint16(got[2:]); cols != 100 {
		t.Fatalf("cols 异常: %d", cols)
	}
}

func TestCNXNPayloadLayout(t *testing.T) {
	payload := cnxnPayload("shell_v2,cmd")
	if len(payload) < 8 {
		t.Fatalf("CNXN 载荷过短: %d", len(payload))
	}
	if v := binary.LittleEndian.Uint32(payload[0:]); v != adbVersion {
		t.Fatalf("协议版本异常: %#x", v)
	}
	if m := binary.LittleEndian.Uint32(payload[4:]); m != maxPayload {
		t.Fatalf("最大包长异常: %d", m)
	}
	rest := payload[8:]
	if !bytes.HasPrefix(rest, []byte("host::\x00")) {
		t.Fatalf("身份串异常: %q", rest)
	}
	if !bytes.Contains(rest, []byte("shell_v2,cmd\x00")) {
		t.Fatalf("特性串异常: %q", rest)
	}
}
