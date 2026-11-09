function doSetIPLocation(){
  if(ipLocBusy)return;
  var ip=$('#ip-input').value.trim(),lang=ipLangVal;
  if(ip&&!/^(\d{1,3}\.){3}\d{1,3}$/.test(ip)){
    $('#ip-status').textContent='IP 格式不正确';
    $('#ip-status').className='status-msg err';return;
  }
  ipLocBusy=true;
  $('#ip-btn').disabled=true;
  $('#ip-status').textContent='设置中...';
  $('#ip-status').className='status-msg';
  send({cmd:'ip_location',arg:ip,lang:lang});
}

function doSendSMS(){
  if(smsBusy)return;
  var addr=$('#sms-addr').value.trim(),body=$('#sms-body').value.trim();
  if(!addr||!body)return;
  smsBusy=true;
  $('#sms-btn').disabled=true;
  $('#sms-status').textContent='发送中...';
  $('#sms-status').className='status-msg';
  send({cmd:'send_sms',address:addr,mbody:body});
}

function doAddContact(){
  if(contactBusy)return;
  var name=$('#contact-name').value.trim(),tel=$('#contact-tel').value.trim();
  if(!name||!tel)return;
  contactBusy=true;
  $('#contact-btn').disabled=true;
  $('#contact-status').textContent='添加中...';
  $('#contact-status').className='status-msg';
  send({cmd:'add_contact',name:name,tel:tel});
}

var uploadBusy=false;
var uploadDropzone=$('#upload-dropzone');

// 读取「自动授予运行时权限」勾选（仅对 .apk 生效；上传命令统一带 autogrant 字段，Go 端按扩展名取舍）
function uploadAutoGrant(){var el=$('#upload-autogrant');return !!(el&&el.checked);}

function doUploadFile(file){
  if(uploadBusy||!file)return;
  // JS drop 兜底路径（仅旧 WebView2 运行时；新运行时拖放穿透宿主走流式无限制）。
  // 超大文件 FileReader/base64 可能失败——失败时引导走点击选择（任意大小）。
  uploadBusy=true;
  $('#upload-status').textContent='读取文件...';
  $('#upload-status').className='status-msg';
  $('#upload-file-row').style.display='flex';
  $('#upload-filename').textContent=file.name;
  var reader=new FileReader();
  reader.onload=function(){
    var b64=reader.result.split(',')[1];
    $('#upload-status').textContent='上传中...';
    send({cmd:'upload_file_base64',name:file.name,data:b64,autogrant:uploadAutoGrant()});
  };
  reader.onerror=function(){
    uploadBusy=false;
    $('#upload-status').textContent='读取文件失败，请改用点击选择上传';
    $('#upload-status').className='status-msg err';
  };
  reader.readAsDataURL(file);
}

if(uploadDropzone){
  uploadDropzone.addEventListener('dragover',function(e){
    e.preventDefault();e.stopPropagation();uploadDropzone.classList.add('dragover');
  });
  uploadDropzone.addEventListener('dragleave',function(e){
    e.preventDefault();uploadDropzone.classList.remove('dragover');
  });
  uploadDropzone.addEventListener('drop',function(e){
    e.preventDefault();e.stopPropagation();uploadDropzone.classList.remove('dragover');
    if(e.dataTransfer.files.length>0)doUploadFile(e.dataTransfer.files[0]);
  });
}

