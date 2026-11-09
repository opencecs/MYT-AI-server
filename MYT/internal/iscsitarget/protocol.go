package iscsitarget

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"net"
	"sort"
	"strings"
)

// iSCSI Opcode
const (
	OpNopOut      = 0x00
	OpScsiCmd     = 0x01
	OpScsiTaskMgt = 0x02
	OpLoginReq    = 0x03
	OpTextReq     = 0x04
	OpScsiDataOut = 0x05
	OpLogoutReq   = 0x06

	OpNopIn           = 0x20
	OpScsiResp        = 0x21
	OpScsiTaskMgtResp = 0x22
	OpLoginResp       = 0x23
	OpTextResp        = 0x24
	OpScsiDataIn      = 0x25
	OpLogoutResp      = 0x26
	OpR2T             = 0x31
	OpAsyncMsg        = 0x32
	OpReject          = 0x3f
)

// Login Status
const (
	LoginSuccess      = 0x00
	LoginRedir        = 0x01
	LoginInitiatorErr = 0x02
	LoginTargetErr    = 0x03
	LoginVersionErr   = 0x05
)

// Login Stage
const (
	StageSecurityNegotiation = 0
	StageOperationalNegot    = 1
	StageFullFeature         = 3
)

// Logout Reason
const (
	LogoutCloseSession = 0
	LogoutCloseConns   = 1
	LogoutRecovery     = 2
)

// BHS 字段偏移
const (
	BHSByte0      = 0
	BHSByte1      = 1
	BHSByte2      = 2
	BHSByte3      = 3
	BSHDataSegLen = 4
	BHSLUN        = 8
	BHSITT        = 16
	BSHTTT        = 20
	BHSStatsn     = 24
	BHSEXPCmdsn   = 28
	BHSMaxCmdsn   = 32
)

const BHSLength = 48

// 默认参数
const (
	// DefaultMaxRecvDataSegmentLength: Target 默认能接收/发送的单 PDU 数据段大小。
	// 65536→262144：大磁盘(600G+)的 ext4 块组描述符表约 300KB，
	// 用 64KB 分片需要 5 个 PDU，用 256KB 只需 2 个，减少 PDU 数量和网络往返。
	DefaultMaxRecvDataSegmentLength = 262144
	DefaultFirstBurstLength         = 262144
	DefaultMaxBurstLength           = 16777216
	DefaultMaxOutstandingR2T        = 1
	VersionMin                      = 0x00
	VersionMax                      = 0x00
)

// BHS iSCSI Basic Header Segment
type BHS [BHSLength]byte

func (b *BHS) Opcode() byte     { return b[BHSByte0] & 0x3f }
func (b *BHS) FBit() bool       { return b[BHSByte0]&0x40 != 0 }
func (b *BHS) ITT() uint32      { return binary.BigEndian.Uint32(b[BHSITT:]) }
func (b *BHS) TTT() uint32      { return binary.BigEndian.Uint32(b[BSHTTT:]) }
func (b *BHS) StatSN() uint32   { return binary.BigEndian.Uint32(b[BHSStatsn:]) }
func (b *BHS) ExpCmdSN() uint32 { return binary.BigEndian.Uint32(b[BHSEXPCmdsn:]) }
func (b *BHS) MaxCmdSN() uint32 { return binary.BigEndian.Uint32(b[BHSMaxCmdsn:]) }
func (b *BHS) DataSegLen() uint32 {
	return binary.BigEndian.Uint32(b[BSHDataSegLen:]) & 0x00ffffff
}
func (b *BHS) TotalAHSLen() byte { return b[4] }
func (b *BHS) LUN() uint64       { return binary.BigEndian.Uint64(b[BHSLUN:]) }

func (b *BHS) LoginVersionMin() byte { return b[BHSByte2] }
func (b *BHS) LoginVersionMax() byte { return b[BHSByte3] }
func (b *BHS) LoginStage() byte      { return (b[BHSByte1] >> 2) & 0x03 }
func (b *BHS) LoginNSG() byte        { return b[BHSByte1] & 0x03 }
func (b *BHS) LoginCSG() byte        { return (b[BHSByte1] >> 2) & 0x03 }

func (b *BHS) ScsiLUN() uint64        { return binary.BigEndian.Uint64(b[BHSLUN:]) }
func (b *BHS) ScsiExpDataLen() uint32 { return binary.BigEndian.Uint32(b[20:]) }
func (b *BHS) ScsiCmdSN() uint32      { return binary.BigEndian.Uint32(b[24:]) }
func (b *BHS) ScsiCDB() []byte        { return b[32:48] }
func (b *BHS) LogoutReason() byte     { return b[BHSByte1] & 0x7f }

func (b *BHS) SetOpcode(op byte) { b[BHSByte0] = op }
func (b *BHS) SetFBit(f bool) {
	if f {
		b[BHSByte0] |= 0x40
	} else {
		b[BHSByte0] &^= 0x40
	}
}
func (b *BHS) SetITT(itt uint32)         { binary.BigEndian.PutUint32(b[BHSITT:], itt) }
func (b *BHS) SetTTT(ttt uint32)         { binary.BigEndian.PutUint32(b[BSHTTT:], ttt) }
func (b *BHS) SetStatSN(sn uint32)       { binary.BigEndian.PutUint32(b[BHSStatsn:], sn) }
func (b *BHS) SetExpCmdSN(sn uint32)     { binary.BigEndian.PutUint32(b[BHSEXPCmdsn:], sn) }
func (b *BHS) SetMaxCmdSN(sn uint32)     { binary.BigEndian.PutUint32(b[BHSMaxCmdsn:], sn) }
func (b *BHS) SetLUN(lun uint64)         { binary.BigEndian.PutUint64(b[BHSLUN:], lun) }
func (b *BHS) SetScsiResponse(resp byte) { b[BHSByte1] = (b[BHSByte1] & 0xFC) | (resp & 0x03) }
func (b *BHS) SetScsiStatus(status byte) { b[BHSByte3] = status }

