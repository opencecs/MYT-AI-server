package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// buildFanoutInjectJS 生成公网矩阵 v2 fanout 注入 JS。
//
// 功能：
//  1. 串行（间隔 1 秒）为每个 fanout URL 建 WebSocket
//  2. 处理 B 端 WebRTC offer（base64 编码 SDP）→ atob 解码 → setRemoteDescription → btoa 编码 answer 发回
//  3. 处理 B 端 icecandiate（base64 编码）→ atob 解码 → addIceCandidate
//  4. ondatachannel → 拿到 DataChannel
//  5. monkey-patch h5_lgair.fun_send_data：复制二进制帧到所有 DataChannel
//  6. monkey-patch h5_lgair.sendCmdEvent：导航按钮也扇出
//
// 所有关键状态通过 chrome.webview.postMessage 回传 Go 侧（{fanout:1,...}）。
func buildFanoutInjectJS(urls []string) string {
	urlList := make([]string, len(urls))
	for i, u := range urls {
		urlList[i] = fmt.Sprintf("%q", u)
	}
	urlsJS := strings.Join(urlList, ",")

	return fmt.Sprintf(`(function(){
  if (window.__mxFanoutInstalled) return;
  window.__mxFanoutInstalled = true;
  window.__mxFanoutWS = [];
  window.__mxFanoutSent = 0;

  function probe(obj) {
    obj.fanout = 1;
    try { window.chrome.webview.postMessage(JSON.stringify(obj)); } catch(e) {}
  }

  var urls = [%s];
  probe({event:'init', targets:urls.length});

  // 处理 B 端 WebRTC：创建 PeerConnection，应答 offer，处理 ICE candidate，
  // 建立 DataChannel 后才能发送二进制控制帧（h5_lgair 协议用 DataChannel 而非 WS）。
  var fanoutIdx = 0;

  function createFanoutPeer(ws, onDcOpen) {
    var idx = ++fanoutIdx;
    var pc = new RTCPeerConnection();
    var sendChannel = null;
    var dcOpened = false;
    var iceRecvCount = 0;
    var iceSentCount = 0;

    pc.onconnectionstatechange = function() {
      probe({event:'pc_state', state:pc.connectionState, idx:idx});
    };
    pc.oniceconnectionstatechange = function() {
      probe({event:'ice_state', state:pc.iceConnectionState, idx:idx});
    };

    pc.ondatachannel = function(e) {
      sendChannel = e.channel;
      sendChannel.onopen = function() {
        ws.__mxDataChannel = sendChannel;
        probe({event:'dc_open', idx:idx});
        if (!dcOpened) {
          dcOpened = true;
          onDcOpen();
        }
      };
      sendChannel.onclose = function() {
        ws.__mxDataChannel = null;
        probe({event:'dc_close', idx:idx});
      };
    };

    pc.onicecandidate = function(e) {
      if (!e.candidate) {
        probe({event:'ice_gather_done', idx:idx, sent:iceSentCount});
        return;
      }
      iceSentCount++;
      var candStr = JSON.stringify(e.candidate.candidate);
      try {
        ws.send(JSON.stringify({id:'icecandiate', data:btoa(JSON.stringify(e.candidate))}));
        probe({event:'ice_sent', idx:idx, n:iceSentCount, cand:candStr});
      } catch(ex) {
        probe({event:'ice_send_err', idx:idx, err:String(ex)});
      }
    };

    ws.__mxPC = pc;

    ws.__mxHandleSignal = function(msg) {
      var decoded = atob(msg.data);
      if (msg.id === 'offer') {
        var sdp = JSON.parse(decoded);
        // 提取 SDP 中的 candidate 行（B端 ICE 候选）
        var candLines = [];
        if (sdp.sdp) {
          var lines = sdp.sdp.split('\r\n');
          for (var i = 0; i < lines.length; i++) {
            if (lines[i].indexOf('a=candidate:') === 0) candLines.push(lines[i].substring(12));
          }
        }
        probe({event:'offer_recv', idx:idx, cands:candLines.length, cand0:candLines[0]||''});
        pc.setRemoteDescription(new RTCSessionDescription(sdp)).then(function() {
          return pc.createAnswer();
        }).then(function(answer) {
          return pc.setLocalDescription(answer);
        }).then(function() {
          try {
            ws.send(JSON.stringify({id:'answer', data:btoa(JSON.stringify(pc.localDescription))}));
            probe({event:'answered', idx:idx});
          } catch(ex) {
            probe({event:'answer_send_err', idx:idx, err:String(ex)});
          }
        }).catch(function(err) {
          probe({event:'webrtc_err', idx:idx, err:String(err)});
        });
      } else if (msg.id === 'icecandiate') {
        iceRecvCount++;
        try {
          var cand = new RTCIceCandidate(JSON.parse(decoded));
          pc.addIceCandidate(cand);
          probe({event:'ice_recv', idx:idx, n:iceRecvCount, cand:decoded.substring(0, 120)});
        } catch(ex) {
          probe({event:'ice_err', idx:idx, err:String(ex)});
        }
      } else {
        probe({event:'signal_unknown', idx:idx, id:msg.id});
      }
    };
  }

  // 1. 串行建立 fanout WebSocket 连接：等前一个 DataChannel open 后再连下一个
  var wsIndex = 0;
  function connectNext() {
    if (wsIndex >= urls.length) return;
    var url = urls[wsIndex++];
    try {
      var ws = new WebSocket(url);
      ws.binaryType = 'arraybuffer';
      ws.onopen = function() { probe({event:'ws_open', url:url}); };
      ws.onclose = function(e) { probe({event:'ws_close', url:url, code:e.code}); };
      ws.onerror = function() { probe({event:'ws_error', url:url}); };
      ws.onmessage = function(e) {
        if (typeof e.data === 'string') {
          try {
            var msg = JSON.parse(e.data);
            if (msg.id === 'offer' || msg.id === 'icecandiate') {
              if (!ws.__mxPC) createFanoutPeer(ws, connectNext);
              ws.__mxHandleSignal(msg);
            } else {
              probe({event:'ws_text', id:msg.id, len:e.data.length});
            }
          } catch(ex) {
            probe({event:'ws_text_raw', len:e.data.length, preview:e.data.substring(0, 80)});
          }
        } else {
          probe({event:'ws_binary', len:e.data.byteLength});
        }
      };
      window.__mxFanoutWS.push(ws);
    } catch(e) {
      probe({event:'ws_fail', url:url, err:String(e)});
      setTimeout(connectNext, 1000);
    }
    // 超时兜底：10 秒后无论 DataChannel 是否 open 都连下一个
    setTimeout(connectNext, 10000);
  }
  connectNext();

  // 2. 等待 h5_lgair 就绪后 monkey-patch fun_send_data + sendCmdEvent
  var patchAttempts = 0;
  function patchLgair() {
    var lg = window.h5_lgair;
    patchAttempts++;
    if (!lg || !lg.fun_send_data || !lg.buffer_view) {
      if (patchAttempts %% 10 === 1) {
        probe({event:'patch_wait', n:patchAttempts, has_lg:!!lg, has_fsd:!!(lg&&lg.fun_send_data), has_bv:!!(lg&&lg.buffer_view)});
      }
      setTimeout(patchLgair, 200);
      return;
    }
    if (lg.__mxFanoutPatched) return;
    lg.__mxFanoutPatched = true;

    probe({event:'patch_found', n:patchAttempts, lg_type:typeof lg, fsd_type:typeof lg.fun_send_data});

    var origSend = lg.fun_send_data.bind(lg);
    lg.fun_send_data = function(a) {
      origSend(a);

      try {
        var buf = lg.buffer_view.buffer;
        var len = 28;
        if (a && a.data && a.data.points != null) {
          len = 40;
        }
        var frame = buf.slice(0, len);
        var ready = 0;
        var dcStates = [];
        window.__mxFanoutWS.forEach(function(ws, i) {
          var state = 'no_dc';
          if (ws.__mxDataChannel) {
            state = ws.__mxDataChannel.readyState;
            if (state === 'open') {
              ws.__mxDataChannel.send(frame);
              ready++;
            }
          } else if (ws.readyState !== 1) {
            state = 'ws_' + ws.readyState;
          }
          dcStates.push(i + ':' + state);
        });
        window.__mxFanoutSent++;
        if (window.__mxFanoutSent %% 100 === 1) {
          probe({event:'sent', count:window.__mxFanoutSent, ready:ready, total:window.__mxFanoutWS.length, dc:dcStates.join(',')});
        }
      } catch(e) {
        probe({event:'send_err', err:String(e)});
      }
    };

    // patch fun_send_data_multi（多点触控/滚轮）
    if (lg.fun_send_data_multi && lg.buffer_view_multi) {
      var origMulti = lg.fun_send_data_multi.bind(lg);
      lg.fun_send_data_multi = function(a) {
        origMulti(a);
        try {
          var buf = lg.buffer_view_multi.buffer;
          var frame = buf.slice(0, 40);
          window.__mxFanoutWS.forEach(function(ws) {
            if (ws.__mxDataChannel && ws.__mxDataChannel.readyState === 'open') ws.__mxDataChannel.send(frame);
          });
        } catch(e) {}
      };
    }

    // patch sendCmdEvent（导航按钮 goHome/goBack/goClean 等）
    if (lg.sendCmdEvent && lg.buffer_view_sys) {
      var origCmd = lg.sendCmdEvent.bind(lg);
      lg.sendCmdEvent = function(cmd) {
        origCmd(cmd);
        try {
          var frame = lg.buffer_view_sys.buffer.slice(0, 20);
          window.__mxFanoutWS.forEach(function(ws) {
            if (ws.__mxDataChannel && ws.__mxDataChannel.readyState === 'open') {
              ws.__mxDataChannel.send(frame);
            }
          });
        } catch(e) {}
      };
    }

    probe({event:'patched', ws_count:window.__mxFanoutWS.length});
  }
  patchLgair();
})();`, urlsJS)
}

