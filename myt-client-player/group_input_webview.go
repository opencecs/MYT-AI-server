package main

import (
	"fmt"
	"strings"
)

// GroupInputConfig 群控输入配置（解析自 -groupinput 参数）
type GroupInputConfig struct {
	BaseURL string // myt-agent 基地址
	Token   string // session token
	Targets string // "device_id:container:index,device_id2:container2:index2"
}

var groupInputCfg *GroupInputConfig

// parseGroupInputParam 解析 -groupinput "baseURL|token|targets_str"
func parseGroupInputParam(param string) {
	param = strings.TrimSpace(param)
	if param == "" {
		return
	}
	parts := strings.SplitN(param, "|", 3)
	if len(parts) != 3 {
		logf("[groupinput] invalid -groupinput param (need 3 parts separated by |)\n")
		return
	}
	groupInputCfg = &GroupInputConfig{
		BaseURL: parts[0],
		Token:   parts[1],
		Targets: parts[2],
	}
	logf("[groupinput] initialized: baseURL=%s targets=%s\n", parts[0], parts[2])
}

// handleGroupInputMessage 处理页面回传的 group-input 探针消息
func handleGroupInputMessage(raw string) {
	logf("[groupinput] page: %s\n", raw)
}

// buildGroupInputInjectJS 生成群控输入注入 JS。
//
// 功能：
//  1. 在 video 元素上捕获 pointer/touch/keyboard 事件
//  2. 坐标映射：clientX/Y → 设备原始分辨率坐标
//  3. 事件转换：down+up<30px→click, down+up>30px→swipe, move→touch_move(50ms节流)
//  4. fetch POST /api/screencast/group-input 一次性扇出到所有被控设备
//  5. 群控开关：window.__mxGroupControl(true/false)
//
// 不阻断本地输入（不 preventDefault / stopPropagation），主控设备正常工作。
func buildGroupInputInjectJS(cfg *GroupInputConfig) string {
	if cfg == nil {
		return ""
	}
	return fmt.Sprintf(`(function(){
  if (window.__mxGIInstalled) return;
  window.__mxGIInstalled = true;
  window.__mxGIActive = false;
  window.__mxGIHandler = null;
  window.__mxGIHandlers = null;

  function probe(obj) {
    obj.gi = 1;
    try { window.chrome.webview.postMessage(JSON.stringify(obj)); } catch(e) {}
  }

  var baseURL = %q;
  var token = %q;
  var targetsStr = %q;

  probe({event:'install', baseURL:baseURL, targetsStr:targetsStr});

  // 解析 targets
  function parseTargets(s) {
    var result = [];
    s.split(',').forEach(function(part) {
      var segs = part.split(':');
      if (segs.length >= 2) {
        result.push({
          device_id: segs[0],
          container: segs[1],
          index: parseInt(segs[2] || '0', 10)
        });
      }
    });
    return result;
  }
  var targets = parseTargets(targetsStr);
  probe({event:'parsed', count:targets.length, first:targets.length>0?JSON.stringify(targets[0]):'none'});
  if (targets.length === 0) { probe({event:'no_targets'}); return; }

  // 发送 group-input 请求（带 50ms 节流）
  var lastSendTime = 0;
  var pendingEvent = null;
  var sendTimer = null;
  var sendCount = 0;

  function sendEvent(event) {
    sendCount++;
    var body = JSON.stringify({ targets: targets, event: event });
    probe({event:'send', n:sendCount, type:event.type, x:event.x, y:event.y});
    fetch(baseURL + '/api/screencast/group-input', {
      method: 'POST',
      headers: {
        'Authorization': 'Bearer ' + token,
        'Content-Type': 'application/json'
      },
      body: body
    }).then(function(resp) {
      probe({event:'resp', n:sendCount, status:resp.status, ok:resp.ok});
      return resp.text();
    }).then(function(text) {
      if (sendCount <= 10) probe({event:'resp_body', n:sendCount, body:text.substring(0, 200)});
    }).catch(function(e) {
      probe({event:'fetch_err', n:sendCount, err:String(e)});
    });
  }

  function throttledSend(event) {
    var now = Date.now();
    if (event.type === 'touch_move' || event.type === 'touch_down' || event.type === 'touch_up') {
      // touch 事件节流 50ms
      if (now - lastSendTime < 50) {
        pendingEvent = event;
        if (!sendTimer) {
          sendTimer = setTimeout(function() {
            sendTimer = null;
            if (pendingEvent) { sendEvent(pendingEvent); pendingEvent = null; lastSendTime = Date.now(); }
          }, 50);
        }
        return;
      }
    }
    lastSendTime = now;
    sendEvent(event);
  }

  // 坐标映射：clientX/Y → 设备原始分辨率
  var noVideoCount = 0;
  function mapCoords(e) {
    var video = document.querySelector('video');
    if (!video || !video.videoWidth) {
      noVideoCount++;
      if (noVideoCount <= 3 || noVideoCount %% 100 === 1) {
        probe({event:'no_video', n:noVideoCount, hasVideo:!!video, vw:video?video.videoWidth:0});
      }
      return null;
    }
    var rect = video.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) return null;
    var x = Math.round((e.clientX - rect.left) / rect.width * video.videoWidth);
    var y = Math.round((e.clientY - rect.top) / rect.height * video.videoHeight);
    x = Math.max(0, Math.min(x, video.videoWidth));
    y = Math.max(0, Math.min(y, video.videoHeight));
    return { x: x, y: y };
  }

  // 手势状态跟踪
  var pointerDown = false;
  var downX = 0, downY = 0, downTime = 0;
  var lastMoveX = 0, lastMoveY = 0;

  function handleEvent(e) {
    // 去重：pointer 支持下跳过 mouse/touch
    if (window.PointerEvent && (e.type === 'mousemove' || e.type.startsWith('touch'))) return;
    if (e.type === 'pointerdown') window.__mxGILastPd = e.timeStamp;
    if (e.type === 'pointerup') window.__mxGILastPu = e.timeStamp;
    if (e.type === 'mousedown' && e.timeStamp - (window.__mxGILastPd || -1) < 200) return;
    if (e.type === 'mouseup' && e.timeStamp - (window.__mxGILastPu || -1) < 200) return;

    // 只处理左键
    if (e.type === 'mousedown' || e.type === 'pointerdown') {
      if (e.button !== 0 && e.button !== -1 && e.button !== undefined) return;
    }

    var coords = mapCoords(e);
    if (!coords) return;

    var now = Date.now();

    if (e.type === 'pointerdown' || e.type === 'mousedown') {
      pointerDown = true;
      downX = coords.x; downY = coords.y; downTime = now;
      lastMoveX = coords.x; lastMoveY = coords.y;
      throttledSend({ type: 'touch_down', x: coords.x, y: coords.y });
    } else if (e.type === 'pointermove' || e.type === 'mousemove') {
      if (!pointerDown) return;
      lastMoveX = coords.x; lastMoveY = coords.y;
      throttledSend({ type: 'touch_move', x: coords.x, y: coords.y });
    } else if (e.type === 'pointerup' || e.type === 'mouseup') {
      if (!pointerDown) return;
      pointerDown = false;
      var dx = coords.x - downX, dy = coords.y - downY;
      var dist = Math.sqrt(dx*dx + dy*dy);
      var duration = now - downTime;

      throttledSend({ type: 'touch_up', x: coords.x, y: coords.y });

      // 手势判断：距离<30px且时间<300ms → click，否则 → swipe
      if (dist < 30 && duration < 300) {
        throttledSend({ type: 'click', x: coords.x, y: coords.y });
      } else if (dist >= 30) {
        throttledSend({ type: 'swipe', x: downX, y: downY, x2: coords.x, y2: coords.y, duration: Math.round(duration) });
      }
    } else if (e.type === 'wheel') {
      var delta = e.deltaY || 0;
      if (Math.abs(delta) < 5) return;
      var dir = delta > 0 ? 1 : -1;
      throttledSend({ type: 'swipe', x: coords.x, y: coords.y, x2: coords.x, y2: coords.y + dir * 200, duration: 300 });
    } else if (e.type === 'keydown') {
      handleKey(e);
    }
  }

  function handleKey(e) {
    // 导航键映射
    var keyMap = {
      'Escape': { type: 'nav', nav: 'goBack' },
      'Home': { type: 'nav', nav: 'goHome' },
      'Enter': { type: 'key', keycode: 66 },
      'Backspace': { type: 'nav', nav: 'goBack' },
    };
    // keycode 映射
    var keyCodeMap = { 27: { type: 'nav', nav: 'goBack' }, 3: { type: 'nav', nav: 'goHome' }, 66: { type: 'key', keycode: 66 }, 187: { type: 'key', keycode: 66 } };
    var ev = keyMap[e.key] || keyCodeMap[e.keyCode];
    if (ev) {
      throttledSend(ev);
    }
  }

  // 群控开关
  window.__mxGroupControl = function(active) {
    if (window.__mxGIActive === active) return;
    // 移除旧监听器
    if (window.__mxGIHandlers) {
      window.__mxGIHandlers.forEach(function(evt) {
        document.removeEventListener(evt, window.__mxGIHandler, true);
      });
      window.__mxGIHandlers = null;
      window.__mxGIHandler = null;
    }
    window.__mxGIActive = active;
    if (!active) {
      console.log('[groupinput] deactivated');
      return;
    }
    var handler = function(e) { handleEvent(e); };
    window.__mxGIHandler = handler;
    var events = ['pointerdown','pointermove','pointerup','mousedown','mouseup','mousemove','wheel','keydown'];
    events.forEach(function(evt) {
      document.addEventListener(evt, handler, true);
    });
    window.__mxGIHandlers = events;
    probe({event:'activated', targets:targets.length});
  };

  // 默认激活群控
  window.__mxGroupControl(true);
  probe({event:'installed', targets:targets.length});
})();`, cfg.BaseURL, cfg.Token, cfg.Targets)
}
