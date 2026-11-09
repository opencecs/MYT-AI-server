package main

// matrixInjectJS is the JavaScript injected into the WebView2 page on
// NavigationCompleted. 公网群控方案 C：语义事件 + myt-agent 注入接口。
//
//	window.__mxSetPrimary(bool) — true: 主控，激活捕获监听，发语义事件
//	  (touch/key) 经 UDP 扇出；false: 被控，移除监听。不 preventDefault。
//
//	window.__mxReplay(evJson) — 被控：解析语义事件 JSON，优先调用 myt-agent
//	  注入接口 window.__mytControl；接口缺失时回退到合成 DOM 事件（v5 旧行为）。
//
// 协议（JSON over UDP，127.0.0.1）：
//	{t:"touch", action:"down|move|up", x:0.5, y:0.3, tid:0}  触摸
//	{t:"key", keycode:66}                                   按键（映射表内）
//	{t:"nav", action:"home|back|recent"}                     导航
//	{t:"reset"}                                            清掊触点
//	{t:"role", primary, targets}                            角色切换（旧）
//	{t:"ev", kind, x, y, ...}                               v5 旧协议（兼容）
//
// Idempotent: guarded by __mxInstalled flag.
const matrixInjectJS = `(function(){
  if (window.__mxInstalled) return;
  window.__mxInstalled = true;
  window.__mxPrimary = false;
  window.__mxHandler = null;
  window.__mxHandlers = null;

  function probe(obj) {
    obj.mx = 2;
    try { window.chrome.webview.postMessage(JSON.stringify(obj)); } catch(ex) {}
  }

  // myt-agent 暴露的注入接口（C 方案核心）。被控侧通过它直接驱动 h5_lgair
  // 真实发送通道，绕开 isTrusted。接口缺失时回退到合成 DOM 事件。
  function hasControlIface() {
    return typeof window.__mytControl === 'function';
  }
  function callControl(ev) {
    if (!hasControlIface()) return false;
    try { window.__mytControl(ev); return true; }
    catch(ex) { probe({probe:'ctrl_err', err:String(ex)}); return false; }
  }
  function callControlReset() {
    if (typeof window.__mytControlReset === 'function') {
      try { window.__mytControlReset(); return true; }
      catch(ex) { probe({probe:'reset_err', err:String(ex)}); }
    }
    return false;
  }

  window.__mxSetPrimary = function(primary) {
    probe({setprimary:primary});
    if (window.__mxPrimary === primary) return;
    if (window.__mxHandlers) {
      window.__mxHandlers.forEach(function(evt) {
        document.removeEventListener(evt, window.__mxHandler, true);
      });
      window.__mxHandlers = null;
      window.__mxHandler = null;
    }
    window.__mxPrimary = primary;
    if (!primary) return;
    var handler = function(e) {
      // 去重：pointer 支持下 mousemove/touch* 跳过（pointermove 已覆盖）；
      // mousedown/up 兼兜底，与 pointerdown/up 用 200ms 时间窗去重。
      if (window.PointerEvent && (e.type === 'mousemove' || e.type.startsWith('touch'))) return;
      if (e.type === 'pointerdown') window.__mxLastPd = e.timeStamp;
      if (e.type === 'pointerup') window.__mxLastPu = e.timeStamp;
      if (e.type === 'mousedown' && e.timeStamp - (window.__mxLastPd || -1) < 200) return;
      if (e.type === 'mouseup' && e.timeStamp - (window.__mxLastPu || -1) < 200) return;

      var video = document.querySelector('video');
      if (!video) return;
      var rect = video.getBoundingClientRect();
      if (rect.width === 0 || rect.height === 0) return;

      // ---- 触摸：手势识别（down 记录起点，move 只更新终点不发，up 时判断手势一次性发）----
      // B 端 rpa/touch 限流（5/秒），原始 touch_down/move/up 会被 429，改发高层手势绕过限流。
      var isDown  = e.type === 'pointerdown' || e.type === 'mousedown';
      var isMove = e.type === 'pointermove' || e.type === 'mousemove';
      var isUp    = e.type === 'pointerup' || e.type === 'mouseup';
      if (isDown || isMove || isUp) {
        // 只处理左键（move 的 button 恒为 -1，不过滤）
        if ((isDown || isUp) && e.button != null && e.button !== 0 && e.button !== -1) return;
        var x = (e.clientX - rect.left) / rect.width;
        var y = (e.clientY - rect.top) / rect.height;
        var now = Date.now();
        if (isDown) {
          window.__mxDown = { x: x, y: y, t: now };
          window.__mxCaptured = (window.__mxCaptured || 0) + 1;
          probe({captured:window.__mxCaptured, action:'down', x:x, y:y});
          return;
        }
        if (isMove) {
          if (window.__mxDown) { window.__mxDown.lastX = x; window.__mxDown.lastY = y; }
          return; // move 不发，只记录终点
        }
        if (isUp) {
          var d = window.__mxDown;
          window.__mxDown = null;
          if (!d) return;
          var dx = x - d.x, dy = y - d.y;
          var dist = Math.sqrt(dx*dx + dy*dy);
          var duration = now - d.t;
          var ev;
          // 手势判断：距离<0.03(归一化)且时间<300ms → click，距离>=0.03 → swipe，时间长 → long_press
          if (dist < 0.03 && duration < 300) {
            ev = { mx:1, t:'gesture', gtype:'click', x:x, y:y };
          } else if (dist < 0.03 && duration >= 500) {
            ev = { mx:1, t:'gesture', gtype:'long_press', x:x, y:y, duration:duration };
          } else {
            ev = { mx:1, t:'gesture', gtype:'swipe', x:d.x, y:d.y, x2:x, y2:y, duration:duration };
          }
          try { window.chrome.webview.postMessage(JSON.stringify(ev)); } catch(ex) {}
          window.__mxCaptured = (window.__mxCaptured || 0) + 1;
          probe({captured:window.__mxCaptured, gtype:ev.gtype, dist:Math.round(dist*1000), duration:duration});
          return;
        }
        return;
      }

      // ---- 按键：keydown → 映射到 myt-agent 支持的 keycode ----
      if (e.type === 'keydown') {
        var kc = mapKeyCode(e);
        if (kc === 0) return; // 表外丢弃
        var ev = { mx:1, t:'key', keycode:kc };
        try { window.chrome.webview.postMessage(JSON.stringify(ev)); } catch(ex) {}
        probe({captured:++window.__mxCaptured, key:e.key, kc:kc});
        return;
      }
      // wheel/keyup 等暂不处理（如需可扩展）
    };
    window.__mxHandler = handler;
    var events = ['pointerdown','pointermove','pointerup','mousedown','mouseup','keydown'];
    events.forEach(function(evt) {
      document.addEventListener(evt, handler, true);
    });
    window.__mxHandlers = events;
    probe({bridge:'active', primary:true, iface:hasControlIface()});
    // 主控自动延迟调 diag（等 myt-agent 诊断接口注入就绪），输出通道状态供对照被控
    if (!window.__mxDiagScheduled) {
      window.__mxDiagScheduled = true;
      var diagAttempts = 0;
      function tryDiag() {
        diagAttempts++;
        if (typeof window.__mytControlDiag === 'function') {
          try { window.__mytControlDiag(); probe({diag:'primary_called', attempts:diagAttempts}); }
          catch(ex) { probe({diag_err:String(ex)}); }
        } else if (diagAttempts < 30) {
          setTimeout(tryDiag, 200);
        } else {
          probe({diag:'primary_no_iface'});
        }
      }
      setTimeout(tryDiag, 500);

      // 主控自动延迟注入一次触摸测试（模拟被控调用方式，等价于控制台手动 __mytControl）。
      // 用于对照“控制台注入”路径：若此时主控画面无反应且后续 diag 仍 sendChannel:null，
      // 则证明 __mytControl 注入路径本身在 sendChannel null 时发不出（与被控同），
      // 主控本地鼠标反应走的是 h5_lgair 原生 DOM handler（独立入口）。
      if (!window.__mxAutoInjectScheduled) {
        window.__mxAutoInjectScheduled = true;
        setTimeout(function() {
          if (typeof window.__mytControl !== 'function') {
            probe({auto_inject:'no_iface'});
            return;
          }
          try {
            window.__mytControl({type:'touch', action:'down', x:0.5, y:0.5, tid:0});
            window.__mytControl({type:'touch', action:'up',   x:0.5, y:0.5, tid:0});
            probe({auto_inject:'called'});
          } catch(ex) { probe({auto_inject_err:String(ex)}); }
        }, 3000);
      }
    }
  };

  // 主控键盘 keycode 映射到 myt-agent 支持表（讨论文档 §9.1）
  // 返回 0 表示不支持（丢弃）
  function mapKeyCode(e) {
    var k = e.key || '';
    var kc = e.keyCode || 0;
    // 导航键
    if (k === 'Escape' || k === 'Backspace' || kc === 27 || kc === 8) return 4;   // goBack
    if (k === 'Home' || kc === 36) return 3;                                    // goHome
    if (k === 'Enter' || kc === 13) return 66;                                   // VK_RETURN
    if (k === 'Delete' || kc === 46) return 67;                                  // VK_DELETE
    if (k === '=' || kc === 187) return 187;                                     // goClean
    // 方向键
    if (k === 'ArrowUp' || kc === 38) return 19;
    if (k === 'ArrowDown' || kc === 40) return 20;
    if (k === 'ArrowLeft' || kc === 37) return 21;
    if (k === 'ArrowRight' || kc === 39) return 22;
    // 音量键
    if (k === 'VolumeUp' || kc === 175) return 24;
    if (k === 'VolumeDown' || kc === 174) return 25;
    return 0;
  }

  window.__mxReplay = function(evJson) {
    var ev;
    try { ev = JSON.parse(evJson); } catch(e) { return; }
    if (!ev || !ev.t) return;
    window.__mxSeen = (window.__mxSeen || 0) + 1;
    if (window.__mxSeen === 1 || window.__mxSeen % 100 === 1) {
      probe({seen:window.__mxSeen, t:ev.t});
    }
    try {
      // ---- C 方案：优先调 myt-agent 注入接口 ----
      if (ev.t === 'touch') {
        if (!hasControlIface()) { replayFallbackDOM(ev); return; }
        var cev = { type:'touch', action:ev.action, x:ev.x, y:ev.y, tid:(ev.tid||0) };
        if (callControl(cev)) {
          window.__mxInjected = (window.__mxInjected || 0) + 1;
          if (ev.action !== 'move' || window.__mxInjected % 100 === 1) {
            probe({injected:window.__mxInjected, action:ev.action});
          }
        }
        return;
      }
      if (ev.t === 'key') {
        if (!hasControlIface()) return; // key 无 DOM 回退意义
        callControl({ type:'key', keycode:ev.keycode });
        probe({injected:++window.__mxInjected, type:'key', kc:ev.keycode});
        return;
      }
      if (ev.t === 'nav') {
        if (!hasControlIface()) return;
        callControl({ type:'nav', action:ev.action });
        probe({injected:++window.__mxInjected, type:'nav', action:ev.action});
        return;
      }
      if (ev.t === 'reset') {
        if (callControlReset()) probe({reset:1});
        return;
      }
      if (ev.t === 'diag') {
        // 被控收到主控广播的 diag 事件，调用 myt-agent 诊断接口打印通道状态
        if (typeof window.__mytControlDiag === 'function') {
          try { window.__mytControlDiag(); probe({diag:'called'}); }
          catch(ex) { probe({diag_err:String(ex)}); }
        } else {
          probe({diag:'no_iface'});
        }
        return;
      }
      // ---- v5 旧协议 ev：直接走 DOM 合成回退 ----
      if (ev.t === 'ev') { replayFallbackDOM(ev); }
    } catch(ex) { probe({replay_err:String(ex)}); }
  };

  // v5 回退：合成 MouseEvent/KeyboardEvent 派发（isTrusted=false，可能被 h5_lgair 丢弃）
  function replayFallbackDOM(ev) {
    var video = document.querySelector('video');
    var kind = ev.kind;
    var rect = video ? video.getBoundingClientRect() : {left:0, top:0, width:0, height:0};
    var clientX = rect.left + (ev.x || 0) * rect.width;
    var clientY = rect.top + (ev.y || 0) * rect.height;
    if (kind && (kind.startsWith('pointer') || kind.startsWith('touch') || kind.startsWith('mouse'))) {
      if (!video) return;
      var isMove = kind === 'pointermove' || kind === 'touchmove' || kind === 'mousemove';
      if (!isMove && ev.button != null && ev.button !== 0) return;
      var mouseKind = kind.indexOf('down') >= 0 ? 'mousedown' :
                      kind.indexOf('move') >= 0 ? 'mousemove' : 'mouseup';
      video.dispatchEvent(new MouseEvent(mouseKind, {
        clientX: clientX, clientY: clientY,
        button: 1, buttons: ev.buttons != null ? ev.buttons : 1,
        bubbles: true, cancelable: true
      }));
      window.__mxReplayed = (window.__mxReplayed || 0) + 1;
      if (mouseKind !== 'mousemove' || window.__mxReplayed % 100 === 1) {
        probe({replayed:window.__mxReplayed, kind:mouseKind});
      }
    } else if (kind === 'keydown' || kind === 'keyup') {
      var target = document.activeElement || document.body;
      target.dispatchEvent(new KeyboardEvent(kind, {
        key: ev.key || '', code: ev.code || '', keyCode: ev.keyCode || 0,
        ctrlKey: ev.ctrl || false, shiftKey: ev.shift || false, altKey: ev.alt || false,
        bubbles: true, cancelable: true
      }));
    }
  }

  console.log('[matrix] JS bridge installed (plan C)');

  // 探测 video 真实分辨率并上报 Go 侧，避免 inject 用写死的 sw/sh 导致坐标偏移。
  // video 元素可能在 navigation 完成后才加载 metadata，轮询直到拿到非零分辨率。
  (function probeVideoRes() {
    var attempts = 0;
    function check() {
      attempts++;
      var v = document.querySelector('video');
      if (v && v.videoWidth && v.videoHeight) {
        try {
          // rotated 直接看 video 宽高比：宽>高即横屏（旋转后宽高互换）
          var rotated = v.videoWidth > v.videoHeight;
          window.chrome.webview.postMessage(JSON.stringify({
            mx: 4, vw: v.videoWidth, vh: v.videoHeight, rotated: rotated
          }));
        } catch(e) {}
        return;
      }
      if (attempts < 60) setTimeout(check, 200); // 最多轮 12 秒
    }
    check();
    // 方向变化时重新上报（旋转后 videoWidth/Height 互换）
    window.__mxReportVideoRes = check;
  })();
})();`
