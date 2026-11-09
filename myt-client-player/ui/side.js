var state={clarity:2,topmost:false,rotated:false,ctrlList:[],allCtrlList:[],deviceAddr:'',connState:'idle',controlled:false,primaryAddr:'',selfAddr:'',camSource:''};
var expanded=false;
var activeTab='clarity';
var ipLocBusy=false;
var smsBusy=false;
var contactBusy=false;
var lastSetIP='';

var $=function(s){return document.querySelector(s)};
var $$=function(s){return document.querySelectorAll(s)};

window.postToHost=function(msg){
  var json=typeof msg==='string'?msg:JSON.stringify(msg);
  if(window.hostSend){window.hostSend(json);}
  else if(window.chrome&&window.chrome.webview){window.chrome.webview.postMessage(json);}
  else if(window.external&&window.external.invoke){window.external.invoke(json);}
  else{fetch('/send',{method:'POST',headers:{'Content-Type':'application/json'},body:json}).catch(function(){});}
};
// 「side」是消息来源戳（标识来自侧边栏面板）。cam_start 伪源（扫码）复用 src 字段
// 携带视频源（screenDevice/screenDesktop），不得覆盖——只在未设置时打默认戳。
function send(obj){if(!obj.src)obj.src='side';window.postToHost(obj);}

function showExpanded(show){
  expanded=show;
  var p=$('#panel-root');
  p.classList.toggle('collapsed',!show);
  p.classList.toggle('expanded',show);
  $('#main-area').style.display=show?'flex':'none';
}

function switchTab(tab){
  if(activeTab===tab){showExpanded(false);send({cmd:'panel',arg:'collapse'});return;}
  activeTab=tab;
  ['clarity','iploc','sms','upload','contact','batch','cam','keymap'].forEach(function(t){
    var el=$('#sb-'+t);if(el)el.classList.toggle('on',t===tab);
  });
  $('#tab-clarity').style.display=tab==='clarity'?'block':'none';
  $('#tab-iploc').style.display=tab==='iploc'?'block':'none';
  $('#tab-sms').style.display=tab==='sms'?'block':'none';
  $('#tab-upload').style.display=tab==='upload'?'block':'none';
  $('#tab-contact').style.display=tab==='contact'?'block':'none';
  $('#tab-batch').style.display=tab==='batch'?'block':'none';
  $('#tab-cam').style.display=tab==='cam'?'block':'none';
  $('#tab-keymap').style.display=tab==='keymap'?'block':'none';
  var titles={clarity:'清晰度',iploc:'IP 定位',sms:'接收短信',upload:'文件上传',contact:'添加联系人',batch:'批量控制',cam:'音视频推流',keymap:'键盘映射'};
  $('#panel-title').textContent=titles[tab]||'';
  if(tab==='keymap'){send({cmd:'km_ui'});}
}

function updateClarity(){
  $$('[data-clarity]').forEach(function(el){
    var v=parseInt(el.getAttribute('data-clarity'));
    el.classList.toggle('sel',v===state.clarity);
  });
}

function updateTopmost(){
  var el=$('#sb-topmost');if(el)el.classList.toggle('on',state.topmost);
}

function updateConn(){
  var bar=$('#conn-bar');if(!bar)return;
  var connected=state.connState==='';
  bar.classList.toggle('connected',connected);
  $('#conn-addr').textContent=state.deviceAddr||'--';
  $('#conn-state').textContent=(state.connState==='idle')?'':state.connState;
}