window.updateState=function(s){
  if(s.clarity!==undefined){state.clarity=s.clarity;updateClarity();}
  if(s.topmost!==undefined){state.topmost=s.topmost;updateTopmost();}
  if(s.rotated!==undefined)state.rotated=s.rotated;
  var batchChanged=false;
  if(s.ctrlList!==undefined){state.ctrlList=s.ctrlList;batchChanged=true;}
  if(s.allCtrlList!==undefined){state.allCtrlList=s.allCtrlList;batchChanged=true;}
  if(s.primaryAddr!==undefined){state.primaryAddr=s.primaryAddr;batchChanged=true;}
  if(s.selfAddr!==undefined){state.selfAddr=s.selfAddr;batchChanged=true;}
  if(s.controlled!==undefined){state.controlled=s.controlled;batchChanged=true;}
  if(batchChanged)updateBatch();
  if(s.deviceAddr!==undefined){state.deviceAddr=s.deviceAddr;updateConn();}
  if(s.connState!==undefined){state.connState=s.connState;updateConn();}
  if(s.ipLocOK!==undefined){
    ipLocBusy=false;$('#ip-btn').disabled=false;
    if(s.ipLocOK){
      $('#ip-status').textContent='设置成功';$('#ip-status').className='status-msg ok';
      lastSetIP=$('#ip-input').value;$('#ip-last').textContent=lastSetIP;$('#ip-last-row').style.display='block';
    }else{$('#ip-status').textContent=s.ipLocErr||'设置失败';$('#ip-status').className='status-msg err';}
  }
  if(s.smsOK!==undefined){
    smsBusy=false;$('#sms-btn').disabled=false;
    if(s.smsOK){$('#sms-status').textContent='发送成功';$('#sms-status').className='status-msg ok';}
    else{$('#sms-status').textContent=s.smsErr||'发送失败';$('#sms-status').className='status-msg err';}
  }
  if(s.smsEnabled!==undefined){
    var el=$('#sb-sms'),el2=$('#sb-upload');
    if(el)el.style.display=s.smsEnabled?'':'none';
    if(el2)el2.style.display=s.smsEnabled?'':'none';
    if(!s.smsEnabled&&(activeTab==='sms'||activeTab==='upload')){switchTab('clarity');showExpanded(true);}
  }
  if(s.contactOK!==undefined){
    contactBusy=false;$('#contact-btn').disabled=false;
    if(s.contactOK){
      $('#contact-status').textContent='添加成功';$('#contact-status').className='status-msg ok';
      $('#contact-name').value='';$('#contact-tel').value='';
    }else{$('#contact-status').textContent=s.contactErr||'添加失败';$('#contact-status').className='status-msg err';}
  }
  if(s.uploadFile!==undefined){
    $('#upload-file-row').style.display='flex';$('#upload-filename').textContent=s.uploadFile;
    // 非 .apk 文件置灰勾选框（pm install -g 不适用，/installapks 不支持 -g）
    var ag=$('#upload-autogrant'),agRow=$('#upload-autogrant-row');
    var isApk=/\.apk$/i.test(s.uploadFile);
    if(ag){ag.disabled=!isApk;if(!isApk)ag.checked=false;}
    if(agRow)agRow.style.opacity=isApk?'':'0.45';
  }
  if(s.uploadPct!==undefined){
    // 上传/安装进度：文本状态 + 进度条都在侧边栏上传区内（无视频区 overlay）
    uploadBusy=true; // upload_file 命令路径（系统对话框）前端此前无 busy 态,收到首个进度时补上
    var bar=$('#upload-progress'),fill=$('#upload-progress-bar');
    if(bar)bar.style.display='block';
    if(fill)fill.style.width=s.uploadPct+'%';
    if(s.uploadDetail!==undefined){
      $('#upload-status').textContent=s.uploadDetail;
      $('#upload-status').className='status-msg';
    }
  }
  if(s.uploadOK!==undefined){
    uploadBusy=false;
    var pb=$('#upload-progress');
    if(pb)pb.style.display='none';
    var fb=$('#upload-progress-bar');
    if(fb)fb.style.width='0%';
    if(s.uploadOK){$('#upload-status').textContent='上传成功';$('#upload-status').className='status-msg ok';}
    else{$('#upload-status').textContent=s.uploadErr||'上传失败';$('#upload-status').className='status-msg err';}
  }
  if(s.camAvailable!==undefined){
    var camBtn=$('#sb-cam');
    if(camBtn)camBtn.style.display=s.camAvailable?'':'none';
    if(s.camAvailable){
      if(!camListLoaded){loadCameraList();}
      if(!micListLoaded){loadMicList();}
    }
  }
  if(s.camRunning!==undefined||s.camOtherInfo!==undefined){
    updateCamUI(s.camRunning,s.camOtherInfo||'');
  }
  if(s.camSource!==undefined){
    state.camSource=s.camSource;
    // 当前源回填（伪源 value 带 "screen:" 前缀；真实摄像头按名字匹配）。
    // 面板重载后 select 重建为空，这里回填避免把正在推流的会话误判为未推流。
    var camSel=$('#cam-select');
    if(camSel&&camSel.options.length>0){
      var curVal=isPseudoSource(s.camSource)?s.camSource:'screen:'+s.camSource;
      for(var i=0;i<camSel.options.length;i++){
        if(camSel.options[i].value===curVal){camSel.selectedIndex=i;break;}
      }
    }
  }
  if(s.speakerOff!==undefined){
    var spBtn=$('#speaker-btn');
    var spStatus=$('#speaker-status');
    if(spBtn){
      if(s.speakerOff){
        spBtn.textContent='开启听筒';
        spBtn.className='accent-btn';
        if(spStatus){spStatus.textContent='已静音';spStatus.className='status-msg';}
      }else{
        spBtn.textContent='关闭听筒';
        spBtn.className='accent-btn speaker-on';
        if(spStatus){spStatus.textContent='播放中';spStatus.className='status-msg ok';}
      }
      spBtn.disabled=false;
    }
  }
  if(s.kmEnabled!==undefined){
    km.enabled=s.kmEnabled;
    kmRenderSwitch();
  }
};

