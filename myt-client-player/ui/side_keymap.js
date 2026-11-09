// side_keymap.js — 键位映射面板:状态接收、渲染、条目编辑交互。
// 方案管理(新建/重命名/删除/切换/导入导出)在 side_keymap_presets.js(后加载)。
// 命令协议:km_ui/km_enable/km_hints/km_edit/km_cancel/km_add/km_sel/km_del/km_bind(+slot)/km_prop(tapmode|android|r|size)。

var km = {enabled:false, hints:true, editing:false, selId:'', active:'', presets:[], work:null, ioOpen:false, renaming:false};
var kmCapturing = false;
var kmCaptureSlot = -1; // dpad 指定槽位(0上/1左/2下/3右,-1=自动)

function kmSend(obj){ send(obj); } // 转发到 side.js 的全局 send

// Go → JS:键位面板完整状态(EvalKeymapUI 推送)
window.updateKeymapUI = function(s){
  var prevActive = km.active;
  km.enabled = !!s.enabled;
  km.hints = s.hints !== false;
  km.editing = !!s.editing;
  km.selId = s.selId || '';
  km.active = s.activeId || '';
  km.presets = s.presets || [];
  km.work = s.work || null;
  kmRender();
  if(typeof kmOnUIUpdated === 'function') kmOnUIUpdated(prevActive); // 方案管理钩子
};

function kmTypeLabel(t){
  return {tap:'点击', dpad:'摇杆', swipe:'滑动', android:'系统键'}[t] || t;
}
function kmKeyLabel(code){
  if(!code) return '';
  if(code.indexOf('Key') === 0) return code.slice(3);
  if(code.indexOf('Digit') === 0) return code.slice(5);
  if(code === 'Space') return '空格';
  return code;
}
function kmAndroidLabel(a){
  return {back:'返回', home:'主页', recent:'多任务', volup:'音量+', voldown:'音量-'}[a] || a;
}
function kmActivePreset(){
  for(var i = 0; i < km.presets.length; i++){
    if(km.presets[i].id === km.active) return km.presets[i];
  }
  return null;
}
function kmSelectedEntry(){
  if(!km.work || !km.work.entries) return null;
  for(var i = 0; i < km.work.entries.length; i++){
    if(km.work.entries[i].id === km.selId) return km.work.entries[i];
  }
  return null;
}

// ─── 渲染 ────────────────────────────────────────────────────────────────────

function kmRender(){
  kmRenderSwitch();
  kmRenderProfile();
  kmRenderPresets();
  kmRenderEditor();
}

function kmRenderSwitch(){
  var el = document.getElementById('km-switch');
  if(el) el.classList.toggle('on', km.enabled);
  var ht = document.getElementById('km-hints');
  if(ht) ht.classList.toggle('on', km.hints);
}

function kmRenderProfile(){
  var name = '未选择方案', meta = 'F9 快捷开关映射';
  var p = km.work || kmActivePreset();
  if(p){
    name = p.name || '未命名方案';
    meta = ((p.entries || []).length) + ' 个键位 · ' + (km.enabled ? '已启用' : '已停用') + ' · F9 快捷开关';
  }
  var a = document.getElementById('km-pro-name');
  if(a && !km.renaming) a.textContent = name; // 重命名输入中不覆盖输入框
  var b = document.getElementById('km-pro-meta');
  if(b) b.textContent = meta;
}

function kmRenderPresets(){
  var sel = document.getElementById('km-preset-select');
  if(!sel) return;
  sel.innerHTML = '';
  if(!km.presets.length){
    sel.appendChild(new Option('暂无方案 — 点「新建」创建一套', ''));
  }
  km.presets.forEach(function(p){
    var opt = new Option(p.name || '(未命名)', p.id);
    if(p.id === km.active) opt.selected = true;
    sel.appendChild(opt);
  });
  sel.disabled = !!km.editing; // 编辑中锁定方案切换(工作副本仍指向当前预设)
  var none = !km.active;
  var del = document.getElementById('km-preset-del');
  if(del) del.disabled = none;
  var rn = document.getElementById('km-rename');
  if(rn) rn.disabled = none;
}

function kmRenderEditor(){
  var editor = document.getElementById('km-editor');
  var btnrow = document.getElementById('km-edit-btnrow');
  if(!editor) return;
  editor.style.display = km.editing ? 'block' : 'none';
  if(btnrow) btnrow.style.display = km.editing ? 'none' : 'flex';
  var eb = document.getElementById('km-edit-btn');
  if(eb) eb.textContent = '编辑键位';
  if(!km.editing) return;
  kmRenderList();
  kmRenderProps();
}

function kmKeycap(code){
  return '<i class="keycap' + (code ? '' : ' empty') + '">' + (code ? kmKeyLabel(code) : '···') + '</i>';
}