func (b *BHS) SetDataSegLen(n uint32) {
	// 只写 byte 5/6/7（DataSegmentLength 24-bit），保留 byte 4 的 TotalAHSLength
	b[5] = byte(n >> 16)
	b[6] = byte(n >> 8)
	b[7] = byte(n)
}

func (b *BHS) SetLoginVersion(min, max byte) {
	b[BHSByte2] = max
	b[BHSByte3] = min
}

func (b *BHS) SetLoginStage(csg, nsg byte) {
	b[BHSByte1] = (b[BHSByte1] & 0xf0) | ((csg & 0x03) << 2) | (nsg & 0x03)
}

func (b *BHS) SetLoginStatus(status byte) {
	b[36] = status
	b[37] = 0
}

func (b *BHS) SetLogoutReason(reason byte) { b[BHSByte1] = (b[BHSByte1] & 0x80) | (reason & 0x7F) }

func (b *BHS) String() string {
	return fmt.Sprintf("Opcode=0x%02x ITT=%d TTT=%d DataLen=%d",
		b.Opcode(), b.ITT(), b.TTT(), b.DataSegLen())
}

// PDU 完整的 iSCSI Protocol Data Unit
type PDU struct {
	BasicHeader  BHS
	AHS          []byte
	HeaderDigest uint32
	Data         []byte
	DataDigest   uint32
}

// ReadPDU 从连接读取一个完整的 PDU
func ReadPDU(conn net.Conn, headerDigest, dataDigest bool) (*PDU, error) {
	pdu := &PDU{}

	if _, err := io.ReadFull(conn, pdu.BasicHeader[:]); err != nil {
		return nil, fmt.Errorf("read BHS: %w", err)
	}

	if headerDigest {
		var buf [4]byte
		if _, err := io.ReadFull(conn, buf[:]); err != nil {
			return nil, fmt.Errorf("read header digest: %w", err)
		}
		pdu.HeaderDigest = binary.BigEndian.Uint32(buf[:])
	}

	ahsLen := int(pdu.BasicHeader.TotalAHSLen()) * 4
	if ahsLen > 0 {
		pdu.AHS = make([]byte, ahsLen)
		if _, err := io.ReadFull(conn, pdu.AHS); err != nil {
			return nil, fmt.Errorf("read AHS: %w", err)
		}
	}

	dataLen := int(pdu.BasicHeader.DataSegLen())
	if dataLen > 0 {
		paddedLen := (dataLen + 3) & ^3
		buf := make([]byte, paddedLen)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return nil, fmt.Errorf("read data segment: %w", err)
		}
		pdu.Data = buf[:dataLen]
	}

	if dataDigest && dataLen > 0 {
		var buf [4]byte
		if _, err := io.ReadFull(conn, buf[:]); err != nil {
			return nil, fmt.Errorf("read data digest: %w", err)
		}
		pdu.DataDigest = binary.BigEndian.Uint32(buf[:])
	}

	return pdu, nil
}

// WritePDU 向连接写入一个完整的 PDU
func WritePDU(conn net.Conn, pdu *PDU, headerDigest, dataDigest bool) error {
	pdu.BasicHeader.SetDataSegLen(uint32(len(pdu.Data)))

	if _, err := conn.Write(pdu.BasicHeader[:]); err != nil {
		return fmt.Errorf("write BHS: %w", err)
	}

	if headerDigest {
		digest := CRC32C(pdu.BasicHeader[:])
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], digest)
		if _, err := conn.Write(buf[:]); err != nil {
			return fmt.Errorf("write header digest: %w", err)
		}
	}

	if len(pdu.Data) > 0 {
		if _, err := conn.Write(pdu.Data); err != nil {
			return fmt.Errorf("write data: %w", err)
		}
		padLen := (4 - len(pdu.Data)%4) % 4
		if padLen > 0 {
			padding := make([]byte, padLen)
			if _, err := conn.Write(padding); err != nil {
				return fmt.Errorf("write padding: %w", err)
			}
		}
		if dataDigest {
			digest := CRC32C(pdu.Data)
			var buf [4]byte
			binary.BigEndian.PutUint32(buf[:], digest)
			if _, err := conn.Write(buf[:]); err != nil {
				return fmt.Errorf("write data digest: %w", err)
			}
		}
	}

	return nil
}

var crc32cTable = crc32.MakeTable(crc32.Castagnoli)

func CRC32C(data []byte) uint32 {
	return crc32.Checksum(data, crc32cTable)
}

func ParseKeyValue(data []byte) map[string]string {
	kv := make(map[string]string)
	text := string(data)
	for _, entry := range strings.Split(text, "\x00") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		idx := strings.Index(entry, "=")
		if idx < 0 {
			continue
		}
		kv[entry[:idx]] = entry[idx+1:]
	}
	return kv
}

func BuildKeyValue(kv map[string]string) []byte {
	// 排序 key 保证 Login 参数顺序稳定（RFC 7143 不要求特定顺序，但确定性顺序有利于调试和兼容性）
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf strings.Builder
	for _, k := range keys {
		buf.WriteString(k)
		buf.WriteByte('=')
		buf.WriteString(kv[k])
		buf.WriteByte(0x00)
	}
	return []byte(buf.String())
}