document.addEventListener('dragover',function(e){e.preventDefault();});
document.addEventListener('drop',function(e){e.preventDefault();});

window.addEventListener('keydown',function(e){
  if(e.target.tagName==='INPUT'||e.target.tagName==='TEXTAREA')return;
  window.postToHost({cmd:'kbd',type:'down',key:e.key,code:e.code,ctrlKey:e.ctrlKey,shiftKey:e.shiftKey,altKey:e.altKey});
  e.stopPropagation();e.preventDefault();
},true);
window.addEventListener('keyup',function(e){
  if(e.target.tagName==='INPUT'||e.target.tagName==='TEXTAREA')return;
  window.postToHost({cmd:'kbd',type:'up',key:e.key,code:e.code,ctrlKey:e.ctrlKey,shiftKey:e.shiftKey,altKey:e.altKey});
  e.stopPropagation();e.preventDefault();
},true);

// ─── 摄像头推流 / 麦克风推流 / 听筒 ───────────────────────────────────
var camListLoaded=false;
var micListLoaded=false;
var camBusy=false;
var micBusy=false;

function loadCameraList(){
  if(camListLoaded)return;
  var sel=$('#cam-select');
  if(!sel)return;
  sel.innerHTML='<option value="">加载中...</option>';
  sel.disabled=true;
  send({cmd:'cam_list'});
}

function loadMicList(){
  if(micListLoaded)return;
  var sel=$('#mic-select');
  if(!sel)return;
  sel.innerHTML='<option value="">加载中...</option>';
  sel.disabled=true;
  send({cmd:'mic_list'});
}

// 伪源（虚拟相机）：value 用 "screen:" 前缀与真实摄像头名区分。
// 「窗口画面」(screenDevice) 选项已下架（Go 侧 captureRendererFrame 路径保留，
// 旧 camSource 值仍可回落兼容）；「电脑桌面」(screenDesktop) 一直未暴露。
var SCREEN_DESKTOP_VAL='screen:'+('screenDesktop');
// 被遮挡窗口（WGC CreateForWindow）：抓 DWM 合成前的窗口内容（抓的是窗口自己）。
// 已被「桌面画面捕获」取代（后者才是抓被挡住的桌面），UI 不再暴露，
// 常量保留供旧播放器/历史配置回落（见 camera_push_wgc.go）。
var SCREEN_WINDOW_VAL='screen:'+('screenWindow');
// 桌面画面捕获（WGC CreateForMonitor + WDA_EXCLUDEFROMCAPTURE）：
// 推流期间投屏窗口从捕获结果消失（屏幕上照常可见），抓显示器合成桌面后
// 按投屏窗口矩形裁剪 → 得到被窗口挡住的真实桌面内容；输出尺寸按窗口矩形
// 比例（不补黑边）。Win10 1903+ 且新版 d3d11render.dll 才可用（见
// camera_push_winrect.go）。限制：推流中窗口跨屏移动需停推重开；窗口
// resize 会触发视频路重启（约 1s 闪断）。
var SCREEN_WINDOW_RECT_VAL='screen:'+('screenWindowRect');