function kmKeysHtml(en){
  if(en.type === 'dpad'){
    var h = '';
    for(var i = 0; i < 4; i++){ h += kmKeycap((en.keys && en.keys[i]) || ''); }
    return '<span class="keys">' + h + '</span>';
  }
  if(en.type === 'android'){
    return '<span class="keys">' + kmKeycap(en.keys && en.keys[0] || '')
      + '<span class="arrowk">→</span><span style="font-size:10px;color:var(--text-2)">' + kmAndroidLabel(en.android) + '</span></span>';
  }
  return '<span class="keys">' + kmKeycap(en.keys && en.keys[0] || '') + '</span>';
}

function kmRenderList(){
  var list = document.getElementById('km-list');
  if(!list) return;
  var work = km.work;
  var cnt = document.getElementById('km-count');
  if(cnt) cnt.textContent = work ? (work.entries || []).length : 0;
  if(!work || !work.entries || !work.entries.length){
    list.innerHTML = '<div class="km-empty">暂无键位,点下方类型块添加</div>';
    return;
  }
  var html = '';
  work.entries.forEach(function(en){
    var selCls = en.id === km.selId ? ' sel' : '';
    html += '<div class="srow kmitem' + selCls + '" data-kmid="' + en.id + '">'
      + '<span class="kmbadge ' + en.type + '">' + kmTypeLabel(en.type) + '</span>'
      + kmKeysHtml(en)
      + '<span class="kmdel" data-kmdel="' + en.id + '" title="删除">×</span>'
      + '</div>';
  });
  list.innerHTML = html;
}

// kmSizeRowHtml 按键大小滑杆行(60%~300%,映射 en.scale 0.6~3.0,与滚轮同域)。
function kmSizeRowHtml(en){
  var sv = Math.round((en.scale || 1) * 100);
  return '<div class="row" style="border-bottom:none;min-height:34px"><span class="rl">按键大小</span>'
    + '<span class="rc" style="width:118px;display:flex;align-items:center;gap:7px">'
    + '<input type="range" class="kmrange" data-prop="size" min="60" max="300" step="10" value="' + sv + '" style="flex:1;accent-color:#4a90d9">'
    + '<span class="mono" style="font-size:10px;color:var(--text-2)">' + sv + '%</span></span></div>';
}

function kmRenderProps(){
  var box = document.getElementById('km-props');
  if(!box) return;
  var en = kmSelectedEntry();
  if(!en){ box.innerHTML = ''; box.style.display = 'none'; return; }
  box.style.display = 'block';
  var cap = '<input class="km-capture" id="km-capture" readonly placeholder="点击后按任意键绑定">';
  var h = '<div class="sect">编辑 · ' + kmTypeLabel(en.type) + '</div>'
    + '<div class="surface" style="padding:9px 10px">';
  if(en.type === 'dpad'){
    h += '<div class="flabel" style="margin-bottom:5px">方向键绑定(点击槽位后按键)</div><div class="slot4">';
    var dirs = ['上','左','下','右'];
    for(var i = 0; i < 4; i++){
      var k = (en.keys && en.keys[i]) || '';
      h += '<div class="slot" data-kmslot="' + i + '"><i class="keycap' + (k ? '' : ' empty') + '">'
        + (k ? kmKeyLabel(k) : '···') + '</i><span class="sd">' + dirs[i] + '</span></div>';
    }
    h += '</div><div class="frow block" style="margin-top:8px;border-bottom:none">' + cap + '</div>';
    var rv = Math.round((en.r || 700) / 10);
    h += '<div class="row" style="border-bottom:none;min-height:34px"><span class="rl">摇杆半径</span>'
      + '<span class="rc" style="width:118px;display:flex;align-items:center;gap:7px">'
      + '<input type="range" class="kmrange" data-prop="r" min="20" max="250" value="' + rv + '" style="flex:1;accent-color:#4a90d9">'
      + '<span class="mono" style="font-size:10px;color:var(--text-2)">' + rv + '</span></span></div>';
    h += kmSizeRowHtml(en);
  } else if(en.type === 'tap'){
    h += '<div class="flabel" style="margin-bottom:5px">绑定按键</div>' + cap;
    h += '<div class="row" style="border-bottom:none;min-height:34px"><span class="rl">触发方式</span>'
      + '<span class="rc" style="width:120px"><div class="seg">'
      + '<div class="' + (en.tapMode !== 'click' ? 'sel' : '') + '" data-kmtap="hold">按住</div>'
      + '<div class="' + (en.tapMode === 'click' ? 'sel' : '') + '" data-kmtap="click">点按</div>'
      + '</div></span></div>';
    h += kmSizeRowHtml(en);
  } else if(en.type === 'swipe'){
    h += '<div class="flabel" style="margin-bottom:5px">绑定按键</div>' + cap;
    h += kmSizeRowHtml(en);
    h += '<div class="hint" style="margin-top:6px;font-size:10px;color:var(--text-3);line-height:1.6">按下时停在起点,松开滑向终点;拖动画面上的两个手柄调整轨迹</div>';
  } else {
    h += '<div class="flabel" style="margin-bottom:5px">绑定按键</div>' + cap;
    h += '<div class="row" style="border-bottom:none;min-height:34px"><span class="rl">系统动作</span>'
      + '<span class="rc" style="width:122px"><select class="text-input" id="km-android">';
    var acts = [['back','返回'],['home','主页'],['recent','多任务'],['volup','音量+'],['voldown','音量-']];
    acts.forEach(function(a){
      h += '<option value="' + a[0] + '"' + (en.android === a[0] ? ' selected' : '') + '>' + a[1] + '</option>';
    });
    h += '</select></span></div>';
    h += kmSizeRowHtml(en);
  }
  h += '</div>';
  box.innerHTML = h;
  var capEl = box.querySelector('.km-capture');
  if(capEl){
    capEl.addEventListener('click', function(){
      if(en.type !== 'dpad') kmCaptureSlot = -1;
      kmCapturing = true; capEl.value = '按任意键…'; capEl.focus();
    });
  }
}

