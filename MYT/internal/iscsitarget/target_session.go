package iscsitarget

import (
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)

func (s *Session) handleFullFeature(target *Target) {
	for s.Active {
		s.Conn.SetReadDeadline(time.Now().Add(idleTimeout))
		pdu, err := ReadPDU(s.Conn, s.HeaderDig, s.DataDig)
		s.Conn.SetReadDeadline(time.Time{})
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// idle 超时：发 NOP-In ping 探测，不直接断开
				if !s.sendNOPInPing() {
					log.Printf("[iSCSI Target] session idle timeout, NOP-In ping failed from %s", s.Conn.RemoteAddr())
					return
				}
				continue // NOP-In 发成功，继续等下一轮
			}
			log.Printf("[iSCSI Target] read PDU error: %v", err)
			return
		}

		switch pdu.BasicHeader.Opcode() {
		case OpScsiCmd:
			s.handleSCSICmd(pdu)
		case OpScsiTaskMgt:
			s.handleScsiTaskManagement(pdu)
		case OpNopOut:
			s.handleNopOut(pdu)
		case OpLogoutReq:
			s.handleLogout(pdu)
			return
		case OpTextReq:
			s.handleText(pdu, target)
		default:
			log.Printf("[iSCSI Target] unhandled opcode 0x%02x", pdu.BasicHeader.Opcode())
		}
	}
}

// sendNOPInPing 发送 unsolicited NOP-In 作为 keepalive 探测
func (s *Session) sendNOPInPing() bool {
	respBHS := BHS{}
	respBHS.SetOpcode(OpNopIn)
	respBHS[1] |= 0x80 // F-bit
	respBHS.SetITT(0xffffffff)
	respBHS.SetTTT(0xffffffff) // unsolicited NOP-In
	s.StatSN++
	respBHS.SetStatSN(s.StatSN)
	respBHS.SetExpCmdSN(s.ExpCmdSN)
	respBHS.SetMaxCmdSN(s.MaxCmdSN)

	if err := WritePDU(s.Conn, &PDU{BasicHeader: respBHS}, s.HeaderDig, s.DataDig); err != nil {
		log.Printf("[iSCSI Target] write NOP-In ping: %v", err)
		return false
	}
	return true
}

func (s *Session) handleNopOut(pdu *PDU) {
	respBHS := BHS{}
	respBHS.SetOpcode(OpNopIn)
	respBHS[1] |= 0x80
	respBHS.SetITT(pdu.BasicHeader.ITT())
	respBHS.SetTTT(0xffffffff)
	s.StatSN++
	respBHS.SetStatSN(s.StatSN)
	respBHS.SetExpCmdSN(s.ExpCmdSN)
	respBHS.SetMaxCmdSN(s.MaxCmdSN)
	if err := WritePDU(s.Conn, &PDU{BasicHeader: respBHS}, s.HeaderDig, s.DataDig); err != nil {
		log.Printf("[iSCSI Target] write nop-in: %v", err)
		s.Active = false
	}
}

func (s *Session) handleLogout(pdu *PDU) {
	respBHS := BHS{}
	respBHS.SetOpcode(OpLogoutResp)
	respBHS[1] |= 0x80
	respBHS.SetITT(pdu.BasicHeader.ITT())
	respBHS.SetLogoutReason(0)
	respBHS.SetScsiResponse(0)
	s.StatSN++
	respBHS.SetStatSN(s.StatSN)
	WritePDU(s.Conn, &PDU{BasicHeader: respBHS}, s.HeaderDig, s.DataDig)
	s.Active = false
	log.Printf("[iSCSI Target] session logout from %s", s.Conn.RemoteAddr())
}

func (s *Session) handleText(pdu *PDU, target *Target) {
	respBHS := BHS{}
	respBHS.SetOpcode(OpTextResp)
	respBHS[1] |= 0x80
	respBHS.SetITT(pdu.BasicHeader.ITT())
	respBHS.SetTTT(0xffffffff)
	s.StatSN++
	respBHS.SetStatSN(s.StatSN)
	respBHS.SetExpCmdSN(s.ExpCmdSN)
	respBHS.SetMaxCmdSN(s.MaxCmdSN)

	var respData []byte
	if len(pdu.Data) > 0 {
		kv := ParseKeyValue(pdu.Data)
		if st, ok := kv["SendTargets"]; ok && st == "All" {
			respData = target.buildSendTargetsResponse(s)
		}
	}

	respBHS.SetDataSegLen(uint32(len(respData)))
	WritePDU(s.Conn, &PDU{BasicHeader: respBHS, Data: respData}, s.HeaderDig, s.DataDig)
}

// buildSendTargetsResponse 构建 SendTargets 响应（所有 IQN + 对应地址）
func (t *Target) buildSendTargetsResponse(s *Session) []byte {
	var addrStr string
	if addr, ok := s.Conn.LocalAddr().(*net.TCPAddr); ok {
		port := strings.Split(t.Portal, ":")
		p := "3260"
		if len(port) >= 2 {
			p = port[len(port)-1]
		}
		addrStr = fmt.Sprintf("%s:%s,1", addr.IP.String(), p)
	}

	t.mu.RLock()
	entries := make([]*TargetEntry, len(t.entryList))
	copy(entries, t.entryList)
	t.mu.RUnlock()

	var buf strings.Builder
	for _, e := range entries {
		buf.WriteString("TargetName=")
		buf.WriteString(e.IQN)
		buf.WriteByte(0x00)
		if addrStr != "" {
			buf.WriteString("TargetAddress=")
			buf.WriteString(addrStr)
			buf.WriteByte(0x00)
		}
	}
	return []byte(buf.String())
}

func (s *Session) handleScsiTaskManagement(pdu *PDU) {
	respBHS := BHS{}
	respBHS.SetOpcode(OpScsiTaskMgtResp)
	respBHS[1] |= 0x80
	respBHS.SetITT(pdu.BasicHeader.ITT())
	respBHS.SetStatSN(s.StatSN + 1)
	respBHS.SetExpCmdSN(s.ExpCmdSN)
	respBHS.SetMaxCmdSN(s.MaxCmdSN)
	respBHS[2] = 0x00 // Function complete
	s.StatSN++
	if err := WritePDU(s.Conn, &PDU{BasicHeader: respBHS}, s.HeaderDig, s.DataDig); err != nil {
		log.Printf("[iSCSI Target] write task management response: %v", err)
	}
}