function isPseudoSource(val){return val&&val.indexOf('screen:')===0;}
function pseudoSrc(val){return val.substring(7);}

window.updateCameraList=function(cameras){
  camListLoaded=true;
  var sel=$('#cam-select');
  if(!sel)return;
  sel.innerHTML='';
  sel.appendChild(new Option('不推视频',''));
  // 扫码伪源组：只保留「桌面画面捕获」（WGC monitor + WDA 隐身投屏窗口）。
  // 「窗口画面」（screenDevice）与「电脑桌面」（screenDesktop）不再暴露；
  // Go 侧各源实现代码保留，旧 camSource 值仍可回落兼容。
  var g1=document.createElement('optgroup');
  g1.label='扫码源（虚拟相机）';
  g1.appendChild(new Option('桌面画面捕获',SCREEN_WINDOW_RECT_VAL));
  sel.appendChild(g1);
  if(!cameras||cameras.length===0){
    sel.disabled=false;
    var btn=$('#cam-btn');if(btn)btn.disabled=false;
    return;
  }
  var g2=document.createElement('optgroup');
  g2.label='摄像头';
  cameras.forEach(function(name){
    var opt=document.createElement('option');
    opt.value=name;opt.textContent=name;
    g2.appendChild(opt);
  });
  sel.appendChild(g2);
  // 回显当前推流源（面板重载后 select 重置为空，按值回填避免再发消息误停推流）。
  // 伪源 value 带 "screen:" 前缀；camSource 是纯源名（screenDevice 等），
  // 伪源统一拼 "screen:" 前缀后与 option 匹配；真实摄像头按名字匹配。
  // cam_list 响应到达晚于 updateState 时由此兜底（state.camSource 已被 updateState 记录）。
  if(state.camSource){
    var curVal=isPseudoSource(state.camSource)?state.camSource:'screen:'+state.camSource;
    for(var i=0;i<sel.options.length;i++){
      if(sel.options[i].value===curVal){sel.selectedIndex=i;break;}
    }
  }
  sel.disabled=false;
  var btn=$('#cam-btn');if(btn)btn.disabled=false;
};

window.updateMicList=function(mics){
  micListLoaded=true;
  var sel=$('#mic-select');
  if(!sel)return;
  sel.innerHTML='';
  if(!mics||mics.length===0){
    sel.innerHTML='<option value="">未找到麦克风</option>';
    sel.disabled=true;
    var btn=$('#mic-btn');if(btn)btn.disabled=true;
    return;
  }
  mics.forEach(function(name){
    var opt=document.createElement('option');
    opt.value=name;opt.textContent=name;
    sel.appendChild(opt);
  });
  sel.disabled=false;
  var btn=$('#mic-btn');if(btn)btn.disabled=false;
};

function updateCamUI(running,otherInfo){
  var btn=$('#cam-btn');
  var status=$('#cam-status');
  if(!btn)return;
  if(running){
    btn.textContent='停止推流';
    btn.className='accent-btn cam-stop';
    btn.disabled=false;
    if(status){status.textContent='推流中';status.className='status-msg ok';}
  }else if(otherInfo){
    btn.textContent='切换到此设备';
    btn.className='accent-btn cam-switch';
    btn.disabled=false;
    if(status){status.textContent='正在推流到 '+otherInfo;status.className='status-msg';}
  }else{
    btn.textContent='开始推流';
    btn.className='accent-btn';
    btn.disabled=false;
    if(status){status.textContent='';status.className='status-msg';}
  }
}

function doToggleCameraPush(){
  if(camBusy)return;
  camBusy=true;
  var btn=$('#cam-btn');
  var camSel=$('#cam-select');
  var micSel=$('#mic-select');
  if(!btn)return;
  var action='cam_start';
  if(btn.classList.contains('cam-stop')){
    action='cam_stop';
  }else if(btn.classList.contains('cam-switch')){
    action='cam_switch';
  }
  var camName=camSel?camSel.value:'';
  var micName=micSel?micSel.value:'';
  var msg={cmd:action,arg:camName,mic:micName};
  if(isPseudoSource(camName)){
    msg.arg='';            // 伪源不携带摄像头名
    msg.src=pseudoSrc(camName);
  }
  send(msg);
  setTimeout(function(){camBusy=false;},500);
}
