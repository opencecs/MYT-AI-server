package iscsitarget

import (
	"fmt"
	"log"
	"strconv"
	"strings"
)

func (s *Session) handleLogin(target *Target) error {
	stage := byte(StageSecurityNegotiation)
	var isid [6]byte

	for {
		pdu, err := ReadPDU(s.Conn, false, false)
		if err != nil {
			return fmt.Errorf("read login: %w", err)
		}

		if pdu.BasicHeader.Opcode() != OpLoginReq {
			return fmt.Errorf("expected login request, got opcode 0x%02x", pdu.BasicHeader.Opcode())
		}

		kv := ParseKeyValue(pdu.Data)

		if iq, ok := kv["InitiatorName"]; ok {
			s.InitiatorIQN = iq
		}

		copy(isid[:], pdu.BasicHeader[8:14])

		isDiscovery := false
		if st, ok := kv["SessionType"]; ok && st == "Discovery" {
			isDiscovery = true
			s.IsDiscovery = true
		}

			// 非 Discovery 登录需匹配 TargetName（只在首次 login PDU 时检查）
			targetName := kv["TargetName"]
			if !isDiscovery && targetName != "" && s.IQN == "" {
				target.mu.Lock()
				entry, found := target.entries[targetName]
				if !found {
					target.mu.Unlock()
					s.sendLoginReject(pdu, isid, stage)
					return fmt.Errorf("unknown target: %s", targetName)
				}
				if old := target.activeConns[targetName]; old != nil {
					// 踢旧 session：先关闭连接，标记新 session 占位，再等待旧 session 完全退出
					log.Printf("[iSCSI Target] kicking old session for %s (initiator: %s)", targetName, old.InitiatorIQN)
					old.Conn.Close()
					target.activeConns[targetName] = s
					target.mu.Unlock()
					<-old.done // 等待旧 session 完全关闭，避免两个 session 同时操作同一 LUN
				} else {
					target.activeConns[targetName] = s
					target.mu.Unlock()
				}
				s.LUNManager = entry.LUNs
				s.IQN = entry.IQN
			}

		respKV := map[string]string{
			"HeaderDigest": "None",
			"DataDigest":   "None",
		}

		// P5+P13: 安全协商阶段正确处理 AuthMethod
		// 当前未配置 CHAP，统一回 AuthMethod=None，但显式协商而非静默忽略
		if stage == byte(StageSecurityNegotiation) {
			if authMethod, ok := kv["AuthMethod"]; ok {
				// Initiator 提出认证方式列表（如 "None","CHAP"），选择 None
				if strings.Contains(authMethod, "None") {
					respKV["AuthMethod"] = "None"
				} else if strings.Contains(authMethod, "CHAP") {
					// 当前不支持 CHAP，拒绝认证
					s.sendLoginReject(pdu, isid, stage)
					return fmt.Errorf("CHAP authentication not supported")
				} else {
					// 不支持的认证方式
					s.sendLoginReject(pdu, isid, stage)
					return fmt.Errorf("unsupported AuthMethod: %s", authMethod)
				}
			}
		}

		if !isDiscovery {
			respKV["InitialR2T"] = "No"
			respKV["ImmediateData"] = "Yes"
			respKV["ErrorRecoveryLevel"] = "0"
			respKV["MaxOutstandingR2T"] = strconv.Itoa(DefaultMaxOutstandingR2T)
			respKV["DataPDUInOrder"] = "Yes"
			respKV["DataSequenceInOrder"] = "Yes"
			respKV["DefaultTime2Wait"] = "2"
			respKV["DefaultTime2Retain"] = "0"
		}

		// MaxRecvDataSegmentLength: 声明型参数（RFC 7143 13.1）
		// Target 声明自己能接收的最大 PDU 数据段长度。
		// 如果 Initiator 声明了更大的值，Target 可以提升自己的 s.MaxRecvLen
		// （用于 sendDataIn 分片大小，更大的值 = 更少 PDU = 更高吞吐）。
		// 但不能超过协议上限 2^24-1 = 16777215。
		if v, ok := kv["MaxRecvDataSegmentLength"]; ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				initiatorMRDSL := uint32(n)
				if initiatorMRDSL > 16777215 {
					initiatorMRDSL = 16777215
				}
				// 提升 s.MaxRecvLen 到 Initiator 声明的值（如果更大），
				// 这样 sendDataIn 用更大的 chunk，减少大磁盘读取的 PDU 数量
				if initiatorMRDSL > s.MaxRecvLen {
					s.MaxRecvLen = initiatorMRDSL
				}
			}
		}
		respKV["MaxRecvDataSegmentLength"] = strconv.Itoa(int(s.MaxRecvLen))

		if _, ok := kv["HeaderDigest"]; ok {
			respKV["HeaderDigest"] = "None"
		}
		if _, ok := kv["DataDigest"]; ok {
			respKV["DataDigest"] = "None"
		}
		if _, ok := kv["InitialR2T"]; ok {
			respKV["InitialR2T"] = "No"
		}
		if _, ok := kv["ImmediateData"]; ok {
			respKV["ImmediateData"] = "Yes"
		}

		// MaxBurstLength: NumericalMin（RFC 7143 13.1）
		// 每个 R2T burst 的最大数据量
		targetMaxBurst := uint32(16777216) // 16MB
		if v, ok := kv["MaxBurstLength"]; ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				initiatorMaxBurst := uint32(n)
				if initiatorMaxBurst < targetMaxBurst {
					targetMaxBurst = initiatorMaxBurst
				}
			}
		}
		respKV["MaxBurstLength"] = strconv.Itoa(int(targetMaxBurst))

		// FirstBurstLength: NumericalMin（RFC 7143 13.1）
		// 不应超过 MaxBurstLength
		targetFirstBurst := uint32(262144) // 256KB，与 iscsid.conf 默认一致
		if v, ok := kv["FirstBurstLength"]; ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				initiatorFirstBurst := uint32(n)
				if initiatorFirstBurst < targetFirstBurst {
					targetFirstBurst = initiatorFirstBurst
				}
			}
		}
		if targetFirstBurst > targetMaxBurst {
			targetFirstBurst = targetMaxBurst
		}
		respKV["FirstBurstLength"] = strconv.Itoa(int(targetFirstBurst))

		nsg := pdu.BasicHeader.LoginNSG()
		csg := pdu.BasicHeader.LoginCSG()
		isTransit := pdu.BasicHeader[1]&0x80 != 0
		isContinue := pdu.BasicHeader[1]&0x40 != 0

		if csg != stage {
			stage = csg
		}

		if isContinue && !isTransit && nsg > stage {
			isTransit = true
		}

		respBHS := BHS{}
		respBHS.SetOpcode(OpLoginResp)
		respBHS.SetFBit(false)
		respBHS.SetLoginVersion(VersionMin, VersionMax)
		respBHS.SetITT(pdu.BasicHeader.ITT())
		copy(respBHS[8:14], isid[:])

		if isTransit {
			respBHS.SetLoginStage(stage, nsg)
			respBHS.SetLoginStatus(LoginSuccess)
			respBHS[1] |= 0x80
			stage = nsg

			if nsg == byte(StageFullFeature) {
				s.ID = target.nextSID.Add(1)
				s.Active = true
				s.StatSN = 1
				s.ExpCmdSN = pdu.BasicHeader.ScsiCmdSN() + 1
				s.MaxCmdSN = s.ExpCmdSN + 64
			}
		} else {
			nextStage := csg
			if nsg != 0 {
				nextStage = nsg
			}
			respBHS.SetLoginStage(stage, nextStage)
			respBHS.SetLoginStatus(LoginSuccess)
			stage = nextStage
		}

		respBHS.SetStatSN(s.StatSN)
		respBHS.SetExpCmdSN(s.ExpCmdSN)
		respBHS.SetMaxCmdSN(s.MaxCmdSN)

		respData := BuildKeyValue(respKV)
		respBHS.SetDataSegLen(uint32(len(respData)))

		respPDU := &PDU{BasicHeader: respBHS, Data: respData}
		if err := WritePDU(s.Conn, respPDU, s.HeaderDig, s.DataDig); err != nil {
			return fmt.Errorf("write login response: %w", err)
		}

		if isTransit && nsg == byte(StageFullFeature) {
			return nil
		}
	}
}

// sendLoginReject 发送登录拒绝响应
func (s *Session) sendLoginReject(pdu *PDU, isid [6]byte, stage byte) {
	respBHS := BHS{}
	respBHS.SetOpcode(OpLoginResp)
	respBHS.SetFBit(false)
	respBHS.SetLoginVersion(VersionMin, VersionMax)
	respBHS.SetITT(pdu.BasicHeader.ITT())
	copy(respBHS[8:14], isid[:])
	respBHS.SetLoginStage(stage, stage)
	respBHS.SetLoginStatus(LoginInitiatorErr)
	respBHS[1] |= 0x80

	respData := BuildKeyValue(map[string]string{
		"MaxRecvDataSegmentLength": strconv.Itoa(int(s.MaxRecvLen)),
		"HeaderDigest":             "None",
		"DataDigest":               "None",
	})
	respBHS.SetDataSegLen(uint32(len(respData)))
	WritePDU(s.Conn, &PDU{BasicHeader: respBHS, Data: respData}, s.HeaderDig, s.DataDig)
}
