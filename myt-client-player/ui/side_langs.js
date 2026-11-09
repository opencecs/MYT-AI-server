var LANGS=[
  ["","不设置"],["ab","Аҧсуа"],["af","Afrikaans"],["ak","Akan"],["am","አማርኛ"],["an","Aragonés"],
  ["ar","العربية"],["as","অসমীয়া"],["av","Авар"],["ay","Aymar"],["az","Azərbaycan"],
  ["ba","Башҡорт"],["be","Беларуская"],["bg","Български"],["bh","भोजपुरी"],["bi","Bislama"],
  ["bm","Bamanankan"],["bn","বাংলা"],["bo","བོད་སྐད"],["br","Brezhoneg"],["bs","Bosanski"],
  ["ca","Català"],["ce","Нохчийн"],["ch","Chamorro"],["co","Corsu"],["cr","ᓀᐦᐃᔭᐍᐏᐣ"],
  ["cs","Čeština"],["cu","Славѣнскъ"],["cv","Чӑвашла"],["cy","Cymraeg"],["da","Dansk"],
  ["de","Deutsch"],["dv","ދިވެހި"],["dz","རྫོང་ཁ"],["ee","Eʋegbe"],["el","Ελληνικά"],
  ["en","English"],["eo","Esperanto"],["es","Español"],["et","Eesti"],["eu","Euskara"],
  ["fa","فارسی"],["ff","Fulfulde"],["fi","Suomi"],["fj","Vosa Vakaviti"],["fo","Føroyskt"],
  ["fr","Français"],["fy","Frysk"],["ga","Gaeilge"],["gd","Gàidhlig"],["gl","Galego"],
  ["gn","Avañe'ẽ"],["gu","ગુજરાતી"],["gv","Gaelg"],["ha","Hausa"],["he","עברית"],
  ["hi","हिन्दी"],["ho","Hiri Motu"],["hr","Hrvatski"],["ht","Kreyòl"],["hu","Magyar"],
  ["hy","Հայերեն"],["hz","OtsiHerero"],["ia","Interlingua"],["id","Bahasa Indonesia"],
  ["ie","Interlingue"],["ig","Igbo"],["ii","ꆈꌠꉙ"],["ik","Iñupiaq"],["in","Indonesia"],
  ["io","Ido"],["is","Íslenska"],["it","Italiano"],["iu","ᐃᓄᒃᑎᑐᑦ"],["ja","日本語"],
  ["jv","Basa Jawa"],["ka","ქართული"],["kg","Kikongo"],["ki","Gĩkũyũ"],["kj","Kuanyama"],
  ["kk","Қазақша"],["kl","Kalaallisut"],["km","ខ្មែរ"],["kn","ಕನ್ನಡ"],["ko","한국어"],
  ["kr","Kanuri"],["ks","कॉशुर"],["ku","Kurdî"],["kv","Коми"],["kw","Kernewek"],
  ["ky","Кыргызча"],["la","Latina"],["lb","Lëtzebuergesch"],["lg","Luganda"],["li","Limburgs"],
  ["ln","Lingála"],["lo","ລາວ"],["lt","Lietuvių"],["lu","Kiluba"],["lv","Latviešu"],
  ["mg","Malagasy"],["mh","Kajin M̧ajeļ"],["mi","Te Reo Māori"],["mk","Македонски"],
  ["ml","മലയാളം"],["mn","Монгол"],["mo","Moldovenească"],["mr","मराठी"],["ms","Bahasa Melayu"],
  ["mt","Malti"],["my","ဗမာစာ"],["na","Dorerin Naoero"],["nb","Norsk Bokmål"],
  ["nd","isiNdebele"],["ne","नेपाली"],["ng","Oshiwambo"],["nl","Nederlands"],["nn","Norsk Nynorsk"],
  ["no","Norsk"],["nr","isiNdebele"],["nv","Diné bizaad"],["ny","Chichewa"],["oc","Occitan"],
  ["oj","ᐊᓂᔑᓈᐯᒧᐎᓐ"],["om","Afaan Oromoo"],["or","ଓଡ଼ିଆ"],["os","Ирон"],["pa","ਪੰਜਾਬੀ"],
  ["pi","पालि"],["pl","Polski"],["ps","پښتو"],["pt","Português"],["qu","Runasimi"],
  ["rm","Rumantsch"],["rn","Kirundi"],["ro","Română"],["ru","Русский"],["rw","Kinyarwanda"],
  ["sa","संस्कृतम्"],["sc","Sardu"],["sd","سنڌي"],["se","Davvisámegiella"],["sg","Sängö"],
  ["si","සිංහල"],["sk","Slovenčina"],["sl","Slovenščina"],["sm","Gagana Sāmoa"],
  ["sn","chiShona"],["so","Soomaali"],["sq","Shqip"],["sr","Српски"],["ss","SiSwati"],
  ["st","Sesotho"],["su","Basa Sunda"],["sv","Svenska"],["sw","Kiswahili"],["ta","தமிழ்"],
  ["te","తెలుగు"],["tg","Тоҷикӣ"],["th","ไทย"],["ti","ትግርኛ"],["tk","Türkmençe"],
  ["tl","Filipino"],["tn","Setswana"],["to","Lea Faka-Tonga"],["tr","Türkçe"],["ts","Xitsonga"],
  ["tt","Татарча"],["tw","Twi"],["ty","Reo Tahiti"],["ug","ئۇيغۇرچە"],["uk","Українська"],
  ["ur","اردو"],["uz","Oʻzbekcha"],["ve","Tshivenḓa"],["vi","Tiếng Việt"],["vo","Volapük"],
  ["wa","Walon"],["wo","Wolof"],["xh","isiXhosa"],["yi","ייִדיש"],["yo","Yorùbá"],
  ["za","Vahcuengh"],["zh","中文"],["zu","isiZulu"]
];

