package iscsitarget

import (
	"encoding/binary"
	"log"
	"time"
)

func (s *Session) handleSCSICmd(pdu *PDU) {
	cdb := pdu.BasicHeader.ScsiCDB()
	op, lba, xferLen := ParseCDB(cdb)
	lunID := int(pdu.BasicHeader.ScsiLUN() >> 48)

	s.DataSN = 0
	s.ExpCmdSN = pdu.BasicHeader.ScsiCmdSN() + 1
	s.MaxCmdSN = s.ExpCmdSN + 64

	switch op {
	case OpTestUnitReady:
		s.sendSCSIResponse(pdu, StatusGood, 0)

	case OpInquiry:
		data := BuildInquiry()
		if xferLen > 0 && int(xferLen) < len(data) {
			data = data[:int(xferLen)]
			if len(data) >= 5 {
				data[4] = byte(len(data) - 5)
			}
		}
		s.sendDataIn(pdu, data, true)

	case OpReadCapacity10:
		lun, ok := s.LUNManager.Get(lunID)
		if !ok {
			s.sendSCSIResponse(pdu, StatusCheckCond, SenseNotReady)
			return
		}
		s.sendDataIn(pdu, BuildReadCapacity10(lun.Backend.Size(), BlockSize), true)

	case OpServiceActionIn16:
		switch cdb[1] & 0x1f {
		case ServiceActionReadCapacity16:
			lun, ok := s.LUNManager.Get(lunID)
			if !ok {
				s.sendSCSIResponse(pdu, StatusCheckCond, SenseNotReady)
				return
			}
			s.sendDataIn(pdu, limitData(BuildReadCapacity16(lun.Backend.Size(), BlockSize), xferLen), true)
		default:
			log.Printf("[iSCSI Target] unsupported service action 0x%02x for opcode 0x9e", cdb[1]&0x1f)
			s.sendSCSIResponse(pdu, StatusCheckCond, SenseIllReq)
		}

	case OpModeSense6:
		s.sendDataIn(pdu, BuildModeSense6(), true)

	case OpRequestSense:
		s.sendDataIn(pdu, BuildSenseData(SenseNoSense, 0, 0), true)

	case OpReportLUNs:
		luns := s.LUNManager.List()
		ids := make([]int, len(luns))
		for i, l := range luns {
			ids[i] = l.ID
		}
		s.sendDataIn(pdu, limitData(BuildReportLUNs(ids), xferLen), true)

	case OpMaintenanceIn:
		switch cdb[1] & 0x1f {
		case ServiceActionReportSupportedOpcodes:
			reportingOptions := cdb[2] & 0x07
			if reportingOptions == 0 {
				s.sendDataIn(pdu, limitData(BuildSupportedOpcodeList(), xferLen), true)
			} else {
				requestedOp := cdb[3]
				serviceAction := binary.BigEndian.Uint16(cdb[4:6])
				s.sendDataIn(pdu, limitData(BuildSupportedOpcodeOne(requestedOp, serviceAction), xferLen), true)
			}
		default:
			log.Printf("[iSCSI Target] unsupported maintenance service action 0x%02x", cdb[1]&0x1f)
			s.sendSCSIResponse(pdu, StatusCheckCond, SenseIllReq)
		}

	case OpRead10, OpRead16:
		lun, ok := s.LUNManager.Get(lunID)
		if !ok {
			s.sendSCSIResponse(pdu, StatusCheckCond, SenseNotReady)
			return
		}
		readOff := int64(lba) * BlockSize
		readLen := int64(xferLen) * BlockSize
		if readOff+readLen > lun.Backend.Size() {
			log.Printf("[iSCSI Target] read beyond LUN size: off=%d len=%d size=%d", readOff, readLen, lun.Backend.Size())
			s.sendSCSIResponse(pdu, StatusCheckCond, SenseIllReq)
			return
		}
		// 分批读取+发送，每批不超过 MaxRecvLen，避免大磁盘全盘读取时 OOM
		remaining := readLen
		curOff := readOff
		maxBatch := int64(s.MaxRecvLen)
		if maxBatch == 0 {
			maxBatch = DefaultMaxRecvDataSegmentLength
		}
		batchNum := 0
		for remaining > 0 {
			batchLen := remaining
			if batchLen > maxBatch {
				batchLen = maxBatch
			}
			buf := make([]byte, batchLen)
			if err := lun.Backend.Read(curOff, buf); err != nil {
				log.Printf("[iSCSI Target] read error at %d (batch %d, len=%d): %v", curOff, batchNum, batchLen, err)
				s.sendSCSIResponse(pdu, StatusCheckCond, SenseMediumErr)
				return
			}
			isFinal := remaining-batchLen <= 0
			s.sendDataIn(pdu, buf, isFinal)
			curOff += batchLen
			remaining -= batchLen
			batchNum++
		}

	case OpWrite10, OpWrite16:
		lun, ok := s.LUNManager.Get(lunID)
		if !ok {
			s.sendSCSIResponse(pdu, StatusCheckCond, SenseNotReady)
			return
		}
		offset := int64(lba) * BlockSize
		size := int64(xferLen) * BlockSize
		if offset+size > lun.Backend.Size() {
			log.Printf("[iSCSI Target] write beyond LUN size: off=%d len=%d size=%d", offset, size, lun.Backend.Size())
			s.sendSCSIResponse(pdu, StatusCheckCond, SenseIllReq)
			return
		}
		written := int64(0)

		// Handle Immediate Data
		if len(pdu.Data) > 0 {
			if err := lun.Backend.Write(offset, pdu.Data); err != nil {
				log.Printf("[iSCSI Target] write error (immediate) at %d len=%d: %v", offset, len(pdu.Data), err)
				s.sendSCSIResponse(pdu, StatusCheckCond, SenseMediumErr)
				return
			}
			written = int64(len(pdu.Data))
		}

		// R2T loop: send R2T for remaining data, read Data-Out(s) until fulfilled
		r2tsn := uint32(0)
		for written < size {
			remaining := size - written
			desiredLen := uint32(remaining)
			if desiredLen > 16777216 {
				desiredLen = 16777216
			}

			// Send R2T — TTT 递增，不再硬编码 1
			s.nextTTT++
			if s.nextTTT == 0 {
				s.nextTTT = 1 // TTT=0 保留给 unsolicited data
			}
			r2tBHS := BHS{}
			r2tBHS.SetOpcode(OpR2T)
			r2tBHS.SetFBit(true) // R2T always has F-bit set (RFC 7143 11.8)
			r2tBHS.SetITT(pdu.BasicHeader.ITT())
			r2tBHS.SetTTT(s.nextTTT)
			r2tBHS.SetLUN(pdu.BasicHeader.ScsiLUN())
			s.StatSN++ // R2T consumes a StatSN (RFC 7143 11.4.9)
			r2tBHS.SetStatSN(s.StatSN)
			r2tBHS.SetExpCmdSN(s.ExpCmdSN)
			r2tBHS.SetMaxCmdSN(s.MaxCmdSN)
			binary.BigEndian.PutUint32(r2tBHS[36:], r2tsn)
			binary.BigEndian.PutUint32(r2tBHS[40:], uint32(written))
			binary.BigEndian.PutUint32(r2tBHS[44:], desiredLen)

			r2tPDU := &PDU{BasicHeader: r2tBHS}
			if err := WritePDU(s.Conn, r2tPDU, s.HeaderDig, s.DataDig); err != nil {
				log.Printf("[iSCSI Target] write R2T: %v", err)
				return
			}

			// Read Data-Out(s) until this R2T is fulfilled
			r2tReceived := int64(0)
			for r2tReceived < int64(desiredLen) {
				s.Conn.SetReadDeadline(time.Now().Add(30 * time.Second))
				dataPDU, err := ReadPDU(s.Conn, s.HeaderDig, s.DataDig)
				s.Conn.SetReadDeadline(time.Time{})
				if err != nil {
					log.Printf("[iSCSI Target] read data-out: %v", err)
					return
				}
				if dataPDU.BasicHeader.Opcode() != OpScsiDataOut {
					log.Printf("[iSCSI Target] expected data-out, got 0x%02x", dataPDU.BasicHeader.Opcode())
					return
				}
				if err := lun.Backend.Write(offset+written, dataPDU.Data); err != nil {
					log.Printf("[iSCSI Target] write error at %d: %v", offset+written, err)
					s.sendSCSIResponse(pdu, StatusCheckCond, SenseMediumErr)
					return
				}
				written += int64(len(dataPDU.Data))
				r2tReceived += int64(len(dataPDU.Data))
			}
			r2tsn++
		}
		// FUA（Force Unit Access，CDB byte1 bit3）：写必须落盘后才算完成——
		// mkfs/fsck 写元数据会置位，语义等同每次写后 fsync
		if cdb[1]&0x08 != 0 {
			if err := lun.Backend.Sync(); err != nil {
				log.Printf("[iSCSI Target] FUA sync error (LUN %d): %v", lunID, err)
				s.sendSCSIResponse(pdu, StatusCheckCond, SenseMediumErr)
				return
			}
		}
		s.sendSCSIResponse(pdu, StatusGood, 0)

	case OpWriteSame10, OpWriteSame16, OpUnmap:
		s.sendSCSIResponse(pdu, StatusGood, 0)

	case OpSyncCache10, OpSyncCache16:
		// SYNCHRONIZE CACHE：initiator 的 fsync/ext4 commit 锚点命令，必须真实落盘
		// 后才能回 Good。忽略 CDB 的 LBA/Length 范围（全盘 Sync，简化且合法）；
		// IMMED=0 语义天然满足（Sync 完成后才回状态）
		lun, ok := s.LUNManager.Get(lunID)
		if !ok {
			s.sendSCSIResponse(pdu, StatusCheckCond, SenseNotReady)
			return
		}
		if err := lun.Backend.Sync(); err != nil {
			log.Printf("[iSCSI Target] sync cache error (LUN %d): %v", lunID, err)
			s.sendSCSIResponse(pdu, StatusCheckCond, SenseMediumErr)
			return
		}
		s.sendSCSIResponse(pdu, StatusGood, 0)

	case OpStartStopUnit, OpPreventAllow, OpVerify10, OpVerify16, OpPrefetch10, OpPrefetch16:
		s.sendSCSIResponse(pdu, StatusGood, 0)

	default:
		log.Printf("[iSCSI Target] unsupported SCSI opcode 0x%02x from LUN %d", op, lunID)
		s.sendSCSIResponse(pdu, StatusCheckCond, SenseIllReq)
	}
}