// handleFanoutMessage 处理页面回传的 fanout 探针消息
func handleFanoutMessage(raw string) {
	if strings.Contains(raw, `"fanout"`) {
		logf("[fanout] page: %s\n", raw)
	}
}

// parseFanoutURLs parses pipe-separated WebSocket URL list for group control fan-out.
func parseFanoutURLs(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var urls []string
	for _, part := range strings.Split(s, "|") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.HasPrefix(part, "ws://") && !strings.HasPrefix(part, "wss://") {
			logf("[main] invalid fanout URL (must start with ws:// or wss://): %q\n", part)
			continue
		}
		urls = append(urls, part)
	}
	return urls
}

// 保留 groupRPA 相关代码供 batch-rpa 导航按钮使用
// （触控走 fanout DataChannel，导航按钮走 batch-rpa HTTP API）

type GroupRPAConfig struct {
	BaseURL    string
	Token      string
	Containers string
}

type groupRPAMgr struct {
	cfg     GroupRPAConfig
	client  *http.Client
	sentCnt int64
	mu      sync.Mutex
}

var groupRPA *groupRPAMgr

func initGroupRPA(param string) {
	if param == "" {
		return
	}
	parts := strings.SplitN(param, "|", 3)
	if len(parts) != 3 {
		logf("[grouprpa] invalid -grouprpa param\n")
		return
	}
	groupRPA = &groupRPAMgr{
		cfg: GroupRPAConfig{
			BaseURL:    parts[0],
			Token:      parts[1],
			Containers: parts[2],
		},
		client: &http.Client{Timeout: 5 * time.Second},
	}
	logf("[grouprpa] initialized: containers=%s\n", parts[2])
}

