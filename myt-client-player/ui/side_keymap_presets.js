// side_keymap_presets.js — 键位映射·方案管理:新建/重命名/删除/切换/导入导出。
// 依赖 side_keymap.js 的 km 状态与渲染(kmRenderProfile/kmActivePreset),故在其后加载。
// 命令协议:km_preset_new/km_preset_rename/km_preset_delete/km_preset_select/km_import/km_export。

var kmDelArmed = false;      // 删除两步确认 armed 态
var kmDelTimer = null;
var kmPendingRename = false; // 新建完成后自动进入重命名
var kmStatusTimer = null;

// kmStatus 方案区状态反馈(#km-status),2.5s 自动清除。
function kmStatus(text, isErr){
  var el = document.getElementById('km-status');
  if(!el) return;
  el.textContent = text || '';
  el.className = 'status-msg' + (text ? (isErr ? ' err' : ' ok') : '');
  if(kmStatusTimer) clearTimeout(kmStatusTimer);
  if(text){
    kmStatusTimer = setTimeout(function(){
      el.textContent = '';
      el.className = 'status-msg';
    }, 2500);
  }
}

// kmDisarmDel 复位删除两步确认。
function kmDisarmDel(){
  if(kmDelTimer){ clearTimeout(kmDelTimer); kmDelTimer = null; }
  kmDelArmed = false;
  var del = document.getElementById('km-preset-del');
  if(del){ del.textContent = '删除'; del.classList.remove('danger'); }
}

// kmOnUIUpdated 每次状态推送后由 updateKeymapUI 调用:
// 复位删除确认态;新建方案后(activeId 已变)自动进入重命名。
function kmOnUIUpdated(prevActive){
  kmDisarmDel();
  if(kmPendingRename){
    kmPendingRename = false;
    if(km.active && km.active !== prevActive){
      setTimeout(kmStartRename, 60); // 等方案区渲染落定再弹名称输入框
    }
  }
}

// ─── 新建/删除/重命名/导入导出 ───────────────────────────────────────────────
// 编辑会话与方案切换类操作互斥:工作副本(km.work)指向进入编辑时的预设,
// 中途换/删/建方案会让「完成」保存到错误目标,一律拦截并提示。
document.addEventListener('click', function(e){
  if(e.target.id === 'km-preset-new'){
    if(km.editing){ kmStatus('请先完成或取消编辑', true); return; }
    kmPendingRename = true; // 名称由 Go 自动编号,建好即弹重命名可直接改名
    kmSend({cmd:'km_preset_new'});
    return;
  }
  if(e.target.id === 'km-preset-del'){
    if(km.editing){ kmStatus('请先完成或取消编辑', true); return; }
    if(!km.active){ kmStatus('先在上方选择要删除的方案', true); return; }
    if(!kmDelArmed){ // 两步确认:第一次点击只武装,3 秒内再点才真正删除
      kmDelArmed = true;
      e.target.textContent = '确认删除';
      e.target.classList.add('danger');
      kmStatus('再点一次确认删除「' + ((kmActivePreset() && kmActivePreset().name) || '') + '」', true);
      if(kmDelTimer) clearTimeout(kmDelTimer);
      kmDelTimer = setTimeout(kmDisarmDel, 3000);
      return;
    }
    kmDisarmDel();
    kmSend({cmd:'km_preset_delete', arg:km.active});
    return;
  }
  if(e.target.id === 'km-rename'){ kmStartRename(); return; }
  if(e.target.id === 'km-io-toggle'){
    km.ioOpen = !km.ioOpen;
    var body = document.getElementById('km-io-body');
    if(body) body.style.display = km.ioOpen ? 'block' : 'none';
    e.target.textContent = km.ioOpen ? '收起导入/导出' : '导入/导出';
    return;
  }
  if(e.target.id === 'km-export-btn'){
    if(km.editing){ kmStatus('请先完成或取消编辑', true); return; }
    if(!km.active){ kmStatus('先选择要导出的方案', true); return; }
    kmSend({cmd:'km_export'});
    return;
  }
  if(e.target.id === 'km-import-btn'){
    if(km.editing){ kmStatus('请先完成或取消编辑', true); return; }
    var ta = document.getElementById('km-import-ta');
    var data = ta ? ta.value.trim() : '';
    if(!data){ kmStatus('先粘贴方案 JSON 再导入', true); return; }
    kmSend({cmd:'km_import', data:data});
    return;
  }
});

// 切换方案:编辑中下拉框已由 kmRenderPresets 禁用,此处兜底拦截(双保险)
document.addEventListener('change', function(e){
  if(e.target.id !== 'km-preset-select') return;
  if(km.editing){
    kmStatus('请先完成或取消编辑', true);
    return;
  }
  kmSend({cmd:'km_preset_select', arg:e.target.value});
});

// ─── 重命名:方案卡名称原地变输入框,Enter 提交 / Esc 取消 / 失焦提交 ─────────
function kmStartRename(){
  if(km.renaming) return;
  var nameEl = document.getElementById('km-pro-name');
  if(!nameEl) return;
  var cur = (km.work && km.work.name) || (kmActivePreset() ? kmActivePreset().name : '');
  if(!cur && !km.active) return;
  km.renaming = true;
  nameEl.innerHTML = '';
  var inp = document.createElement('input');
  inp.className = 'text-input';
  inp.value = cur;
  inp.style.fontSize = '12px'; inp.style.fontWeight = '650';
  nameEl.appendChild(inp);
  inp.focus(); inp.select();
  var done = false;
  function commit(submit){
    if(done) return; done = true; km.renaming = false;
    var v = inp.value.trim();
    var id = (km.work && km.work.id) || km.active;
    if(submit && v){
      if(id){
        kmSend({cmd:'km_preset_rename', arg:id, name:v}); // Go 侧同步工作副本名
        return;
      }
      if(km.work) km.work.name = v; // 尚未保存的新方案:本地改名,「完成」时落盘
    }
    kmRenderProfile(); // 取消/空名/本地改名:还原或刷新显示
  }
  inp.addEventListener('keydown', function(ev){
    ev.stopPropagation();
    if(ev.key === 'Enter') commit(true);
    else if(ev.key === 'Escape') commit(false);
  });
  inp.addEventListener('blur', function(){ commit(true); });
}