function updateBatch(){
  var list=$('#batch-list'),empty=$('#batch-empty');
  var sbBatch=$('#sb-batch');
  // 只在有矩阵控制状态时显示
  if(!state.primaryAddr&&!state.selfAddr&&!state.controlled){
    empty.style.display='block';list.innerHTML='';
    updateBadge('batch-badge',0);
    if(sbBatch)sbBatch.classList.remove('is-primary','is-controlled');
    return;
  }
  empty.style.display='none';
  var isPrimary=state.selfAddr&&state.selfAddr===state.primaryAddr;
  var html='<div class="batch-status">'
    +'<div class="status-dot '+(isPrimary?'primary':'controlled')+'"></div>'
    +'<div class="status-info">'
    +'<div class="status-label">'+(isPrimary?'主控':'被控')+'</div>'
    +(state.primaryAddr?'<div class="status-addr">主控: '+escHtml(state.primaryAddr)+'</div>':'')
    +'</div></div>';
  if(!isPrimary&&state.selfAddr){
    html+='<button class="primary-btn" data-bact="primary" data-addr="'+escHtml(state.selfAddr)+'" style="margin-top:10px;width:100%;text-align:center;padding:6px 0">设为主控</button>';
  }
  list.innerHTML=html;
  updateBadge('batch-badge',state.primaryAddr?1:0);
  // 侧边栏图标在收起状态也显示主控/被控角色
  if(sbBatch){
    sbBatch.classList.toggle('is-primary',isPrimary);
    sbBatch.classList.toggle('is-controlled',!isPrimary);
  }
}
function updateBadge(id,count){
  var el=$('#'+id);if(!el)return;
  if(count>0){el.textContent=count;el.style.display='flex';}
  else{el.style.display='none';}
}

function escHtml(s){return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;')}

document.addEventListener('click',function(e){
  var el=e.target.closest('[data-act]');
  if(el){
    var act=el.getAttribute('data-act');
    if(act==='tab:clarity'||act==='tab:iploc'||act==='tab:sms'||act==='tab:upload'||act==='tab:contact'||act==='tab:batch'||act==='tab:cam'||act==='tab:keymap'){
      var tab=act.split(':')[1];
      if(!expanded){switchTab(tab);showExpanded(true);send({cmd:'panel',arg:'expand'});}
      else switchTab(tab);
    }else if(act==='topmost'){state.topmost=!state.topmost;updateTopmost();send({cmd:'topmost'});}
    else if(act==='rotate'){send({cmd:'rotate'})}
    else if(act==='screenshot'){send({cmd:'screenshot'})}
    else if(act==='vol_up'){send({cmd:'vol_up'})}
    else if(act==='vol_down'){send({cmd:'vol_down'})}
    else if(act==='arrange'){send({cmd:'arrange'})}
    else if(act==='collapse'){showExpanded(false);send({cmd:'panel',arg:'collapse'})}
  }
  var ci=e.target.closest('[data-clarity]');
  if(ci){var v=parseInt(ci.getAttribute('data-clarity'));state.clarity=v;updateClarity();send({cmd:'clarity',arg:String(v)});}
  var ba=e.target.closest('[data-bact]');
  if(ba){
    var bact=ba.getAttribute('data-bact'),idx=parseInt(ba.getAttribute('data-idx'));
    if(bact==='primary'){
      var addr=ba.getAttribute('data-addr');
      if(addr){state.primaryAddr=addr;updateBatch();send({cmd:'set_primary',arg:addr});}
      return;
    }
    var item=state.ctrlList[idx];if(!item)return;
    if(bact==='toggle'){item.on=!item.on;updateBatch();send({cmd:'ctrl_toggle',arg:item.addr});}
    else if(bact==='del'){state.ctrlList.splice(idx,1);updateBatch();send({cmd:'ctrl_remove',arg:item.addr});}
  }
  if(e.target.id==='ip-btn')doSetIPLocation();
  if(e.target.id==='sms-btn')doSendSMS();
  if(e.target.id==='contact-btn')doAddContact();
  if(e.target.id==='cam-btn')doToggleCameraPush();
  if(e.target.id==='speaker-btn'){
    var btn=$('#speaker-btn');
    var off=btn.classList.contains('speaker-on');
    send({cmd:'speaker_toggle',arg:off?'1':'0'});
  }
  if(e.target.id==='upload-dropzone'||e.target.closest('#upload-dropzone'))
    send({cmd:'upload_file',autogrant:uploadAutoGrant()}); // 走系统文件对话框（Go 直读文件，不经 base64，支持任意大小）
});
document.addEventListener('keyup',function(e){
  if(e.target.id==='ip-input'&&e.key==='Enter')doSetIPLocation();
});

updateClarity();updateTopmost();updateBatch();updateConn();