(function(){
  var drop=document.getElementById('ip-lang-drop');
  if(!drop) return;
  var html='';
  for(var i=0;i<LANGS.length;i++){
    var code=LANGS[i][0],name=LANGS[i][1];
    var label=code?name+' ('+code+')':name;
    html+='<div class="sselect-opt" data-val="'+code+'">'+label+'</div>';
  }
  drop.innerHTML=html;
})();

var ipLangVal='';
(function(){
  var inp=document.getElementById('ip-lang');
  var drop=document.getElementById('ip-lang-drop');
  var opts=drop?drop.querySelectorAll('.sselect-opt'):[];
  var open=false;
  function setOpen(v){
    open=v;
    if(drop) drop.classList.toggle('open',v);
    if(v){inp.value='';filterOpts('');inp.focus();}
    else{restoreDisplay();}
  }
  function restoreDisplay(){
    if(!ipLangVal){inp.value='';return;}
    for(var i=0;i<opts.length;i++){
      if(opts[i].getAttribute('data-val')===ipLangVal){
        inp.value=opts[i].textContent;return;
      }
    }
    inp.value='';
  }
  function filterOpts(q){
    for(var i=0;i<opts.length;i++){
      var t=opts[i].textContent.toLowerCase(),v=opts[i].getAttribute('data-val');
      var match=!q||t.indexOf(q)>=0||v.indexOf(q)>=0;
      opts[i].classList.toggle('hidden',!match);
      opts[i].classList.toggle('sel',ipLangVal===v);
    }
  }
  inp.addEventListener('focus',function(){if(!open)setOpen(true);});
  inp.addEventListener('input',function(){filterOpts(inp.value.toLowerCase());});
  document.addEventListener('mousedown',function(e){
    if(!open)return;
    var wrap=document.getElementById('ip-lang-wrap');
    if(wrap&&wrap.contains(e.target))return;
    open=false;
    if(drop)drop.classList.remove('open');
    restoreDisplay();inp.blur();
  });
  if(drop)drop.addEventListener('mousedown',function(e){
    var o=e.target.closest('.sselect-opt');
    if(!o)return;
    e.preventDefault();
    ipLangVal=o.getAttribute('data-val');
    open=false;
    drop.classList.remove('open');
    restoreDisplay();inp.blur();
  });
})();
