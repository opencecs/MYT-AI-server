package main

import (
	"strings"
	"time"

	gortsplib "github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/pion/rtp"
)

// fixBadAACConfig replaces zero or near-zero AAC config values in an fmtp line.
// e.g. "config=0" or "config=0000" -> "config=1210"
func fixBadAACConfig(line string) string {
	const marker = "config="
	idx := strings.Index(line, marker)
	if idx < 0 {
		return line
	}
	start := idx + len(marker)
	end := start
	for end < len(line) && line[end] != ';' && line[end] != ' ' && line[end] != '\r' {
		end++
	}
	val := line[start:end]
	allZero := len(val) > 0
	for _, ch := range val {
		if ch != '0' {
			allZero = false
			break
		}
	}
	if allZero {
		return line[:start] + "1210" + line[end:]
	}
	return line
}

// sanitizeSDP modifies SDP media sections so gortsplib can parse them:
//  1. "lgcmd" media type -> renamed to "application" (gortsplib rejects unknown types)
//  2. Audio sections with bad AAC config (config=0) -> fixed to config=1210
func sanitizeSDP(sdp []byte) []byte {
	type section struct {
		lines     []string
		isAudio   bool
		isLgcmd   bool
		badConfig bool
		unknown   bool
	}

	var sections []section
	var cur *section
	var headerLines []string

	for line := range strings.SplitSeq(string(sdp), "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "m=") {
			sections = append(sections, section{})
			cur = &sections[len(sections)-1]

			fields := strings.Fields(strings.TrimPrefix(trimmed, "m="))
			if len(fields) > 0 {
				switch fields[0] {
				case "lgcmd":
					cur.isLgcmd = true
					cur.lines = append(cur.lines, "m=application "+strings.Join(fields[1:], " "))
				default:
					cur.unknown = true
					cur.lines = append(cur.lines, line)
				case "audio":
					cur.isAudio = true
					cur.lines = append(cur.lines, line)
				case "video", "application", "metadata", "text":
					cur.lines = append(cur.lines, line)
				}
			} else {
				cur.lines = append(cur.lines, line)
			}
		} else if cur != nil {
			if cur.isAudio && strings.HasPrefix(trimmed, "a=fmtp:") && strings.Contains(trimmed, "config=") {
				fixed := fixBadAACConfig(trimmed)
				if fixed != trimmed {
					cur.badConfig = true
					cur.lines = append(cur.lines, fixed)
					continue
				}
			}
			cur.lines = append(cur.lines, line)
		} else {
			headerLines = append(headerLines, line)
		}
	}

	out := make([]byte, 0, len(sdp)+64)
	for _, l := range headerLines {
		out = append(out, l...)
		out = append(out, '\n')
	}

	for i, s := range sections {
		if s.unknown && !s.isLgcmd {
			logf("[rtsp] sanitizeSDP: dropping unknown section %d\n", i)
			continue
		}
		for _, l := range s.lines {
			out = append(out, l...)
			out = append(out, '\n')
		}
	}

	return out
}

func (r *RTSPClient) setupH264(c *gortsplib.Client, desc *description.Session, medi *description.Media, forma *format.H264) error {
	rtpDec, err := forma.CreateDecoder()
	if err != nil {
		return err
	}

	if r.OnH264Frame != nil {
		var nalus [][]byte
		if forma.SPS != nil {
			nalus = append(nalus, forma.SPS)
		}
		if forma.PPS != nil {
			nalus = append(nalus, forma.PPS)
		}
		if len(nalus) > 0 {
			r.OnH264Frame(nalus, 0)
		}
	}

	_, err = c.Setup(desc.BaseURL, medi, 0, 0)
	if err != nil {
		return err
	}

	c.OnPacketRTP(medi, forma, func(pkt *rtp.Packet) {
		nalus, err := rtpDec.Decode(pkt)
		if err != nil {
			// 静默 return 原路径保留（ErrMorePacketsNeeded 是正常分片等待）；
			// 其余错误（分片重组失败等）意味着参考链数据缺口，计数进帧门控
			// 状态机（frame_gate.go），与丢包等价处理。
			if err != rtph264.ErrMorePacketsNeeded {
				gateOnDecodeError()
			}
			return
		}

		r.mu.Lock()
		client := r.c
		r.mu.Unlock()
		var pts time.Duration
		if client != nil {
			if ptsNanos, ok := client.PacketPTS(medi, pkt); ok {
				pts = time.Duration(ptsNanos)
			}
		}

		if r.OnH264Frame != nil {
			r.OnH264Frame(nalus, pts)
		}
	})

	return nil
}

func (r *RTSPClient) setupAAC(c *gortsplib.Client, desc *description.Session, medi *description.Media, forma *format.MPEG4Audio) error {
	_, err := c.Setup(desc.BaseURL, medi, 0, 0)
	if err != nil {
		return err
	}

	r.aacSampleRate = forma.ClockRate()
	r.aacChannels = 2
	if forma.Config != nil {
		r.aacChannels = forma.Config.ChannelCount
	}

	rtpDec, err := forma.CreateDecoder()
	if err != nil {
		return err
	}

	c.OnPacketRTP(medi, forma, func(pkt *rtp.Packet) {
		aus, err := rtpDec.Decode(pkt)
		if err != nil {
			return
		}
		for _, au := range aus {
			if r.OnAACFrame != nil {
				r.OnAACFrame(au)
			}
		}
	})

	return nil
}

func (r *RTSPClient) setupLgcmd(c *gortsplib.Client, desc *description.Session, medi *description.Media, forma *format.Generic) error {
	_, err := c.Setup(desc.BaseURL, medi, 0, 0)
	if err != nil {
		return err
	}

	c.OnPacketRTP(medi, forma, func(pkt *rtp.Packet) {
		if r.OnLgcmdData != nil && len(pkt.Payload) > 0 {
			r.OnLgcmdData(pkt.Payload)
		}
	})

	return nil
}