// ─── 交互事件(方案管理按钮在 side_keymap_presets.js) ─────────────────────────

document.addEventListener('click', function(e){
  var sw = e.target.closest('#km-switch');
  if(sw){ kmSend({cmd:'km_enable', arg: km.enabled ? '0' : '1'}); return; }
  var ht = e.target.closest('#km-hints');
  if(ht){ kmSend({cmd:'km_hints', arg: km.hints ? '0' : '1'}); return; }
  if(e.target.id === 'km-edit-btn'){
    kmSend({cmd:'km_edit', arg: km.editing ? '0' : '1'});
    return;
  }
  if(e.target.id === 'km-done'){ kmSend({cmd:'km_edit', arg:'0'}); return; }
  if(e.target.id === 'km-cancel'){ kmSend({cmd:'km_cancel'}); return; }
  var slotEl = e.target.closest('[data-kmslot]');
  if(slotEl){
    kmCaptureSlot = parseInt(slotEl.getAttribute('data-kmslot'), 10) || 0;
    kmCapturing = true;
    var cap2 = document.getElementById('km-capture');
    if(cap2){ cap2.value = '按任意键…'; cap2.focus(); }
    return;
  }
  var addBtn = e.target.closest('[data-kmadd]');
  if(addBtn){ kmSend({cmd:'km_add', arg:addBtn.getAttribute('data-kmadd')}); return; }
  var delBtn = e.target.closest('[data-kmdel]');
  if(delBtn){ kmSend({cmd:'km_del', arg:delBtn.getAttribute('data-kmdel')}); return; }
  var item = e.target.closest('[data-kmid]');
  if(item){ kmSend({cmd:'km_sel', arg:item.getAttribute('data-kmid')}); return; }
  var tapSeg = e.target.closest('[data-kmtap]');
  if(tapSeg){
    var en = kmSelectedEntry();
    if(en) kmSend({cmd:'km_prop', arg:en.id, prop:'tapmode', val:tapSeg.getAttribute('data-kmtap')});
    return;
  }
});

document.addEventListener('change', function(e){
  if(e.target.id === 'km-android'){
    var en = kmSelectedEntry();
    if(en) kmSend({cmd:'km_prop', arg:en.id, prop:'android', val:e.target.value});
  }
});

// 滑杆(摇杆半径/按键大小):input 拖动中实时提交(画面即时跟随),
// 数字标签同步更新;Go 侧侧栏推送走防抖,不会重建滑杆打断拖动。
document.addEventListener('input', function(e){
  if(e.target.classList && e.target.classList.contains('kmrange')){
    var lbl = e.target.parentNode.querySelector('.mono');
    var suffix = e.target.getAttribute('data-prop') === 'size' ? '%' : '';
    if(lbl) lbl.textContent = e.target.value + suffix;
    var en = kmSelectedEntry();
    var prop = e.target.getAttribute('data-prop') || 'r';
    if(en) kmSend({cmd:'km_prop', arg:en.id, prop:prop, val:e.target.value});
  }
});

// 按键捕获(readonly input 聚焦时,side_api.js 的全局捕获会跳过 INPUT 目标)
document.addEventListener('keydown', function(e){
  if(!kmCapturing || e.target.id !== 'km-capture') return;
  var code = e.code || '';
  if(code && code !== 'Escape'){
    kmSend({cmd:'km_bind', arg:code, slot:kmCaptureSlot});
  }
  kmCapturing = false; kmCaptureSlot = -1;
  kmRenderList(); kmRenderProps();
  e.preventDefault(); e.stopPropagation();
}, true);

kmRender();
