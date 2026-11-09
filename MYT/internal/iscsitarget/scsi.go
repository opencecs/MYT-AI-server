package iscsitarget

import (
	"encoding/binary"
)

// SCSI Opcode
const (
	OpTestUnitReady     = 0x00
	OpRequestSense      = 0x03
	OpInquiry           = 0x12
	OpModeSense6        = 0x1A
	OpStartStopUnit     = 0x1B
	OpPreventAllow      = 0x1E
	OpReadCapacity10    = 0x25
	OpVerify10          = 0x2F
	OpPrefetch10        = 0x34
	OpRead10            = 0x28
	OpWrite10           = 0x2A
	OpSyncCache10       = 0x35
	OpWriteSame10       = 0x41
	OpUnmap             = 0x42
	OpAtaPassThrough16  = 0x85
	OpRead16            = 0x88
	OpWrite16           = 0x8A
	OpVerify16          = 0x8F
	OpPrefetch16        = 0x90
	OpSyncCache16       = 0x91
	OpWriteSame16       = 0x93
	OpServiceActionIn16 = 0x9E
	OpReportLUNs        = 0xA0
	OpMaintenanceIn     = 0xA3
)

const (
	ServiceActionReadCapacity16         = 0x10
	ServiceActionReportSupportedOpcodes = 0x0C
)

// SCSI Status
const (
	StatusGood        = 0x00
	StatusCheckCond   = 0x02
	StatusBusy        = 0x08
	StatusTaskSetFull = 0x28
)

// Sense Key
const (
	SenseNoSense   = 0x00
	SenseNotReady  = 0x02
	SenseMediumErr = 0x03
	SenseHardwErr  = 0x04
	SenseIllReq    = 0x05
	SenseUnitAttn  = 0x06
	SenseDataProt  = 0x07
	SenseAborted   = 0x0B
)

const (
	AscNoAddlSense   = 0x00
	AscLUNNotReady   = 0x04
	AscWriteFault    = 0x03
	AscInvalidOpCode = 0x20
	AscInvalidField  = 0x24
)

func BuildSenseData(senseKey, asc, ascq byte) []byte {
	buf := make([]byte, 18)
	buf[0] = 0x70
	buf[2] = senseKey
	buf[7] = 10
	buf[12] = asc
	buf[13] = ascq
	return buf
}

func BuildInquiry() []byte {
	buf := make([]byte, 96)
	buf[0] = 0x00
	buf[1] = 0x00
	buf[2] = 0x05
	buf[3] = 0x02
	buf[4] = 91
	copy(buf[8:], "MYT     ")
	copy(buf[16:], "iSCSI Disk       ")
	copy(buf[32:], "1.0 ")
	binary.BigEndian.PutUint16(buf[58:], 0x0380)
	binary.BigEndian.PutUint16(buf[60:], 0x0300)
	return buf
}

func BuildReadCapacity10(size int64, blockSize int) []byte {
	buf := make([]byte, 8)
	lba64 := size/int64(blockSize) - 1
	lba := uint32(lba64)
	// 超过 32 位范围时返回最大值，initiator 应使用 READ CAPACITY 16
	if lba64 > 0xFFFFFFFF {
		lba = 0xFFFFFFFF
	}
	binary.BigEndian.PutUint32(buf[0:], lba)
	binary.BigEndian.PutUint32(buf[4:], uint32(blockSize))
	return buf
}

func BuildReadCapacity16(size int64, blockSize int) []byte {
	buf := make([]byte, 32)
	lba := uint64(size/int64(blockSize) - 1)
	binary.BigEndian.PutUint64(buf[0:], lba)
	binary.BigEndian.PutUint32(buf[8:], uint32(blockSize))
	return buf
}

func BuildModeSense6() []byte {
	buf := make([]byte, 4)
	buf[0] = 3
	return buf
}

func BuildReportLUNs(lunIDs []int) []byte {
	buf := make([]byte, 8+len(lunIDs)*8)
	binary.BigEndian.PutUint32(buf[0:], uint32(len(lunIDs)*8))
	for i, id := range lunIDs {
		off := 8 + i*8
		if id < 256 {
			buf[off] = 0x00
			buf[off+1] = byte(id)
		}
	}
	return buf
}

func BuildSupportedOpcodeOne(op byte, serviceAction uint16) []byte {
	buf := make([]byte, 4)
	buf[1] = 0x03 // supported, no command timeout descriptor
	if (op == OpServiceActionIn16 || op == OpMaintenanceIn) && serviceAction != 0 {
		buf[1] |= 0x08 // service action valid
	}
	return buf
}

func BuildSupportedOpcodeList() []byte {
	ops := []struct {
		op            byte
		serviceAction uint16
	}{
		{OpTestUnitReady, 0},
		{OpRequestSense, 0},
		{OpInquiry, 0},
		{OpModeSense6, 0},
		{OpReadCapacity10, 0},
		{OpRead10, 0},
		{OpWrite10, 0},
		{OpSyncCache10, 0},
		{OpUnmap, 0},
		{OpRead16, 0},
		{OpWrite16, 0},
		{OpSyncCache16, 0},
		{OpWriteSame16, 0},
		{OpReportLUNs, 0},
		{OpServiceActionIn16, ServiceActionReadCapacity16},
		{OpMaintenanceIn, ServiceActionReportSupportedOpcodes},
	}
	buf := make([]byte, 4+len(ops)*8)
	binary.BigEndian.PutUint32(buf[0:], uint32(len(ops)*8))
	for i, item := range ops {
		off := 4 + i*8
		buf[off] = item.op
		if item.serviceAction != 0 {
			binary.BigEndian.PutUint16(buf[off+2:], item.serviceAction)
			buf[off+5] = 0x01
		}
	}
	return buf
}

func ParseCDB(cdb []byte) (op byte, lba uint64, xferLen uint32) {
	if len(cdb) < 16 {
		return
	}
	op = cdb[0]
	switch op {
	case OpRead10, OpWrite10, OpWriteSame10:
		lba = uint64(binary.BigEndian.Uint32(cdb[2:6]))
		xferLen = uint32(binary.BigEndian.Uint16(cdb[7:9]))
	case OpRead16, OpWrite16, OpWriteSame16:
		lba = binary.BigEndian.Uint64(cdb[2:10])
		xferLen = binary.BigEndian.Uint32(cdb[10:14])
	case OpServiceActionIn16:
		xferLen = binary.BigEndian.Uint32(cdb[10:14])
	case OpMaintenanceIn:
		xferLen = binary.BigEndian.Uint32(cdb[6:10])
	case OpInquiry:
		xferLen = uint32(binary.BigEndian.Uint16(cdb[3:5]))
	case OpModeSense6:
		xferLen = uint32(cdb[4])
	case OpReportLUNs:
		xferLen = uint32(binary.BigEndian.Uint32(cdb[6:10]))
	}
	return
}