func HandleGroupKey(cmd string) {
	if groupRPA == nil {
		return
	}
	containers := strings.Split(groupRPA.cfg.Containers, ",")
	if len(containers) == 0 {
		return
	}
	keyName := navKeyName(cmd)
	if keyName == "" {
		return
	}
	body := map[string]interface{}{"key": keyName}
	go groupRPA.sendBatchRPA("rpa/key", body)
}

// navAction 把侧边栏命令转成方案 C 的 nav action（home/back/recent）。
// 主控经 UDP nav 事件扇出时使用，与 myt-agent __mytControl type:"nav" 对应。
func navAction(cmd string) string {
	switch cmd {
	case "goHome":
		return "home"
	case "goBack":
		return "back"
	case "goClean":
		return "recent"
	}
	return ""
}

// navKeyName 把侧边栏命令转成 batch-rpa 的 key 名（home/back/recent）。
func navKeyName(cmd string) string {
	switch cmd {
	case "goHome":
		return "home"
	case "goBack":
		return "back"
	case "goClean":
		return "recent"
	}
	return ""
}

func (g *groupRPAMgr) sendBatchRPA(path string, body map[string]interface{}) {
	containers := strings.Split(g.cfg.Containers, ",")
	reqBody := map[string]interface{}{
		"containers": containers,
		"path":       path,
		"body":       body,
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequest("POST", g.cfg.BaseURL, strings.NewReader(string(bodyBytes)))
	req.Header.Set("Authorization", "Bearer "+g.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		logf("[grouprpa] batch-rpa error: %v\n", err)
		return
	}
	resp.Body.Close()
}