func limitData(data []byte, xferLen uint32) []byte {
	if xferLen > 0 && int(xferLen) < len(data) {
		return data[:int(xferLen)]
	}
	return data
}

func (s *Session) sendDataIn(reqPDU *PDU, data []byte, final bool) {
	offset := uint32(0)
	totalLen := uint32(len(data))
	maxChunk := s.MaxRecvLen
	if maxChunk == 0 {
		maxChunk = DefaultMaxRecvDataSegmentLength
	}

	for offset < totalLen {
		chunkSize := totalLen - offset
		if chunkSize > maxChunk {
			chunkSize = maxChunk
		}
		isFinal := final && (offset+chunkSize >= totalLen)

		respBHS := BHS{}
		respBHS.SetOpcode(OpScsiDataIn)
		if isFinal {
			respBHS[1] |= 0x80 // F-bit
			respBHS[1] |= 0x01 // S-bit (status)
		}
		respBHS.SetITT(reqPDU.BasicHeader.ITT())
		respBHS.SetTTT(0xffffffff)
		respBHS.SetScsiStatus(StatusGood)
		respBHS.SetLUN(reqPDU.BasicHeader.ScsiLUN())
		binary.BigEndian.PutUint32(respBHS[36:], s.DataSN) // DataSN
		s.DataSN++
		binary.BigEndian.PutUint32(respBHS[40:], offset) // BufferOffset（累加，不再硬编码 0）
		if isFinal {
			s.StatSN++
			respBHS.SetStatSN(s.StatSN)
		}
		respBHS.SetExpCmdSN(s.ExpCmdSN)
		respBHS.SetMaxCmdSN(s.MaxCmdSN)

		chunk := data[offset : offset+chunkSize]
		if err := WritePDU(s.Conn, &PDU{BasicHeader: respBHS, Data: chunk}, s.HeaderDig, s.DataDig); err != nil {
			log.Printf("[iSCSI Target] write data-in: %v", err)
			return
		}
		offset += chunkSize
	}
}

func (s *Session) sendSCSIResponse(reqPDU *PDU, status byte, senseKey byte) {
	respBHS := BHS{}
	respBHS.SetOpcode(OpScsiResp)
	respBHS[1] |= 0x80
	respBHS.SetITT(reqPDU.BasicHeader.ITT())
	respBHS.SetTTT(0xffffffff)
	respBHS.SetScsiResponse(0)
	respBHS.SetScsiStatus(status)
	s.StatSN++
	respBHS.SetStatSN(s.StatSN)
	respBHS.SetExpCmdSN(s.ExpCmdSN)
	respBHS.SetMaxCmdSN(s.MaxCmdSN)

	if status != StatusGood && senseKey > 0 {
		senseData := BuildSenseData(senseKey, AscNoAddlSense, 0)
		respBHS.SetDataSegLen(uint32(len(senseData)))
		WritePDU(s.Conn, &PDU{BasicHeader: respBHS, Data: senseData}, s.HeaderDig, s.DataDig)
		return
	}

	WritePDU(s.Conn, &PDU{BasicHeader: respBHS}, s.HeaderDig, s.DataDig)
}
