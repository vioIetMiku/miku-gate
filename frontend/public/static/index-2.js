window.MikuTheme.bind();
const $ = s => document.querySelector(s);
const ICON = {
  copy:'<svg viewBox="0 0 24 24"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>',
  stop:'<svg viewBox="0 0 24 24"><rect x="6" y="6" width="12" height="12" rx="1"/></svg>',
  redo:'<svg viewBox="0 0 24 24"><path d="M21 12a9 9 0 1 1-3-6.7L21 8"/><path d="M21 3v5h-5"/></svg>',
  ok:'<svg viewBox="0 0 24 24"><path d="M20 6 9 17l-5-5"/></svg>',
  bad:'<svg viewBox="0 0 24 24"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>',
  run:'<svg viewBox="0 0 24 24" class="spin"><path d="M21 12a9 9 0 1 1-6.2-8.5"/></svg>',
  wait:'<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9"/></svg>',
  plus:'<svg viewBox="0 0 24 24"><path d="M12 5v14"/><path d="M5 12h14"/></svg>',
  trash:'<svg viewBox="0 0 24 24"><path d="M3 6h18"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>',
  x:'<svg viewBox="0 0 24 24"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>',
  lock:'<svg viewBox="0 0 24 24" class="lock"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>'
};

// 界面挂在随机前缀下，请求一律走相对路径
async function api(path, opts){
  const r = await fetch(path.replace(/^\//, ''), opts);
  const d = await r.json().catch(()=>null);
  if(!r.ok){
    if(d && d.code==='management_moved' && d.url) managementMoved(d.url);
    const fallback=({400:'请求未被正确处理，请刷新当前登录地址后重试。',503:'管理服务暂时不可用，请稍后重试；若刚切换域名，请打开新的登录地址。'})[r.status];
    const err=new Error(d && d.error || fallback || ('请求失败（HTTP '+r.status+'）'));
    err.status=r.status;err.code=d && d.code;throw err;
  }
  if(!d) throw new Error('管理服务返回了无效响应，请稍后重试。');
  return d;
}
function esc(s){ return String(s == null ? '' : s).replace(/[&<>"']/g, c =>
  ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }

let toastTimer;
function toast(msg, bad){
  const el = $('#toast');
  el.textContent = msg;
  el.className = 'toast show' + (bad ? ' bad' : '');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { el.className = 'toast'; }, 2400);
}
async function copy(text){
  // navigator.clipboard 只在 HTTPS/localhost 下存在，而面板通常是 http://IP 访问，
  // 所以必须留一条 execCommand 兜底路径，否则复制在正常使用场景里必然失败。
  if(navigator.clipboard && window.isSecureContext){
    try{ await navigator.clipboard.writeText(text); toast('已复制'); return; }
    catch(e){}
  }
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.setAttribute('readonly', '');
  // 放在视口内但不可见：置于视口外会让 iOS 在聚焦时滚动页面
  ta.style.cssText = 'position:fixed;top:0;left:0;width:1px;height:1px;opacity:0;padding:0;border:0';
  document.body.appendChild(ta);
  const prev = document.activeElement;
  ta.focus();
  ta.setSelectionRange(0, ta.value.length);
  let ok = false;
  try{ ok = document.execCommand('copy'); }catch(e){}
  ta.remove();
  if(prev && prev.focus) prev.focus();
  toast(ok ? '已复制' : '复制失败，请手动选中', !ok);
}

let view = {exits:[], direct:[], panel:'', backend:'', public_ip:''};
let inbounds = [];
$('#heroSubBtn').onclick=()=>$('#subBtn').click();

// 自建模式下入站由 fanout 自己管，界面要提供新建入口；
// 接管 3x-ui 时入站归面板管，这里只读不写。
function isNative(){ return view.backend === 'native' || view.backend === 'sing-box'; }
// xray-cf-lite 模式下节点归它管，fanout 只改路由，界面不给新建入口
function isXCL(){ return view.backend === 'xray-cf-lite'; }
const BACKEND_NAME = {'native':'自建 Xray', 'sing-box':'自建 sing-box', 's-ui':'S-UI', '3x-ui':'3x-ui', 'xray-cf-lite':'xray-cf-lite'};
function backendName(){ return BACKEND_NAME[view.backend] || '3x-ui'; }

const STATUS = {up:'已连通', starting:'连接中', failed:'失败', stopped:'已停止'};


const BROWSER_LATENCY_INTERVAL=30000;
const browserLatencyKey='miku.browser-latency.'+(location.pathname || '/');
let clientLatencyMS=null, clientLatencyChecked=0, browserPingBusy=false, browserPingError='', browserPingAttempted=0;
try{
  const saved=JSON.parse(sessionStorage.getItem(browserLatencyKey) || 'null'),now=Date.now();
  if(saved && Number.isFinite(saved.attempted) && saved.attempted>0 && saved.attempted<=now){
    browserPingAttempted=saved.attempted;
    if(Number.isFinite(saved.ms) && saved.ms>=0 && Number.isFinite(saved.checked) && saved.checked>0 && saved.checked<=now && now-saved.checked<60000){
      clientLatencyMS=saved.ms;clientLatencyChecked=saved.checked;
    }
    if(now-saved.attempted<BROWSER_LATENCY_INTERVAL && saved.error===true) browserPingError='本地到VPS测量失败，稍后自动重试';
  }
}catch(e){}
function saveBrowserLatency(){
  try{sessionStorage.setItem(browserLatencyKey,JSON.stringify({attempted:browserPingAttempted,ms:clientLatencyMS,checked:clientLatencyChecked,error:!!browserPingError}));}catch(e){}
}
function latencyOf(exit){
  const gate=exit.gate_latency;
  const method=gate && ({icmp:'ICMP',tcp:'TCP',tunnel_tcp:'隧道 TCP'})[gate.method] || '';
  const approx=!!gate && gate.method==='tunnel_tcp';
  const explanation=approx?'参考总延迟 = 本地→VPS 浏览器往返时间 + 经VPN隧道到检测站的TCP时间，包含出口到检测站开销':'总延迟 = 本地→VPS 浏览器往返时间 + VPS→VPN Gate '+(method || '网络')+' 往返时间；ICMP不响应时自动改测配置中的TCP端口';
  if(exit.status!=='up') return {label:'—',title:explanation,parts:''};
  if(!gate || gate.ms==null || !Number.isFinite(gate.ms) || gate.ms<0 || !Number.isFinite(Date.parse(gate.checked)) || Date.now()-Date.parse(gate.checked)>90000){
    return {label:gate && gate.error && !gate.pending?'暂未测得':'测量中…',title:gate && gate.error?gate.error+'；'+explanation:explanation,parts:'',method:'',approx:false};
  }
  if(clientLatencyMS==null || Date.now()-clientLatencyChecked>60000){
    return {label:browserPingError?'本地测量失败':'测量中…',title:browserPingError || explanation,parts:''};
  }
  const total=clientLatencyMS+gate.ms;
  const parts='本地 '+clientLatencyMS.toFixed(1)+' + '+(approx?'隧道':'Gate')+' '+gate.ms.toFixed(1)+' ms';
  return {label:Math.round(total)+' ms',title:parts+'；'+explanation,parts:parts,method:method,approx:approx};
}
async function measureBrowserPing(){
  const now=Date.now();
  if(managementMovedURL || browserPingBusy || document.hidden || (browserPingAttempted && now-browserPingAttempted<BROWSER_LATENCY_INTERVAL)) return;
  browserPingAttempted=now;
  saveBrowserLatency();
  browserPingBusy=true;
  const samples=[];
  try{
    // Warm the same-origin connection, then use the median of three round trips.
    for(let i=0;i<4;i++){
      const controller=new AbortController(),timer=setTimeout(()=>controller.abort(),8000);
      try{
        const started=performance.now();
        const response=await fetch('api/ping?_='+Date.now()+'-'+i,{cache:'no-store',credentials:'same-origin',signal:controller.signal});
        await response.arrayBuffer();
        if(response.status!==204) throw new Error('本地到VPS测量未完成，请检查登录和网络');
        const ms=performance.now()-started;
        if(i>0 && Number.isFinite(ms) && ms>=0) samples.push(ms);
      }finally{clearTimeout(timer);}
    }
    if(samples.length<2) throw new Error('本地到VPS测量失败，稍后自动重试');
    samples.sort((a,b)=>a-b);
    clientLatencyMS=samples[Math.floor(samples.length/2)];
    clientLatencyChecked=Date.now();browserPingError='';
  }catch(err){clientLatencyMS=null;browserPingError='本地到VPS测量失败，稍后自动重试';}
  finally{browserPingBusy=false;saveBrowserLatency();renderExits();}
}
document.addEventListener('visibilitychange',()=>{if(!document.hidden) measureBrowserPing();});

let showcaseMode=true;
let preferredTemplateID=0;
const selectedEntrances=new Map();
const expandedCardTools=new Set();
try{showcaseMode=localStorage.getItem('miku.showcase')!=='0';}catch(e){}
function enabledEntrances(exit){return (exit.inbounds || []).filter(i=>i.enable!==false);}
function selectedEntrance(exit){
  const list=enabledEntrances(exit), id=selectedEntrances.get(exit.slot);
  return list.find(i=>i.id===id) || list[0];
}
function displayType(exit){
  const info=exit.ip_info;
  if(!info || !exit.exit_ip) return 'pending';
  return ['residential','datacenter','mobile'].includes(info.ip_type) ? info.ip_type : info.pending?'pending':info.error?'unavailable':'pending';
}
const IP_TYPE_LABEL={residential:'住宅 IP',datacenter:'机房 IP',mobile:'移动网络',pending:'检测中…',unavailable:'检测失败'};
function typeBadge(exit){
  const type=displayType(exit),info=exit.ip_info || {};
  return '<span class="network-type '+type+'" title="'+esc(info.type_reason || (info.error?'检测失败，稍后自动重试':'等待类型检测'))+'">'+IP_TYPE_LABEL[type]+'</span>';
}
function networkOwner(exit){
  const info=exit.ip_info || {},owner=info.organization || info.isp || info.company || (info.error?'归属查询暂不可用':'归属信息查询中…');
  return '<div class="network-owner">'+(info.asn?'<span class="asn-tag">AS'+Number(info.asn)+'</span>':'')+'<strong class="asn-owner" title="'+esc(owner)+'">'+esc(owner)+'</strong></div>';
}
function stabilityNotice(exit){
  const notice=exit.stability;
  if(!notice || !Number.isInteger(notice.reconnects) || notice.reconnects<=3 || !Number.isFinite(Date.parse(notice.until)) || Date.parse(notice.until)<=Date.now()) return '';
  return '<aside class="exit-stability-notice" role="note" aria-label="连接稳定性提示"><svg aria-hidden="true" viewBox="0 0 24 24"><path d="M10.3 4.1 2.8 17.2A2 2 0 0 0 4.5 20h15a2 2 0 0 0 1.7-2.8L13.7 4.1a2 2 0 0 0-3.4 0Z"/><path d="M12 8v5m0 3v.3"/></svg><div><div class="exit-stability-heading"><strong>连接稳定性提示</strong><span>近10分钟自动重连 '+notice.reconnects+' 次</span></div><p>'+esc(notice.message)+'</p></div></aside>';
}
function cardIcon(name){return (ICON[name] || '').replace('<svg ','<svg aria-hidden="true" ');}
const LOCATION_ICON='<svg aria-hidden="true" viewBox="0 0 24 24"><path d="M20 10c0 6-8 11-8 11S4 16 4 10a8 8 0 0 1 16 0Z"/><circle cx="12" cy="10" r="2.5"/></svg>';
const MORE_ICON='<svg aria-hidden="true" viewBox="0 0 24 24"><path d="m6 9 6 6 6-6"/></svg>';
function countryCode(exit){return String(exit.region || '').toUpperCase() || '??';}
function countryName(exit){return exit.country || exit.region || '未知地区';}
function setShowcaseMode(){
  document.body.classList.toggle('showcase-mode',showcaseMode);
  $('#displayModeBtn').setAttribute('aria-pressed',String(showcaseMode));
  $('#displayModeBtn').textContent=showcaseMode ? '管理模式' : '展示模式';
  $('#list').classList.toggle('showcase-grid',showcaseMode);
}
$('#displayModeBtn').onclick=()=>{
  showcaseMode=!showcaseMode;
  try{localStorage.setItem('miku.showcase',showcaseMode?'1':'0');}catch(e){}
  renderExits();
};
function renderShowcase(){
  $('#list').innerHTML=orderedExits().map(e=>{
    const entrances=enabledEntrances(e),entry=selectedEntrance(e),up=e.status==='up';
    const latency=latencyOf(e);
    const ready=up && entry,disabled=up?'':' disabled',error=e.entry_error || e.err;
    const selector=entrances.length>1 ? '<label class="node-card-entry"><span>选择入口</span><select data-card-entry="'+e.slot+'" aria-label="选择此出口的入口">'+entrances.map(i=>'<option value="'+i.id+'"'+(entry.id===i.id?' selected':'')+'>'+esc(entranceLabel(i)+' :'+i.port+(i.remark?' · '+i.remark:''))+'</option>').join('')+'</select></label>' : '';
    return '<article class="node-card'+(up?' connected':'')+'" data-exit-card="'+e.slot+'">'
      +'<div class="node-card-head"><span class="node-card-country">'+countryHTML(e.region,countryName(e))+' <span class="country-code">'+esc(countryCode(e))+'</span></span><span class="node-card-state '+esc(e.status)+'"><span class="dot '+esc(e.status)+'"></span>'+esc(STATUS[e.status] || e.status)+'</span></div>'
      +'<button class="node-card-ip"'+(e.exit_ip?' data-ip="'+esc(e.exit_ip)+'" title="查看 IP 与网络信息"':' disabled')+'>'+esc(e.exit_ip || (e.status==='starting'?'连接中…':'—'))+(e.exit_ip?'<svg aria-hidden="true" viewBox="0 0 24 24"><path d="M7 17 17 7M7 7h10v10"/></svg>':'')+'</button>'
      +'<div class="node-card-meta">'+typeBadge(e)+'<span class="node-card-id"><span>#'+(e.order || e.slot)+'</span><span class="node-card-protocol">'+esc(entry?entranceLabel(entry):'SOCKS5 / HTTP')+'</span></span></div>'
      +'<div class="node-card-network">'+networkOwner(e)+'<div class="node-card-location">'+LOCATION_ICON+'<span>'+esc(exitLocation(e))+'</span></div></div>'
      +'<div class="node-card-facts"><div><small>'+(entry?'节点端口':'代理端口')+'</small><strong>'+(entry?entry.port:e.port)+'</strong></div><div title="'+esc(latency.title)+'"><small>'+(latency.approx?'参考总延迟':'总延迟')+(latency.method?'<span class="latency-method">'+esc(latency.method)+'</span>':'')+'</small><strong'+(/^\d/.test(latency.label)?'':' class="latency-unavailable"')+'>'+esc(latency.label)+'</strong>'+(latency.parts?'<span class="node-card-latency-parts">'+esc(latency.parts)+'</span>':'')+'</div></div>'
      +stabilityNotice(e)+selector+'<div class="node-card-actions"><button class="copy-node" data-copy-node="'+e.slot+'" title="复制当前'+esc(entry?entranceLabel(entry):'')+'节点"'+(ready?'':' disabled')+'><span>复制节点</span>'+(entrances.length>1?'<small>'+esc(String(entry.protocol).toUpperCase())+'</small>':'')+'</button><button class="switch-ip" data-swap="'+e.slot+'"'+(e.status==='starting'?' disabled':'')+'>'+cardIcon('redo')+'<span>切换 IP</span></button></div>'
      +'<details class="node-card-more" data-card-tools="'+e.slot+'"'+(expandedCardTools.has(e.slot)?' open':'')+'><summary><span>代理与管理</span>'+MORE_ICON+'</summary><div class="node-card-proxies"><div class="node-card-proxy-label">链式代理 · 指纹浏览器</div><button data-proxy-copy="socks5" data-slot="'+e.slot+'" title="复制 SOCKS5 代理地址" aria-label="复制 SOCKS5 代理地址"'+disabled+'>'+cardIcon('copy')+'<span>SOCKS5</span></button><button data-proxy-copy="http" data-slot="'+e.slot+'" title="复制 HTTP 代理地址" aria-label="复制 HTTP 代理地址"'+disabled+'>'+cardIcon('copy')+'<span>HTTP</span></button></div><div class="node-card-tools">'+(e.exit_ip?'<button data-ip="'+esc(e.exit_ip)+'">IP 详情</button>':'')+'<button data-cred="'+e.slot+'">代理凭据</button>'+(entry?'<button data-detail="'+entry.id+'">节点详情</button>':e.needs_node?'<button data-repair="'+e.slot+'"'+disabled+'>'+(e.entry_error?'修复入口':'入口准备中')+'</button>':'')+'<button class="stop-exit" data-stop="'+e.slot+'">停止出口</button></div></details>'
      +(error?'<div class="errline">'+esc(error)+'</div>':'')+'</article>';
  }).join('') || '<div class="empty">还没有出口</div>';
}
document.addEventListener('change',e=>{
  if(e.target.matches('[data-card-entry]')){selectedEntrances.set(Number(e.target.dataset.cardEntry),Number(e.target.value));renderExits();}
});
document.addEventListener('toggle',e=>{
  if(e.target.matches('details[data-card-tools]')){
    const slot=Number(e.target.dataset.cardTools);
    if(e.target.open) expandedCardTools.add(slot);else expandedCardTools.delete(slot);
  }
},true);
document.addEventListener('click',async e=>{
  const proxy=e.target.closest('[data-proxy-copy]');
  if(proxy){
    const exit=view.exits.find(x=>x.slot===Number(proxy.dataset.slot));
    if(!exit || exit.status!=='up') return toast('出口尚未连通',true);
    return openProxyGuide(exit.slot,proxy.dataset.proxyCopy);
  }
  const node=e.target.closest('[data-copy-node]');
  if(node){
    const exit=view.exits.find(x=>x.slot===Number(node.dataset.copyNode)),entry=exit && selectedEntrance(exit);
    if(!entry) return;
    node.disabled=true;
    try{const result=await api('/api/exits/links?slot='+exit.slot+'&id='+entry.id);await copy(result.links.join('\n'));}catch(err){toast(err.message,true);}finally{node.disabled=false;}
  }
  const template=e.target.closest('[data-import-template]');
  if(template){preferredTemplateID=Number(template.dataset.importTemplate);openModal('wizard');await loadWizard();}
});

const FLAG_CODES=new Set("AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ CA CC CD CF CG CH CI CK CL CM CN CO CP CR CU CV CW CX CY CZ DE DG DJ DK DM DO DZ EC EE EG EH ER ES ET EU FI FJ FK FM FO FR GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU IC ID IE IL IM IN IO IQ IR IS IT JE JM JO JP KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PC PE PF PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM UN US UY UZ VA VC VE VG VI VN VU WF WS XK XX YE YT ZA ZM ZW".split(' '));
function countryHTML(code,label){
  code=String(code || '').trim().toUpperCase();
  const text=String(label || code || '未知地区').replace(/[\u{1F1E6}-\u{1F1FF}]{2}/gu,'').trim();
  return (FLAG_CODES.has(code)?'<img class="country-flag" src="assets/flags/'+code.toLowerCase()+'.svg" alt="'+esc(code)+'" width="24" height="18" decoding="async">':'')+esc(text);
}
let exitSort='fast';try{exitSort=localStorage.getItem('miku.exitSort') || 'fast';}catch(e){}
function orderedExits(){
  if(exitSort==='order') return view.exits.slice();
  const measured=e=>{
    const g=e.gate_latency;
    return g && Number.isFinite(g.ms) && g.ms>=0 && Number.isFinite(Date.parse(g.checked)) && Date.now()-Date.parse(g.checked)<90000?g.ms:Infinity;
  };
  return view.exits.slice().sort((a,b)=>{
    const ready=e=>e.status==='up' && (!e.needs_node || enabledEntrances(e).length>0);
    if(ready(a)!==ready(b)) return ready(a)?-1:1;
    const av=measured(a),bv=measured(b);
    if(av!==bv) return av<bv?-1:1;
    return (a.order || a.slot)-(b.order || b.slot);
  });
}
$('#exitSort').value=exitSort;
$('#exitSort').onchange=()=>{exitSort=$('#exitSort').value;try{localStorage.setItem('miku.exitSort',exitSort);}catch(e){}renderExits();};
function exitPicker(currentHost,attrs,defaultLabel){
  const exits=orderedExits().filter(e=>e.status==='up'),current=exits.find(e=>e.host===currentHost);
  const label=e=>countryHTML(e.region,e.country)+' <small>'+esc(e.exit_ip || e.host)+'</small>';
  return '<div class="exit-picker"><select hidden '+attrs+'>'+exitOptions(currentHost).replace('直连（不走隧道）',defaultLabel)+'</select><details class="exit-picker-menu"><summary>'+(current?label(current):esc(defaultLabel))+'</summary><div class="exit-choice-list" role="group" aria-label="选择出口"><button type="button" data-exit-choice="" aria-pressed="'+!current+'">'+esc(defaultLabel)+'</button>'+exits.map(e=>'<button type="button" data-exit-choice="'+esc(e.host)+'" aria-pressed="'+(e.host===currentHost)+'">'+label(e)+'</button>').join('')+'</div></details></div>';
}
document.addEventListener('click',event=>{
  const button=event.target.closest('[data-exit-choice]');if(!button) return;
  const picker=button.closest('.exit-picker'),select=picker.querySelector('select');
  if(select.disabled) return;
  picker.querySelector('details').open=false;
  if(select.value===button.dataset.exitChoice) return;
  select.dataset.previousValue=select.value;
  select.dataset.previousSummary=picker.querySelector('summary').innerHTML;
  select.value=button.dataset.exitChoice;
  picker.querySelector('summary').innerHTML=button.innerHTML;
  picker.querySelectorAll('[data-exit-choice]').forEach(option=>option.setAttribute('aria-pressed',String(option===button)));
  select.dispatchEvent(new Event('change',{bubbles:true}));
});
function restoreExitPicker(select){
  if(select.dataset.previousValue===undefined) return;
  select.value=select.dataset.previousValue;
  const picker=select.closest('.exit-picker');if(!picker) return;
  picker.querySelector('summary').innerHTML=select.dataset.previousSummary;
  picker.querySelectorAll('[data-exit-choice]').forEach(option=>option.setAttribute('aria-pressed',String(option.dataset.exitChoice===select.value)));
}


function entranceLabel(i){ return i.stack || String(i.protocol || "入口").toUpperCase(); }
function entranceChip(i){ return '<span class="entry-stack">'+esc(entranceLabel(i))+'</span><small>#'+i.id+' · :'+i.port+'</small>'; }

function renderExits(){
  setShowcaseMode();
  const list = $('#list');
  const n = view.exits.length;
  const online=view.exits.filter(e=>e.status==='up');
  $('#onlineCount').textContent=online.length;
  $('#regionCount').textContent=new Set(online.map(e=>e.region).filter(Boolean)).size;
  $('#entryCount').textContent=new Set(online.flatMap(e=>enabledEntrances(e).map(i=>i.id))).size;
  $('#ecount').textContent = n ? n + ' 个' : '';
  $('#exportAll').disabled = !view.exits.some(e => e.inbounds && e.inbounds.length);
  $('#stopall').disabled = !n;

  if(!n){
    list.innerHTML = '<div class="empty">还没有出口'
      + '<div><button class="primary" id="newexit2">'
      + '<svg viewBox="0 0 24 24"><path d="M12 5v14"/><path d="M5 12h14"/></svg>'
      + '新建出口</button></div></div>';
    return;
  }

  if(showcaseMode){renderShowcase();return;}
  list.innerHTML = orderedExits().map(e => {
    const label = e.exit_ip || (e.status === 'starting' ? '连接中…' : '—');
    const chips = enabledEntrances(e).length
      ? enabledEntrances(e).map(i => '<button class="chip entry-chip" data-detail="' + i.id + '" title="'
          + esc((i.remark || '入口 #'+i.id) + ' · ' + entranceLabel(i) + ' :' + i.port) + '">'
          + entranceChip(i) + '</button>').join('')
      : (e.needs_node ? '<button class="chip none" data-repair="'+e.slot+'" title="补建或修复此出口入口">'+(e.entry_error ? '修复入口' : '入口准备中')+'</button>' : '<button class="chip" data-cred="'+e.slot+'">SOCKS5 / HTTP</button>');
    const error = e.entry_error || e.err;
    const err = error && (e.status === 'failed' || e.entry_error)
      ? '<div class="errline" title="' + esc(error) + '">' + esc(error) + '</div>' : '';
    // country 后端已经给成"国旗 中文"，再拼国家码就重复了
    const place = countryHTML(e.region,e.country);
    return '<div class="exit">'
      + '<div class="row">'
      +   '<span class="dot ' + e.status + '" title="' + (STATUS[e.status] || e.status) + '"></span>'
      +   '<span class="exit-number">#' + (e.order || e.slot) + '</span>'
      +   '<span class="exit-location">' + esc(exitLocation(e)) + '</span>'
      +   '<span class="ip">' + esc(label) + '</span>'
      +   '<span class="meta">' + place + ' · ' + esc(e.host) + '</span>'
      +   '<span class="chips">' + chips + '</span>'
      +   '<span class="socks"><button data-cred="' + e.slot + '" title="SOCKS5 / HTTP 访问凭据">'
      +     ICON.lock + ':' + e.port + '</button></span>'
      +   '<span class="acts">'
      +     '<button class="icon" data-swap="' + e.slot + '" title="换一个节点">' + ICON.redo + '</button>'
      +     '<button class="icon" data-stop="' + e.slot + '" title="停止这个出口">' + ICON.stop + '</button>'
      +   '</span>'
      + '</div>' + ipSummary(e) + err + stabilityNotice(e) + '</div>';
  }).join('');
}

function renderImported(){
  let box = $('#importedNodes');
  if(!box){box=document.createElement('div');box.id='importedNodes';$('#list').after(box);}
  const list=view.imported || [];
  box.innerHTML=list.length ? '<section class="bar"><h2>已识别本机节点</h2><span class="count">关联现有入口 · 使用本 VPS 地址</span></section>'+list.map(n=>'<div class="orphan"><div class="orow"><b>'+esc(n.name)+'</b><span class="spacer"></span><span class="dim">'+esc(n.protocol)+' · '+esc(n.server)+':'+n.port+'</span>'+(n.inbound_id ? '<button data-import-template="'+n.inbound_id+'">用作模板</button>' : '')+'<button data-import-copy="'+esc(n.id)+'">复制节点</button><button class="icon" data-import-remove="'+esc(n.id)+'" title="移除此识别记录，保留本机入口">'+ICON.trash+'</button></div></div>').join('') : '';
}

// Only genuine, enabled direct entrances appear in the template area.
function renderOrphans(){
  const box = $('#orphans');
  const list = (view.direct || []).filter(i => i.enable !== false);
  if(!list.length){ box.innerHTML = ''; return; }
  const hasUp = view.exits.some(e => e.status === 'up');
  box.innerHTML = '<div class="orphan"><div class="top">'
    + '<h3>入口节点 / 批量模板</h3><span class="count">' + list.length + ' 个，可选模板或绑定落地</span>'
    + '<span class="spacer"></span>'
    + (isXCL() ? ''
        : '<button data-delorphans="1" title="删除这些入站">' + ICON.trash + '清理</button>')
    + '</div>'
    + list.map(i =>
        '<div class="orow">'
        + '<button class="chip entry-chip" data-detail="' + i.id + '" title="'
        +   esc((i.remark || '入口 #'+i.id) + ' · ' + entranceLabel(i) + ' :' + i.port) + '">'
        +   entranceChip(i) + '</button>'
        + '<span class="spacer"></span>'
        + (hasUp
            ? '<select class="obind" data-tag="' + esc(i.tag) + '">' + exitOptions('') + '</select>'
            : '<span class="dim">先开一个出口</span>')
        + (isXCL() ? ''
            : '<button class="icon danger" data-delone="' + i.id + '" data-name="'
              + esc((i.remark || i.protocol) + ' :' + i.port) + '" title="删除这个入站">'
              + ICON.trash + '</button>')
        + '</div>').join('')
    + '</div>';
}

const expandedJobs=new Set();
document.addEventListener('toggle',event=>{
  if(!event.target.matches('[data-job-history]'))return;
  const id=event.target.dataset.jobHistory;
  if(event.target.open)expandedJobs.add(id);else expandedJobs.delete(id);
},true);
function renderJobs(jobs){
  syncDomainJob(jobs);
  syncQuickSetup(jobs);
	$('#stopall').disabled = !(view.exits || []).length && !jobs.some(j => j.status === 'running' && (j.kind === 'provision' || j.kind === 'quick-setup'));
  const box = $('#jobs');
  box.innerHTML = jobs.map(j => {
    const steps = j.steps.map(s => {
      const ic = {ok:ICON.ok, failed:ICON.bad, running:ICON.run}[s.status] || ICON.wait;
      const t = s.detail ? s.label + ' — ' + s.detail : s.label;
      return '<span class="step ' + s.status + '" title="' + esc(t) + '">' + ic
        + '<span>'+esc(s.status === 'ok' && s.detail ? s.detail : s.label)
        + (s.status === 'failed' && s.detail ? '<small class="step-detail">'+esc(s.detail)+'</small>' : '')+'</span></span>';
    }).join('');
    const close = j.status === 'running' ? (j.kind === 'provision' ? '<button data-provision-stop="' + esc(j.id) + '">停止补齐</button>' : '')
      : '<button class="icon" data-job="' + esc(j.id) + '" title="关闭">' + ICON.x + '</button>';
    const finished=j.status!=='running',tag=finished?'details':'div',head=finished?'summary':'div';
    return '<'+tag+' class="job'+(finished?' job-history':'')+'"'+(finished?' data-job-history="'+esc(j.id)+'"'+(expandedJobs.has(j.id)?' open':''):'')+'><'+head+' class="top"><strong>' + esc(j.summary) + '</strong>'
      + '<span class="count">' + j.done + '/' + j.total + '</span>'
      + '<span class="spacer"></span>' + close + '</'+head+'>'
      + '<div class="steps">' + steps + '</div>' + (j.result_url ? domainLink(j.result_url,'新的管理登录地址') : '') + '</'+tag+'>';
  }).join('');
}

async function poll(){
  if(managementMovedURL) return;
  try{
    view = await api('/api/exits');
    $('#workspaceSync').textContent='实时同步';$('#workspaceLive').classList.remove('offline');
    $('#panel').textContent = view.panel
      ? (backendName() + ': ' + view.panel)
      : (view.panel_info || '');
    // xray-cf-lite 的节点由它自己生成，fanout 这边只管把它们导到哪条出口
    $('#newnode').hidden = isXCL();
    // 链接由 xray-cf-lite 的订阅体系发，fanout 这边导不出来
    $('#exportAll').hidden = isXCL();
    renderExits();
    renderImported();
    renderOrphans();
  }catch(e){$('#workspaceSync').textContent='同步暂不可用';$('#workspaceLive').classList.add('offline');}
  if(managementMovedURL) return;
  try{ renderJobs(await api('/api/jobs') || []); }catch(e){}
}

// ---- 新建向导 ----
let regions = [], region = '', regionsLoaded = false;

const modalReturnFocus=new Map();
function openModal(id){
  const modal=$('#'+id);
  if(!modal.classList.contains('open')) modalReturnFocus.set(id,document.activeElement);
  modal.classList.add('open');
  document.body.classList.add('modal-active');
}
function closeModal(id){
  $('#'+id).classList.remove('open');
  if(!document.querySelector('.modal.open'))document.body.classList.remove('modal-active');
  const previous=modalReturnFocus.get(id);modalReturnFocus.delete(id);
  if(previous && previous.isConnected && previous.focus) previous.focus();
}

document.addEventListener('click', e => {
  const c = e.target.closest('[data-close]');
  if(c) closeModal(c.dataset.close);
});
document.addEventListener('keydown', e => {
  if(e.key === 'Escape') document.querySelectorAll('.modal.open')
    .forEach(m => closeModal(m.id));
});
document.querySelectorAll('.modal').forEach(m => {
  m.onclick = e => { if(e.target === m) closeModal(m.id); };
});

function renderRegions(){
  const kw = $('#rgfilter').value.trim().toLowerCase();
  const list = regions.filter(r => !kw
    || r.code.toLowerCase().includes(kw) || r.name.toLowerCase().includes(kw));
  $('#regions').innerHTML = ['<button class="rg' + (region === '' ? ' sel' : '')
      + '" data-rg=""><b>不限地区</b><em>速度优先</em></button>',
    '<button class="rg' + (region === '*' ? ' sel' : '')
      + '" data-rg="*"><b>每个国家</b><em>' + regions.length + ' 个国家各来几个</em></button>']
    .concat(list.map(r => '<button class="rg' + (region === r.code ? ' sel' : '')
      + '" data-rg="' + esc(r.code) + '"><b>' + countryHTML(r.code,r.name || r.code) + '</b>'
      + '<em>' + r.available + ' 个空闲 · ' + r.best_speed_mbps.toFixed(0) + ' Mbps</em></button>'))
    .join('');
  updateAvail();
}

function availOf(code){
  if(code === '') return regions.reduce((a, r) => a + r.available, 0);
  if(code === '*') return regions.reduce((a, r) => a + r.available, 0);
  const r = regions.find(x => x.code === code);
  return r ? r.available : 0;
}

function updateAvail(){
  const want = Number($('#count').value) || 0;
  const hint = $('#availhint');
  // 选了"每个国家"时，数量的意思是每国几个，提示要给出总条数
  $('#countlabel').textContent = region === '*' ? '每个国家几个' : '数量';
  if(region === '*'){
    const n = regions.length;
    const total = Math.min(n * want, availOf('*'));
    hint.className = 'hint';
    hint.textContent = n
      ? n + ' 个国家 × ' + want + '，一共 ' + total + ' 条出口'
      : '还没有可用节点';
    $('#go').disabled = !n || !want;
    return;
  }
  const avail = availOf(region);
  hint.textContent = avail ? '可用 ' + avail + ' 个节点' : '这个地区没有空闲节点';
  hint.className = 'hint' + (want > avail ? ' bad' : '');
  if(want > avail && avail) hint.textContent = '只剩 ' + avail + ' 个，将全部使用';
  $('#go').disabled = !avail;
}

async function loadWizard(){
  try{
    regions = await api('/api/regions') || [];
    regionsLoaded = true;
    renderRegions();
  }catch(e){ toast('读取地区失败: ' + e.message, true); }

  const sel = $('#tpl');
  // xray-cf-lite 模式不能复制节点，向导退化成"只开出口"，之后在节点详情里挑出口
  if(isXCL()){
    $('#tplwrap').hidden = true;
    sel.innerHTML = '<option value="0">只开出口，不建节点</option>';
    return;
  }
  $('#tplwrap').hidden = false;
  try{
    // 已经挂在出口上的多半是上一批复制出来的，拿它当模板会套娃，
    // 所以把没绑出口的排在前面并默认选中
    const v = await api('/api/exits');
    const free = (v.direct || []).filter(i=>i.enable!==false);
    const seen = new Set(free.map(i=>i.id));
    const bound = (v.exits || []).flatMap(e => e.inbounds || []).filter(i=>{if(i.enable===false || seen.has(i.id)) return false;seen.add(i.id);return true;});
    inbounds = free.concat(bound);
    if(!inbounds.length){
      sel.innerHTML = '<option value="-1">自动创建 VLESS REALITY 入口并绑定</option>';
      $('#tplhint').textContent = '不需要先配置 OpenVPN；点开始后自动选择候选、建立隧道、分配端口并生成节点链接';
      return;
    }
    const opt = i => '<option value="' + i.id + '">'
      + esc(i.remark || ('入口 #' + i.id)) + ' · ' + esc(entranceLabel(i))
      + ' :' + i.port + '</option>';
    sel.innerHTML =
      (free.length ? '<optgroup label="未绑定出口">' + free.map(opt).join('') + '</optgroup>' : '')
      + (bound.length ? '<optgroup label="已挂在出口上">' + bound.map(opt).join('') + '</optgroup>' : '')
      + '<option value="-1">自动创建 VLESS REALITY 入口</option><option value="0">仅 SOCKS5 / HTTP</option>';
    if(preferredTemplateID && Array.from(sel.options).some(o=>Number(o.value)===preferredTemplateID)) sel.value=String(preferredTemplateID);
    $('#tplhint').textContent = '每个出口独立分配入口端口；自建模式重复使用同一模板时复用现有入口';
  }catch(e){
    sel.innerHTML = '<option value="0">' + backendName() + '不可用</option>';
    $('#tplhint').textContent = e.message;
  }
}

document.addEventListener('click', e => {
  if(e.target.closest('#newexit') || e.target.closest('#newexit2')){
    openModal('wizard');
    if(!regionsLoaded) loadWizard(); else { renderRegions(); loadWizard(); }
  }
  const rg = e.target.closest('[data-rg]');
  if(rg){
    region = rg.dataset.rg;
    // "每个国家"是批量，默认每国 1 个，免得一点就开出几十条

    renderRegions();
  }
});

let sourceRows=[];
function renderSourcePriority(catalog){
  sourceRows=(catalog || []).filter(v=>!v.error && (v.count>0 || !v.fetched)).sort((a,b)=>(a.priority || 3)-(b.priority || 3));
  $('#sourceStatus').innerHTML=sourceRows.map((v,i)=>'<div><span title="'+esc(v.url)+'">'+esc(v.name || v.url)+'</span><b>'+(v.count>0 ? v.count+' 个' : '待刷新')+'</b><button data-source-move="'+i+'" data-direction="-1" '+(i===0 ? 'disabled' : '')+' aria-label="上移来源">↑</button><button data-source-move="'+i+'" data-direction="1" '+(i===sourceRows.length-1 ? 'disabled' : '')+' aria-label="下移来源">↓</button></div>').join('');
}
async function confirmSource(input,button,hint){
  const url=$(input).value.trim();if(!url){$(hint).textContent='请先填写链接';return;}
  $(button).disabled=true;$(hint).textContent='正在读取并检查来源…';
  try{
    const result=await api('/api/sources/add',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({url})});
    $('#setSources').value=(result.vpngate_sources || []).join('\n');
    if(curSettings) curSettings.vpngate_sources=result.vpngate_sources;
    renderSourcePriority(result.source_catalog || []);
    $(input).value='';$(hint).textContent='已加入 '+result.count+' 个候选节点';
    regionsLoaded=false;await loadWizard();poll();
  }catch(err){$(hint).textContent=err.message;}
  $(button).disabled=false;
}
$('#sourceQuickAdd').onclick=()=>confirmSource('#sourceQuickURL','#sourceQuickAdd','#sourceQuickHint');
$('#sourceAddConfirm').onclick=()=>confirmSource('#sourceAddURL','#sourceAddConfirm','#sourceAddHint');
document.addEventListener('click',async e=>{
  const move=e.target.closest('[data-source-move]');
  if(move){
    const index=Number(move.dataset.sourceMove),next=index+Number(move.dataset.direction);
    if(next<0 || next>=sourceRows.length) return;
    const reordered=sourceRows.slice();[reordered[index],reordered[next]]=[reordered[next],reordered[index]];
    move.disabled=true;
    try{const r=await api('/api/sources/order',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({order:reordered.map(v=>v.url)})});renderSourcePriority(r.source_catalog);toast('优先顺序已保存');}catch(err){move.disabled=false;toast(err.message,true);}
  }
  const repair=e.target.closest('[data-repair]');
  if(repair){repair.disabled=true;try{await api('/api/entry/repair?slot='+repair.dataset.repair,{method:'POST'});poll();}catch(err){toast(err.message,true);}repair.disabled=false;}
  const cp=e.target.closest('[data-import-copy]');if(cp){const node=(view.imported || []).find(n=>n.id===cp.dataset.importCopy);if(node) copy(node.link);}
  const rm=e.target.closest('[data-import-remove]');if(rm){rm.disabled=true;try{await api('/api/nodes/import',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({remove:rm.dataset.importRemove})});poll();}catch(err){toast(err.message,true);rm.disabled=false;}}
});
$('#nimportConfirm').onclick=async e=>{
  e.target.disabled=true;$('#nimportHint').textContent='';
  try{const r=await api('/api/nodes/import',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({links:$('#nimport').value})});$('#nimport').value='';$('#nimportHint').textContent=r.added ? '已识别 '+r.added+' 个本机节点，可在下方用作入口模板' : '这些本机节点已识别';const template=(r.links || []).find(n=>n.inbound_id);if(template) preferredTemplateID=template.inbound_id;poll();}catch(err){$('#nimportHint').textContent=err.message;}
  e.target.disabled=false;
};

// ---- 新建节点 ----
document.addEventListener('click', e => {
  if(e.target.closest('#newnode') || e.target.closest('#newnode2')){
    $('#nnhint').textContent = '';
    syncNodeForm();
    openModal('newnodebox');
    loadNodeCertificates();
  }
});

// 表单随协议/传输/安全层联动：只露出当前组合真正用得到的字段
function syncNodeForm(){
  const proto = $('#nproto').value;
  let net     = $('#nnet').value;
  const sec   = $('#nsec').value;

  // REALITY 靠模仿 TLS 握手工作，套在自带头部的传输上没有意义
  for(const option of $('#nnet').options){ option.disabled = (view.backend === 'sing-box' || view.backend === 's-ui') && option.value === 'xhttp'; }
  if($('#nnet').selectedOptions[0].disabled) { $('#nnet').value = 'tcp'; net = 'tcp'; }
  const realityOK = net === 'tcp' || net === 'xhttp' || net === 'grpc';
  const secSel = $('#nsec');
  for(const o of secSel.options){
    if(o.value === 'reality') o.disabled = !realityOK;
  }
  if(secSel.value === 'reality' && !realityOK) secSel.value = 'none';

  const cur = secSel.value;
  $('#nsniwrap').hidden  = cur !== 'tls';
  $('#ncertificatewrap').hidden = cur !== 'tls';
$('#ncertwrap').hidden = cur !== 'tls' || $('#ncertificate').value !== 'manual';
  $('#nkeywrap').hidden = cur !== 'tls' || $('#ncertificate').value !== 'manual';
  $('#ndestwrap').hidden = cur !== 'reality';

  // Vision 只在 VLESS + 裸 TCP + TLS/REALITY 下有效
  const visionOK = proto === 'vless' && net === 'tcp' && cur !== 'none';
  $('#nvisionwrap').hidden = !visionOK;
  if(!visionOK) $('#nvision').checked = false;

  const needPath = net === 'ws' || net === 'httpupgrade' || net === 'xhttp' || net === 'grpc';
  $('#npathwrap').hidden = !needPath;
  $('#npathlabel').textContent = net === 'grpc' ? '服务名' : '路径';

  $('#nsechint').textContent =
    cur === 'reality' ? '密钥与 shortId 自动生成' :
    cur === 'tls' ? '优先使用已绑定域名与证书；自签证书链接带指纹。' : '';
}
$('#nproto').onchange = syncNodeForm;
$('#nnet').onchange = syncNodeForm;
$('#nsec').onchange = () => { syncNodeForm(); if($('#nsec').value === 'tls') loadNodeCertificates(); };
$('#ncertificate').onchange = syncCertificateSelection;

$('#ncreate').onclick = async e => {
  const q = new URLSearchParams({
    protocol: $('#nproto').value,
    network:  $('#nnet').value,
    security: $('#nsec').value,
    port:     ($('#nport').value || '').trim(),
    remark:   ($('#nremark').value || '').trim(),
    path:     ($('#npath').value || '').trim(),
    sni:      ($('#nsni').value || '').trim(),
    cert:     ($('#ncert').value || '').trim(),
    key:      ($('#nkey').value || '').trim(),
    dest:     ($('#ndest').value || '').trim(),
  });
  if($('#nvision').checked) q.set('vision', '1');
  const selection = ($('#ncertificate').value || 'auto').split('|');
  if($('#nsec').value === 'tls'){
    q.set('certificate_mode', selection[0] === 'manual' || selection[0] === 'self_signed' ? selection[0] : 'auto');
    if(selection.length > 1) q.set('certificate_id', selection[0]);
    if(selection[0] !== 'manual'){ q.delete('cert'); q.delete('key'); }
  }
  e.target.disabled = true;
  try{
    const r = await api('/api/panel/inbound/new?' + q.toString(), {method:'POST'});
    toast('已创建 ' + r.protocol + ' 节点，端口 ' + r.port);
    closeModal('newnodebox');
    $('#nport').value = '';
    $('#nremark').value = '';
    poll();
  }catch(err){ toast(err.message, true); }
  e.target.disabled = false;
};

$('#rgfilter').oninput = renderRegions;
$('#minus').onclick = () => { step(-1); };
$('#plus').onclick = () => { step(1); };
function step(d){
  const el = $('#count');
  el.value = Math.max(1, (Number(el.value) || 1) + d);
  updateAvail();
}
$('#count').oninput = updateAvail;

$('#go').onclick = async e => {
  const want = Math.min(Number($('#count').value) || 1, availOf(region) || 1);
  const tpl = $('#tpl').value || '0';
  e.target.disabled = true;
  try{
    await api('/api/provision?count=' + want
      + (region === '*' ? '&every=1' : '&region=' + encodeURIComponent(region))
      + '&template=' + tpl, {method:'POST'});
    closeModal('wizard');
    poll();
  }catch(err){ toast(err.message, true); }
  e.target.disabled = false;
};

// ---- 出口操作 ----
document.addEventListener('click', async e => {
  const stop = e.target.closest('[data-stop]');
  if(stop){
    stop.disabled = true;
    try{ await api('/api/stop?slot=' + stop.dataset.stop, {method:'POST'}); }
    catch(err){ toast(err.message, true); }
    poll();
    return;
  }
  const swap = e.target.closest('[data-swap]');
  if(swap){
    swap.disabled = true;
    try{
      await api('/api/swap?slot=' + swap.dataset.swap, {method:'POST'});
      toast('正在换节点');
    }catch(err){ toast(err.message, true); }
    poll();
    return;
  }
  const cred = e.target.closest('[data-cred]');
  if(cred){ openCred(Number(cred.dataset.cred)); return; }
  const job = e.target.closest('[data-job]');
  if(job){
    try{ await api('/api/jobs/dismiss?id=' + job.dataset.job, {method:'POST'}); }catch(err){}
    poll();
    return;
  }
  const provisionStop = e.target.closest('[data-provision-stop]');
  if(provisionStop){
    provisionStop.disabled = true;
    try{ await api('/api/provision/stop?id=' + encodeURIComponent(provisionStop.dataset.provisionStop), {method:'POST'}); toast('正在停止补齐，已保留连通节点'); }
    catch(err){ toast(err.message, true); }
    poll();
    return;
  }
  const del = e.target.closest('[data-delorphans]');
  if(del){
    const list = view.direct || [];
    if(!confirm('删除这 ' + list.length + ' 个未绑定节点？此操作不可撤销。')) return;
    del.disabled = true;
    try{
      await api('/api/xui/delete?ids=' + list.map(i => i.id).join(','), {method:'POST'});
      toast('已清理 ' + list.length + ' 个入站');
    }catch(err){ toast(err.message, true); }
    poll();
    return;
  }

  const one = e.target.closest('[data-delone]');
  if(one){
    if(!confirm('删除入站 ' + one.dataset.name + '？此操作不可撤销。')) return;
    one.disabled = true;
    try{
      await api('/api/xui/delete?ids=' + one.dataset.delone, {method:'POST'});
      toast('已删除 ' + one.dataset.name);
    }catch(err){ toast(err.message, true); }
    poll();
  }
});

$('#stopall').onclick = async e => {
  if(!confirm('停止全部 ' + view.exits.length + ' 个出口，并停止后台补齐？')) return;
  e.target.disabled = true;
  try{ await api('/api/stop-all', {method:'POST'}); }
  catch(err){ toast(err.message, true); }
  poll();
};

// ---- 节点详情 ----
let curDetail = null;

// 详情弹窗的重绘要跟轮询解耦：正在编辑时被 poll 刷掉输入会很烦
async function openDetail(id){
  $('#dbody').innerHTML = '<div class="empty">读取中…</div>';
  curDetail = null;
  $('#ddel').disabled = true;
  openModal('detail');
  try{
    const d = await api('/api/xui/detail?id=' + id);
    curDetail = d;
    renderDetail(d);
    $('#ddel').hidden = isXCL();
    $('#ddel').disabled = isXCL();
  }catch(err){
    $('#dbody').innerHTML = '<div class="empty">读取失败: ' + esc(err.message) + '</div>';
  }
}

// 出口下拉：列出所有已连通的隧道，外加"直连"。绑定按 Xray 的 inboundTag 走。
function exitOptions(currentHost){
  const up = orderedExits().filter(e => e.status === 'up');
  return '<option value=""' + (currentHost ? '' : ' selected') + '>直连（不走隧道）</option>'
    + up.map(e => '<option value="' + esc(e.host) + '"'
        + (e.host === currentHost ? ' selected' : '') + '>'
        + esc((e.exit_ip || e.host) + ' · ' + (e.country || e.region)) + '</option>').join('');
}

function renderDetail(d){
  const owner = view.exits.find(x => (x.inbounds || []).some(i => i.id === d.id));

  const clients = (d.clients || []).map((c, i) => {
    const link = (d.links || [])[i] || '';
    return '<div class="client">'
      + '<div class="crow">'
      +   '<span class="cemail">' + esc(c.email) + '</span>'
      +   '<span class="cid">' + esc(c.id) + '</span>'
      +   '<span class="spacer"></span>'
      +   (link ? '<button class="icon" data-copy="' + esc(link) + '" title="复制链接">' + ICON.copy + '</button>' : '')
      +   '<button class="icon" data-creset="' + esc(c.email) + '" title="换一套凭据，旧链接立即失效">' + ICON.redo + '</button>'
      +   '<button class="icon" data-cdel="' + esc(c.email) + '" title="删除这个客户端">' + ICON.trash + '</button>'
      + '</div>'
      + (isNative() ? '<div class="orow"><span>此客户端落地</span>' + exitPicker((view.exits.find(e => e.host.replace(/[^A-Za-z0-9_-]/g,'-') === c.bound_to) || {}).host || '', 'class="client-landing" data-email="'+esc(c.email)+'"', '跟随入口默认落地') + '</div>' : '')
      + (link ? '<div class="share">' + esc(link) + '</div>' : '')
      + '</div>';
  }).join('');

  $('#dtitle').textContent = (d.remark || '节点') + '　:' + d.port;
  // xray-cf-lite 的节点归它自己管，这里只留出口选择，改端口/备注/客户端都不给
  const editable = !isXCL();
  $('#dbody').innerHTML = '<dl class="kv">'
    + '<dt>出口</dt><dd>'+exitPicker(owner ? owner.host : '', 'id="dbind" data-tag="'+esc(d.tag)+'"','直连（不走隧道）')+'</dd>'
    + '<dt>协议</dt><dd>' + esc(d.protocol) + '　' + esc(d.network || '')
    +   (d.tls && d.tls !== 'none' ? '　' + esc(d.tls) : '') + '</dd>'
    + '<dt>监听</dt><dd>' + esc(d.listen || '0.0.0.0') + '</dd>'
    + '</dl>'
    + (editable ? ('<div class="editbar">'
    +   '<label class="ef"><span>备注</span>'
    +     '<input id="dremark" type="text" value="' + esc(d.remark || '') + '"></label>'
    +   '<label class="ef"><span>端口</span>'
    +     '<input id="dport" type="text" inputmode="numeric" value="' + d.port + '"></label>'
    +   '<label class="chk"><input type="checkbox" id="denable"'
    +     (d.enable === false ? '' : ' checked') + '> 启用</label>'
    +   '<span class="spacer"></span>'
    +   '<button class="primary" id="dsave">保存</button>'
    + '</div>'
    + '<div class="chead"><h3>客户端</h3><span class="count">'
    +   (d.clients || []).length + ' 个</span><span class="spacer"></span>'
    +   '<button id="dcadd">' + ICON.plus + '添加</button></div>'
    + (clients || '<div class="empty">没有客户端</div>'))
    : '<div class="hint">这个节点由 xray-cf-lite 管，端口、UUID 和分享链接都去它那边改。这里只决定它走哪条出口。</div>');
}

document.addEventListener('change', async event => {
  const select = event.target.closest('.client-landing'); if(!select || !curDetail) return;
  select.disabled = true;
  try { await api('/api/panel/client/bind?id=' + curDetail.id + '&email=' + encodeURIComponent(select.dataset.email) + '&host=' + encodeURIComponent(select.value), {method:'POST'}); toast('此凭据已绑定落地，共用原入口端口'); await openDetail(curDetail.id); } catch(error) { restoreExitPicker(select);toast(error.message,true); } finally { select.disabled=false; }
});

// 未绑定区的出口下拉，选中即绑
document.addEventListener('change', async e => {
  const sel = e.target.closest('.obind');
  if(!sel || !sel.value) return;
  sel.disabled = true;
  try{
    await api('/api/xui/bind?tag=' + encodeURIComponent(sel.dataset.tag)
      + '&host=' + encodeURIComponent(sel.value), {method:'POST'});
    toast('已绑定');
    poll();
  }catch(err){ toast(err.message, true); sel.disabled = false; }
});

// 出口下拉改动即生效。绑定按 inboundTag 走，host 传空表示解绑回直连。
document.addEventListener('change', async e => {
  const sel = e.target.closest('#dbind');
  if(!sel) return;
  sel.disabled = true;
  try{
    await api('/api/xui/bind?tag=' + encodeURIComponent(sel.dataset.tag)
      + '&host=' + encodeURIComponent(sel.value), {method:'POST'});
    toast(sel.value ? '已绑定' : '已解绑');
    poll();
  }catch(err){ restoreExitPicker(sel);toast(err.message, true); }
  sel.disabled = false;
});

document.addEventListener('click', async e => {
  const link = e.target.closest('[data-detail]');
  if(link) return openDetail(link.dataset.detail);

  if(e.target.closest('#dsave')){
    const btn = e.target.closest('#dsave');
    btn.disabled = true;
    const q = new URLSearchParams({
      id: curDetail.id,
      port: ($('#dport').value || '').trim(),
      remark: ($('#dremark').value || '').trim(),
      enable: $('#denable').checked ? '1' : '0',
    });
    try{
      await api('/api/panel/inbound/update?' + q, {method:'POST'});
      toast('已保存');
      await openDetail(curDetail.id);
      poll();
    }catch(err){ toast(err.message, true); btn.disabled = false; }
    return;
  }

  const add = e.target.closest('#dcadd');
  if(add){
    add.disabled = true;
    try{
      await api('/api/panel/client/add?id=' + curDetail.id, {method:'POST'});
      toast('已添加客户端');
      await openDetail(curDetail.id);
    }catch(err){ toast(err.message, true); add.disabled = false; }
    return;
  }

  const del = e.target.closest('[data-cdel]');
  if(del){
    if(!confirm('删除客户端 ' + del.dataset.cdel + '？它的链接会立即失效。')) return;
    del.disabled = true;
    try{
      await api('/api/panel/client/del?id=' + curDetail.id
        + '&email=' + encodeURIComponent(del.dataset.cdel), {method:'POST'});
      toast('已删除');
      await openDetail(curDetail.id);
    }catch(err){ toast(err.message, true); del.disabled = false; }
    return;
  }

  const reset = e.target.closest('[data-creset]');
  if(reset){
    if(!confirm('重置 ' + reset.dataset.creset + ' 的凭据？已分发的旧链接会立即失效。')) return;
    reset.disabled = true;
    try{
      await api('/api/panel/client/reset?id=' + curDetail.id
        + '&email=' + encodeURIComponent(reset.dataset.creset), {method:'POST'});
      toast('已重置');
      await openDetail(curDetail.id);
    }catch(err){ toast(err.message, true); reset.disabled = false; }
    return;
  }

  // 详情弹窗里删掉当前这个入站
  const dd = e.target.closest('#ddel');
  if(dd && curDetail){
    const name = (curDetail.remark || curDetail.protocol || '节点') + ' :' + curDetail.port;
    if(!confirm('删除入站 ' + name + '？它的所有客户端链接都会失效，且不可撤销。')) return;
    dd.disabled = true;
    try{
      await api('/api/xui/delete?ids=' + curDetail.id, {method:'POST'});
      toast('已删除 ' + name);
      curDetail = null;
      closeModal('detail');
      poll();
    }catch(err){ toast(err.message, true); dd.disabled = false; }
  }
});

document.addEventListener('click', e => {
  const c = e.target.closest('[data-copy]');
  if(c) copy(c.dataset.copy);
});

// ---- SOCKS5 凭据 ----
let curCred = null;

function socksURL(host, port, user, pass){
  if(host.includes(':') && !host.startsWith('[')) host = '[' + host + ']';
  if(!user) return 'socks5://' + host + ':' + port;
  return 'socks5://' + encodeURIComponent(user) + ':' + encodeURIComponent(pass) + '@' + host + ':' + port;
}

// SOCKS5 端口监听在母机（跑 fanout 的这台服务器）上，客户端要连的是母机的
// 公网 IPv4，流量再从出口 IP 出去。出口 IP 是"出去以后"的地址，不能当连接地址。
// public_ip 是后端探测到的母机公网地址；探测不到才退回访问面板用的主机名。
function credHost(e){
  return view.public_ip || location.hostname || e.host;
}

function openCred(slot){
  const e = view.exits.find(x => x.slot === slot);
  if(!e){ toast('这个出口不在了', true); return; }
  curCred = {slot: slot, port: e.port, host: credHost(e)};
  $('#crtitle').textContent = (e.country || e.region) + ' · :' + e.port;
  $('#cruser').value = e.socks_user || '';
  $('#crpass').value = e.socks_pass || '';
  refreshCredURL();
  openModal('credbox');
}

function refreshCredURL(){
  if(!curCred) return;
  $('#crurl').textContent = socksURL(curCred.host, curCred.port,
    $('#cruser').value.trim(), $('#crpass').value.trim());
  $('#crhttpurl').textContent = $('#crurl').textContent.replace(/^socks5:/,'http:');
}
$('#cruser').oninput = refreshCredURL;
$('#crpass').oninput = refreshCredURL;

$('#crrand').onclick = () => {
  // 客户端和服务端都要能识别，只用无歧义、无需转义的字符
  const abc = 'abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789';
  const gen = n => Array.from(crypto.getRandomValues(new Uint8Array(n)))
    .map(v => abc[v % abc.length]).join('');
  $('#cruser').value = 'fo' + gen(6);
  $('#crpass').value = gen(14);
  refreshCredURL();
};

$('#crcopy').onclick = () => { if(curCred) openProxyGuide(curCred.slot,'socks5'); };
$('#crhttpcopy').onclick = () => { if(curCred) openProxyGuide(curCred.slot,'http'); };

$('#crsave').onclick = async e => {
  if(!curCred) return;
  const btn = e.target; btn.disabled = true;
  const q = new URLSearchParams({
    slot: curCred.slot,
    user: $('#cruser').value.trim(),
    pass: $('#crpass').value.trim(),
  });
  try{
    const r = await api('/api/cred?' + q, {method:'POST'});
    $('#cruser').value = r.user;
    $('#crpass').value = r.pass;
    refreshCredURL();
    toast('已保存，立即生效');
    poll();
  }catch(err){ toast(err.message, true); }
  btn.disabled = false;
};

// ---- 导出 ----
$('#exportAll').onclick = async () => {
  $('#exbox').value = '读取中…';
  $('#excount').textContent = '';
  openModal('export');
  try{
    const d = await api('/api/sub/links');
    $('#exbox').value = (d.links || []).join('\n');
    $('#excount').textContent = (d.links || []).length + ' 条';
  }catch(err){ $('#exbox').value = '导出失败: ' + err.message; }
};
$('#copyall').onclick = () => { const v = $('#exbox').value; if(v) copy(v); };

// ---- 订阅 ----
// 地址用 location.origin 拼：后端给的是访问路径下的相对部分，
// 这样反代、改端口、换路径之后拿到的都是用户此刻真正能访问的地址。
let currentSubPath = '';
function renderSubURL(){
  if(!currentSubPath) return;
  const address=new URL(currentSubPath,location.origin);
  const format=$('#subformat').value || 'base64';
  if(format !== 'base64') address.searchParams.set('target',format);
  $('#suburl').value=address.href;
}
$('#subformat').onchange=renderSubURL;
async function showSub(d){
  currentSubPath=d.path;
  renderSubURL();
  const n = view ? new Set(view.exits.flatMap(x => (x.inbounds || [])).map(n=>n.id)).size + (view.imported || []).length : 0;
  $('#subcount').textContent = n ? n + ' 个节点' : '还没有节点，先开出口再建节点链接';
}
$('#subBtn').onclick = async () => {
  $('#suburl').value = '读取中…';
  $('#subcount').textContent = '';
  openModal('subbox');
  try{ showSub(await api('/api/sub')); }
  catch(err){ $('#suburl').value = '读取失败: ' + err.message; }
};
$('#subcopy').onclick = () => { const v = $('#suburl').value; if(v) copy(v); };
$('#subreset').onclick = async e => {
  if(!confirm('换一串口令？旧地址立刻失效，已经配过的客户端要重新填一次。')) return;
  e.target.disabled = true;
  try{
    showSub(await api('/api/sub/reset', {method:'POST'}));
    toast('已换新地址');
  }catch(err){ toast(err.message, true); }
  e.target.disabled = false;
};

// ---- 设置：改密码 / 改路径 / 改端口 / 改本地监听 ----
let curSettings = null;
let curBackend = null;

// 后端切换：把本机能用的模式列出来，装了的可选，没装的置灰并说明原因
async function loadBackendModes(){
  const sel = $('#setBackend');
  const hint = $('#setBackendHint');
  try{
    const m = await api('/api/panel/mode');
    curBackend = m.mode || '';
    sel.innerHTML = '<option value="">自动（按本机装了什么挑）</option>'
      + (m.modes || []).map(x =>
          '<option value="' + esc(x.mode) + '"' + (x.available ? '' : ' disabled')
          + '>' + esc(x.label) + (x.available ? '' : '（没装）') + '</option>').join('');
    sel.value = curBackend;
    const bad = (m.modes || []).filter(x => !x.available);
    hint.textContent = m.describe
      ? ('当前：' + m.describe + (bad.length ? '。灰掉的是本机没装的。' : ''))
      : '节点从哪来。装了 3x-ui 或 xray-cf-lite 就能直接接管，都没有就用自建。';
  }catch(err){
    sel.innerHTML = '<option value="">读取失败</option>';
    hint.textContent = err.message;
  }
}

$('#settingsBtn').onclick = async () => {
  $('#setPw').value = '';
  $('#setPath').value = '';
  $('#setPathHint').textContent = '读取中…';
  openModal('settings');
  loadBackendModes();
  try{
    const s = await api('/api/settings');
    curSettings = s;
    $('#setPath').value = (s.base_path || '').replace(/^\//, '');
    $('#setPort').value = s.port || '';
    $('#setListen').value = s.listen_addr || '0.0.0.0';
    $('#setResi').checked = !!s.residential_only;
$('#setSources').value = (s.vpngate_sources || []).join('\n');
$('#setIPKey').value = '';
$('#ipKeyHint').textContent = s.ip_info_key_configured ? '已有API Key；留空保持，只在服务端使用。' : '不填可查询ASN及基础属性；填写Key后读取ASN / IP双类型，用于住宅IP规则判断。';
renderSourcePriority(s.source_catalog || s.source_status || []);
    $('#setPublicHost').value = s.public_host || '';
    $('#setPathHint').textContent = '后端访问路径，只能用字母数字和 - _。修改后需重新同步前端配置。';
    $('#updCur').textContent = s.version || '-';
    $('#updLatest').textContent = '';
    $('#updNotes').hidden = true;
    $('#updApply').hidden = true;
    $('#updCheck').disabled = false;
    $('#updCheck').hidden = true;
    $('#updNotes').hidden = false;
    $('#updNotes').textContent = 'Docker 部署请在服务器运行 bash install-docker.sh update；前端可单独更新，无需在网页替换后端程序。';
  }catch(err){ $('#setPathHint').textContent = '读取失败: ' + err.message; }
};

// 检查更新：问后端 GitHub 最新版，有新版就亮出更新按钮和更新内容
$('#updCheck').onclick = async e => {
  e.target.disabled = true;
  e.target.textContent = '检查中…';
  try{
    const u = await api('/api/update/check');
    $('#updCur').textContent = u.current || '-';
    if(u.has_update){
      $('#updLatest').textContent = '有新版本 ' + u.latest;
      $('#updApplyVer').textContent = u.latest;
      $('#updApply').hidden = false;
      $('#updNotes').textContent = u.notes || '（这个版本没写更新说明）';
      $('#updNotes').hidden = false;
    } else {
      $('#updLatest').textContent = '已是最新';
      $('#updApply').hidden = true;
      $('#updNotes').hidden = true;
    }
  }catch(err){ toast(err.message, true); }
  e.target.disabled = false;
  e.target.textContent = '检查更新';
};

// 一键更新：后端下载替换二进制并重启服务，进程重启期间界面会短暂断连
$('#updApply').onclick = async e => {
  if(!confirm('更新到 ' + $('#updApplyVer').textContent + '？服务会重启，界面会短暂断开。')) return;
  e.target.disabled = true;
  e.target.textContent = '更新中…';
  try{
    const r = await api('/api/update/apply', {method:'POST'});
    if(r.restarting){
      $('#updNotes').textContent = '已下载新版本，服务正在重启，几秒后刷新页面即可。';
      $('#updNotes').hidden = false;
      toast('更新中，服务重启后刷新页面');
      // 给服务重启留点时间再自动刷新
      setTimeout(() => location.reload(), 6000);
    } else {
      toast(r.message || '已是最新版');
      e.target.disabled = false;
      e.target.textContent = '更新到 ' + $('#updApplyVer').textContent;
    }
  }catch(err){
    toast(err.message, true);
    e.target.disabled = false;
    e.target.textContent = '更新到 ' + $('#updApplyVer').textContent;
  }
};

// 端口/监听地址变了要提示用户之后从新地址进；密码/路径可原地生效
function nextURL(port, listen, path){
  const host = (listen && listen !== '0.0.0.0') ? listen : location.hostname;
  return location.protocol + '//' + host + ':' + port + (path ? '/' + path : '') + '/';
}

$('#setSave').onclick = async e => {
  e.target.disabled = true;
  const body = {};
  const pw = $('#setPw').value.trim();
  if(pw) body.password = pw;
  body.base_path = $('#setPath').value.trim();
  const portText = $('#setPort').value.trim();
  if(portText && (!/^\d+$/.test(portText) || Number(portText) < 1024 || Number(portText) > 65535)){
    toast('后端端口需为 1024–65535 的整数', true);
    e.target.disabled = false;
    return;
  }
  const port = Number(portText);
  if(port) body.port = port;
  body.listen_addr = $('#setListen').value;
  body.residential_only = $('#setResi').checked;
body.vpngate_sources = $('#setSources').value.split(/\r?\n/).map(v => v.trim()).filter(Boolean);
if($('#setIPKey').value.trim()) body.ip_info_key = $('#setIPKey').value.trim();
  body.public_host = $('#setPublicHost').value.trim();

  const portChanged = curSettings && ((port && port !== curSettings.port)
    || body.listen_addr !== (curSettings.listen_addr || '0.0.0.0'));

  try{
    // 后端和其它设置分属两个接口，先切后端：切失败就别继续，免得用户以为整单都生效了
    const backend = $('#setBackend').value;
    if(curBackend !== null && backend !== curBackend){
      const r = await api('/api/panel/mode', {
        method:'POST',
        headers:{'Content-Type':'application/json'},
        body: JSON.stringify({mode: backend}),
      });
      curBackend = r.mode || '';
      $('#setBackendHint').textContent = '当前：' + (r.describe || r.kind || '已切换');
    }
    await api('/api/settings', {
      method:'POST',
      headers:{'Content-Type':'application/json'},
      body: JSON.stringify(body),
    });
    const pathChanged = body.base_path !== ((curSettings && curSettings.base_path || '').replace(/^\//, ''));
    const hostChanged = body.public_host !== ((curSettings && curSettings.public_host) || '');
    if(portChanged || pathChanged || hostChanged){
      managementMovedURL = location.href;
      $('#setPortHint').textContent = '后端设置已保存。请在前端服务器运行 python3 -B scripts/frontend.py restart，再运行 python3 -B scripts/frontend.py info 查看实际访问地址。当前页面已暂停自动刷新。';
      toast('后端设置已保存，请同步前端配置');
    } else {
      toast('已保存');
      closeModal('settings');
      poll();
    }
  }catch(err){ toast(err.message, true); }
  e.target.disabled = false;
};


let quickSubmitting=false, quickActive=false, quickHiddenJob='', quickLastJob='', quickResultJob=null, quickResultSeen='', quickResultSequence=0;
const quickResultStorageKey='miku.quickResult.'+location.pathname;
try{quickResultSeen=sessionStorage.getItem(quickResultStorageKey) || '';}catch(e){}
function quickShowcase(){
  showcaseMode=true;
  try{localStorage.setItem('miku.showcase','1');}catch(e){}
  renderExits();
}
function openQuickSetup(){
  $('#quickHint').textContent='';
  openModal('quickbox');
  $('#quickDomain').focus();
}
$('#quickSetupBtn').onclick=openQuickSetup;
async function startQuickSetup(bind){
  if(quickSubmitting) return;
  const domain=bind?$('#quickDomain').value.trim():'';
  if(bind && !domain){$('#quickHint').textContent='填入域名即可开始；也可以选择暂不绑定。';$('#quickDomain').focus();return;}
  quickSubmitting=true;$('#quickBind').disabled=true;$('#quickSkip').disabled=true;
  $('#quickHint').textContent='正在开始，请稍等一下…';
  try{
    const result=await api('/api/quick-setup',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({domain:domain})});
    quickHiddenJob='';quickActive=true;quickShowcase();closeModal('quickbox');
    $('#quickStatus').hidden=false;$('#quickStatus').classList.remove('finished');$('#quickHide').hidden=true;
    $('#quickTitle').textContent='Miku 正在为你点亮下一站';$('#quickDetail').textContent='请稍等一下，正在准备连接…';$('#quickProgress').value=0;
    $('#quickSetupBtn').disabled=true;
    $('#quickStatus').scrollIntoView({block:'nearest',behavior:window.matchMedia('(prefers-reduced-motion: reduce)').matches?'auto':'smooth'});
    await poll();
  }catch(err){$('#quickHint').textContent=err.message;}
  finally{quickSubmitting=false;$('#quickBind').disabled=false;$('#quickSkip').disabled=false;}
}
$('#quickBind').onclick=()=>startQuickSetup(true);
$('#quickSkip').onclick=()=>startQuickSetup(false);
$('#quickDomain').onkeydown=event=>{if(event.key==='Enter'){event.preventDefault();startQuickSetup(!!$('#quickDomain').value.trim());}};
$('#quickStop').onclick=async()=>{
  if(!quickActive || !quickResultJob) return;
  $('#quickStop').disabled=true;
  try{
    await api('/api/quick-setup/stop?id='+encodeURIComponent(quickResultJob.id),{method:'POST'});
    $('#quickDetail').textContent='正在停止后台补齐，已连通节点会继续保留…';
    await poll();
  }catch(err){toast(err.message,true);}
  finally{$('#quickStop').disabled=false;}
};
$('#quickHide').onclick=()=>{quickHiddenJob=quickLastJob;$('#quickStatus').hidden=true;};
function syncQuickSetup(jobs){
  const list=jobs.filter(j=>j.kind==='quick-setup'),job=list.find(j=>j.status==='running') || list[list.length-1];
  quickActive=!!job && job.status==='running';
  $('#quickStop').hidden=!quickActive;
  $('#quickSetupBtn').disabled=quickActive || quickSubmitting;
  $('#quickSetupBtn').textContent=quickActive?'Miku 准备中…':'一键配置';
  if(!job){$('#quickStatus').hidden=true;return;}
  quickResultJob=job;
  const hasResult=job.quick_result && job.quick_result.ready>0;
  $('#quickResultBtn').hidden=!hasResult;
  if(hasResult && !quickActive && quickResultSeen!==job.id && !document.querySelector('.modal.open')){
    openQuickResult(job);
  }
  if(job.id!==quickLastJob){quickLastJob=job.id;if(quickActive) quickShowcase();}
  if(quickHiddenJob===job.id){$('#quickStatus').hidden=true;return;}
  $('#quickStatus').hidden=false;
  $('#quickStatus').classList.toggle('finished',!quickActive);$('#quickHide').hidden=quickActive;
  $('#quickTitle').textContent=quickActive?'Miku 正在为你点亮下一站':job.status==='done'?'准备好了，出发吧！':'已保留本轮可用节点';
  const current=job.steps.find(s=>s.status==='running'),failed=job.steps.find(s=>s.status==='failed');
  const summary=job.steps[job.steps.length-1];
  $('#quickDetail').textContent=quickActive?(current && (current.detail || current.label) || '正在并行连接节点，请稍等一下…'):(summary && summary.detail || (failed ? failed.label+'：'+failed.detail : '配置已完成。'));
  $('#quickProgress').max=job.total || 1;$('#quickProgress').value=job.done || 0;
  $('#quickCount').textContent=quickActive?'后台持续补齐 · '+(hasResult?job.quick_result.ready+' / 11 个节点已就绪，可立即复制订阅；':'')+'最多 33 路候选，先连通先提供，失败自动换候选。':hasResult?'订阅与全部链接已准备好。需要更多可到「出口与节点 → 新建出口」添加。':'请查看任务提示，处理后可再次执行一键配置。';
}
function renderQuickResultSub(){
  const input=$('#quickresultSub');
  if(!input.dataset.path) return;
  const address=new URL(input.dataset.path,location.origin),format=$('#quickresultFormat').value;
  if(format && format!=='base64') address.searchParams.set('target',format);
  input.value=address.href;
}
async function loadQuickResult(){
  const seq=++quickResultSequence;
  $('#quickresultCopySub').disabled=true;$('#quickresultCopyLinks').disabled=true;$('#quickresultRetry').hidden=true;
  $('#quickresultSub').value='';delete $('#quickresultSub').dataset.path;$('#quickresultSub').placeholder='正在读取订阅地址…';
  $('#quickresultLinks').value='';$('#quickresultLinks').placeholder='正在读取全部可用节点…';$('#quickresultLinkCount').textContent='';
  $('#quickresultSubError').textContent='';$('#quickresultLinksError').textContent='';
  const results=await Promise.allSettled([api('/api/sub'),api('/api/sub/links')]);
  if(seq!==quickResultSequence) return;
  const [subscription,links]=results;
  if(subscription.status==='fulfilled' && subscription.value.path){
    $('#quickresultSub').dataset.path=subscription.value.path;renderQuickResultSub();$('#quickresultCopySub').disabled=false;
  }else{
    $('#quickresultSub').placeholder='暂未读取到订阅地址';$('#quickresultSubError').textContent=subscription.reason?subscription.reason.message:'订阅地址暂未就绪，请重试。';$('#quickresultRetry').hidden=false;
  }
  if(links.status==='fulfilled'){
    const values=(links.value.links || []).filter(Boolean);
    $('#quickresultLinks').value=values.join('\n');$('#quickresultLinkCount').textContent=values.length+' 条';$('#quickresultCopyLinks').disabled=!values.length;
    if(!values.length){$('#quickresultLinks').placeholder='暂无可用链接';$('#quickresultLinksError').textContent='入口可能仍在同步，请稍后重新读取。';$('#quickresultRetry').hidden=false;}
  }else{
    $('#quickresultLinks').placeholder='暂未读取到节点链接';$('#quickresultLinksError').textContent=links.reason.message;$('#quickresultRetry').hidden=false;
  }
}
function openQuickResult(job){
  if(!job || !job.quick_result || !job.quick_result.ready) return;
  const result=job.quick_result;
  if(job.status!=='running'){quickResultSeen=job.id;try{sessionStorage.setItem(quickResultStorageKey,job.id);}catch(e){}}
  $('#quickresultHeading').textContent=result.ready>=result.target?'节点已就绪，开始连接吧':'已为你保留 '+result.ready+' 个可用节点';
  $('#quickresultIntro').textContent='本次配置 '+result.ready+' / '+result.target+' 个节点就绪。'+(job.status==='running'?'其余节点继续在后台补齐，可先使用以下订阅。':'下方可复制订阅地址，或批量导入当前全部可用节点。');
  $('#quickresultRegions').innerHTML=(result.regions || []).map(r=>'<span>'+countryHTML(r.code,r.name)+' '+Number(r.ready)+' / '+Number(r.target)+'</span>').join('');
  openModal('quickresult');$('#quickresultFormat').focus();
  loadQuickResult();
}
$('#quickResultBtn').onclick=()=>openQuickResult(quickResultJob);
$('#quickresultFormat').onchange=renderQuickResultSub;
$('#quickresultCopySub').onclick=()=>{if(!$('#quickresultCopySub').disabled) copy($('#quickresultSub').value);};
$('#quickresultCopyLinks').onclick=()=>{if(!$('#quickresultCopyLinks').disabled) copy($('#quickresultLinks').value);};
$('#quickresultRetry').onclick=loadQuickResult;
$('#quickresultAdd').onclick=()=>{closeModal('quickresult');$('#newexit').click();};
let proxyGuideState=null,proxyGuideSequence=0;
async function openProxyGuide(slot,scheme,copyFirst=true){
  const exit=view.exits.find(x=>x.slot===slot);
  if(!exit || exit.status!=='up'){toast('这个出口还在连接，请稍等一下再复制。',true);return;}
  const seq=++proxyGuideSequence,entry=selectedEntrance(exit);
  const proxyURL=socksURL(credHost(exit),exit.port,exit.socks_user,exit.socks_pass).replace(/^socks5:/,scheme+':');
  closeModal('credbox');
  proxyGuideState={slot:slot,scheme:scheme,proxy:proxyURL,links:''};
  $('#proxyguideIntro').textContent=(scheme==='http'?'HTTP':'SOCKS5')+' 代理信息已准备好，对应下方同一个出口。';
  $('#proxyguideProxy').textContent='代理地址：'+proxyURL;
  $('#proxyguideLinks').value='';$('#proxyguideLinks').placeholder='正在读取对应入口…';
  $('#proxyguideStatus').textContent='正在读取这个出口当前绑定的入口，请稍等一下…';
  $('#proxyguideCopyEntry').disabled=true;$('#proxyguideRetry').hidden=true;
  openModal('proxyguide');$('#proxyguideCopyProxy').focus();
  if(copyFirst) await copy(proxyURL);
  try{
    const result=await api('/api/exits/links?slot='+slot+(entry?'&id='+entry.id:''));
    if(seq!==proxyGuideSequence) return;
    const links=(result.links || []).filter(Boolean);
    if(!links.length) throw new Error('这个出口暂时没有可用入口，请先创建或修复入口。');
    proxyGuideState.links=links.join('\n');$('#proxyguideLinks').value=proxyGuideState.links;
    $('#proxyguideCopyEntry').disabled=false;
    $('#proxyguideStatus').textContent=(entry?entranceLabel(entry)+' · 端口 '+entry.port+'。':'')+'连接成功后，继续第 2 步。';
  }catch(err){
    if(seq!==proxyGuideSequence) return;
    $('#proxyguideLinks').placeholder='入口暂未就绪';$('#proxyguideStatus').textContent=err.message+' 已有代理信息会为你保留，可以稍后再试。';$('#proxyguideRetry').hidden=false;
  }
}
$('#proxyguideCopyProxy').onclick=()=>{if(proxyGuideState) copy(proxyGuideState.proxy);};
$('#proxyguideCopyEntry').onclick=()=>{if(proxyGuideState && proxyGuideState.links) copy(proxyGuideState.links);};
$('#proxyguideRetry').onclick=()=>{if(proxyGuideState) openProxyGuide(proxyGuideState.slot,proxyGuideState.scheme,false);};
document.addEventListener('keydown',event=>{
  if(event.key!=='Tab') return;
  const modal=['proxyguide','quickbox','quickresult','ipbox'].map(id=>$('#'+id)).find(el=>el.classList.contains('open'));
  if(!modal) return;
  const focusable=Array.from(modal.querySelectorAll('button,input,select,textarea,a[href]')).filter(el=>!el.disabled && !el.hidden && el.getClientRects().length);
  const first=focusable[0],last=focusable[focusable.length-1];
  if(event.shiftKey && document.activeElement===first){event.preventDefault();last.focus();}
  else if(!event.shiftKey && document.activeElement===last){event.preventDefault();first.focus();}
});


let domainData = {certificates:[], registered:[]}, domainPending = null, managementMovedURL='';
function managementMoved(url){
  let address;try{address=new URL(url,location.href);}catch(e){return;}
  if(!['http:','https:'].includes(address.protocol)) return;
  managementMovedURL=address.href;
  domainPending=null;
  const box=$('#managementMoved');box.hidden=false;
  box.innerHTML=domainLink(address.href,'后端管理地址已更新')+'<p class="hint">请在前端服务器运行 python3 -B scripts/frontend.py restart 同步后端连接，再运行 info 查看前端地址。当前页面已暂停自动刷新。</p>';
  showDomainResult(address.href,'新的管理登录地址','请打开新地址后继续操作。');
  $('#domainHint').textContent='管理地址已更新，请打开新地址登录。';
  $('#domainSubmit').disabled=true;$('#domainUnbind').disabled=true;
}
function managementChanged(url){
  try{if(new URL(url,location.href).origin!==location.origin) managementMoved(url);}catch(e){}
}
function domainLink(url,label){
  if(!/^https?:\/\//.test(url || '')) return '';
  return '<div class="domain-login-link"><strong>'+esc(label)+'</strong><a target="_blank" rel="noopener noreferrer" href="'+esc(url)+'">'+esc(url)+'</a><button type="button" data-copy-domain="'+esc(url)+'">复制登录地址</button></div>';
}
function showDomainResult(url,label,note){
  const box=$('#domainResult');box.hidden=false;box.innerHTML=domainLink(url,label)+'<p class="hint">'+esc(note || '')+'</p>';
}
function syncDomainJob(jobs){
  if(!domainPending) return;
  const job=jobs.find(j=>j.id===domainPending.id);
  if(!job) return;
  if(job.result_url){showDomainResult(job.result_url,'新的管理登录地址','绑定已完成，请打开新地址登录。');managementChanged(job.result_url);domainPending=null;}
  else if(job.status==='failed'){const fail=job.steps.find(s=>s.status==='failed');$('#domainResult').hidden=true;$('#domainHint').textContent='绑定未完成：'+(fail ? fail.detail : '请检查签发任务');domainPending=null;}
}
document.addEventListener('click',async event=>{const button=event.target.closest('[data-copy-domain]');if(button){try{await copy(button.dataset.copyDomain);}catch(err){toast(err.message,true);}}});
function certificateOptions(list, includeSpecial){
  let result = includeSpecial ? '<option value="auto">自动选择当前管理域名的证书</option>' : '<option value="">自动复用证书 / 申请 HTTPS 证书</option><option value="__http__">仅绑定 HTTP 管理域名（不签发证书）</option>';
  (list || []).forEach(c => (c.domains || []).forEach(name => {
    result += '<option value="' + esc(c.id + '|' + name) + '">' + esc(name + ' · ' + c.source + (c.self_signed ? ' · 自签' : '')) + '</option>';
  }));
  if(includeSpecial) result += '<option value="self_signed">自签证书（链接携带证书指纹）</option><option value="manual">手动填写证书文件</option>';
  return result;
}
async function loadNodeCertificates(){
  try{
    domainData = await api('/api/domains');
    const select = $('#ncertificate'), previous = select.value;
    select.innerHTML = certificateOptions(domainData.certificates, true);
    if(Array.from(select.options).some(o => o.value === previous)) select.value = previous;
    if(select.value === 'auto'){
      const candidates = (domainData.certificates || []).flatMap(c => (c.domains || []).map(name => ({id:c.id,name:name})));
      const preferred = candidates.find(c => c.name === domainData.current && !c.name.includes('*')) || candidates.find(c => !c.name.includes('*'));
      if(preferred) select.value = preferred.id + '|' + preferred.name;
      else if(!candidates.length) select.value = 'self_signed';
    }
    $('#ncertificatehint').textContent = domainData.certificates.length ? '已读取服务器证书，可为这个入口选择自己的域名。' : '未发现有效证书。可先绑定域名，也可使用自签证书。';
    syncCertificateSelection();
  }catch(err){ $('#ncertificatehint').textContent = '证书读取失败：' + err.message; }
}
function syncCertificateSelection(){
  const selection = $('#ncertificate').value.split('|');
  if(selection.length > 1) $('#nsni').value = selection[1].includes('*') ? '' : selection[1];
  if(selection[0] === 'self_signed' && !$('#nsni').value) $('#nsni').value = domainData.current || location.hostname;
  syncNodeForm();
}
async function openDomains(refresh=false){
  openModal('domainbox');$('#domainHint').textContent = '读取域名与证书…';
  try{
    domainData = await api('/api/domains'+(refresh===true?'?refresh=1':''));
    $('#domainCurrent').innerHTML = '<strong>后端管理地址</strong><div>' + esc(domainData.management_url || location.href) + '</div>';
    const previous = $('#domainCert').value;
    $('#domainCert').innerHTML = certificateOptions(domainData.certificates,false);
    if(Array.from($('#domainCert').options).some(o => o.value === previous)) $('#domainCert').value = previous;
    $('#domainList').innerHTML = (domainData.registered || []).map(d => {
      const c = (domainData.certificates || []).find(c => (c.domains || []).includes(d.domain));
      return '<div class="domain-card"><strong>' + esc(d.domain) + '</strong><small>' + (c ? '有效期至 ' + new Date(c.expires).toLocaleDateString() + (c.self_signed ? ' · 自签证书' : '') : '证书需检查') + '</small><div class="domain-actions"><button data-domain-use="' + esc(d.domain) + '" data-certificate="' + esc(c ? c.id : '') + '">设为管理域名</button><button data-domain-select="' + esc(d.domain) + '">用于新建TLS入口</button><button data-domain-remove="' + esc(d.domain) + '">移出列表</button></div></div>';
    }).join('') || '<div class="hint">还没有绑定记录。已有Certbot、acme.sh、管理或入口证书会自动列出。</div>';
    syncDomainMethod();
    $('#domainHint').textContent = '';
  }catch(err){ $('#domainHint').textContent = err.message; }
}
$('#domainBtn').onclick = openDomains;
$('#domainRefresh').onclick = () => openDomains(true);
function syncDomainMethod(){
  const selection=$('#domainCert').value.split('|'),http=selection[0]==='__http__';
  $('#domainManagement').disabled=http;if(http) $('#domainManagement').checked=true;
  if(selection.length>1 && !selection[1].includes('*')) $('#domainName').value=selection[1];
}
$('#domainCert').onchange=syncDomainMethod;
async function domainAction(body){
  const result = await api('/api/domains',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
  if(result.job){domainPending={id:result.job,url:result.next_url};$('#domainHint').textContent='证书签发进行中，可在任务区查看进度。';if(result.next_url) showDomainResult(result.next_url,'签发成功后的登录地址','此地址将在签发成功后启用。签发成功后请打开上方地址登录，当前端口保持不变。');poll();return;}
  if(result.management || body.action === 'unbind'){
    $('#domainHint').textContent='绑定已生效，请使用新地址登录。';showDomainResult(result.url,'新的管理登录地址','已包含实际管理端口和完整登录路径。');$('#domainCurrent').textContent='当前管理地址：'+result.url;
    managementChanged(result.url);
    if(body.action==='unbind') $('#domainHint').textContent='管理域名已解绑，请使用新的 IP 登录地址。';
    toast(body.action==='unbind'?'管理域名已解绑，请打开 IP 登录地址':'管理地址已更新，请打开新地址');
  }else{ toast('域名已保存');await openDomains(); }
  if(!managementMovedURL) loadNodeCertificates();
}
$('#domainSubmit').onclick = async event => {
  const selection = $('#domainCert').value.split('|');
  event.target.disabled=true;
  try{ await domainAction({action:selection[0]==='__http__' ? 'bind-http' : selection[0] ? 'bind' : 'issue',domain:$('#domainName').value.trim(),certificate_id:selection[0]==='__http__' ? '' : selection[0],management:$('#domainManagement').checked}); }
  catch(err){ $('#domainHint').textContent=err.message; }
  finally{event.target.disabled=!!managementMovedURL;}
};
$('#domainUnbind').onclick = async event => {event.target.disabled=true;try{await domainAction({action:'unbind'});}catch(err){$('#domainHint').textContent=err.message;toast(err.message,true);}finally{event.target.disabled=!!managementMovedURL;}};
document.addEventListener('click',async event => {
  const use=event.target.closest('[data-domain-use]'),choose=event.target.closest('[data-domain-select]'),remove=event.target.closest('[data-domain-remove]');
  if(use){try{await domainAction({action:'bind',domain:use.dataset.domainUse,certificate_id:use.dataset.certificate,management:true});}catch(err){toast(err.message,true);}}
  if(choose){closeModal('domainbox');$('#nsec').value='tls';syncNodeForm();openModal('newnodebox');await loadNodeCertificates();const option=Array.from($('#ncertificate').options).find(o=>o.value.endsWith('|'+choose.dataset.domainSelect));if(option){$('#ncertificate').value=option.value;syncCertificateSelection();}}
  if(remove){try{await domainAction({action:'remove',domain:remove.dataset.domainRemove});}catch(err){toast(err.message,true);}}
});
function ipSummary(exit){
  if(!exit.exit_ip) return '';
  const latency=latencyOf(exit);
  return '<div class="ip-summary">'+networkOwner(exit)+typeBadge(exit)+'<span class="summary-latency'+(/^\d/.test(latency.label)?'':' failed')+'" title="'+esc(latency.title)+'">'+(latency.approx?'参考总延迟 ':'总延迟 ')+esc(latency.label)+(latency.method?' <span class="latency-method">'+esc(latency.method)+'</span>':'')+'</span><button data-ip="'+esc(exit.exit_ip)+'">查看IP</button></div>';
}
function exitLocation(exit){
  const info = exit.ip_info;
  if(info && info.location) return info.location;
  if(info && (info.country || info.city)) return [info.country,info.city].filter(Boolean).join(' / ');
  return 'IP位置待查';
}
let ipLookupSequence=0;
function boolLabel(value){return value === true ? '是' : value === false ? '否' : '未提供';}
function knownIPValue(value){return value!==undefined && value!==null && !['','未知','unknown','n/a','-'].includes(String(value).trim().toLowerCase());}
function ipToolbar(ip){
  return '<div class="ip-result-toolbar"><a class="ip-details-link" href="https://ping0.cc/'+(ip?'ip/'+encodeURIComponent(ip):'')+'" target="_blank" rel="noopener noreferrer">在 Ping0 检测'+(ip?'此 IP':'')+' <span aria-hidden="true">↗</span></a>'+(ip?'<button data-copy-ip="'+esc(ip)+'">'+ICON.copy+'复制 IP</button>':'')+'</div>';
}
function renderIPState(ip,message,bad=false){
  $('#ipDetails').innerHTML=ipToolbar(ip)+'<div class="ip-state'+(bad?' bad':'')+'" role="status"><h3>'+esc(bad?'查询暂未完成':'正在查询 '+ip)+'</h3><p>'+esc(message)+'</p></div>';
}
function ipCheckedTime(value){
  if(!value || String(value).startsWith('0001-'))return '';
  const date=new Date(value);
  return Number.isFinite(date.getTime())?date.toLocaleString('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit'}):'';
}
function renderIPInfo(info){
  const ip=String(info.ip || $('#ipAddress').value.trim()),type=['residential','datacenter','mobile'].includes(info.ip_type)?info.ip_type:'unknown';
  const typeLabel=IP_TYPE_LABEL[type] || '类型待确认',owner=info.organization || info.isp || info.company || '归属信息暂未提供';
  const confidence=({high:'高置信',medium:'中置信 · 规则推断',low:'低置信 · 待复核'})[info.type_confidence] || '';
  const facts=[['地理位置',info.location || [info.country,info.city].filter(knownIPValue).join(' / ') || '暂未获得定位信息','wide'],['ASN',info.asn?'AS'+info.asn+(knownIPValue(info.asn_type)?' · '+info.asn_type:''):'暂未提供','asn-value'],['归属运营商',owner,'']];
  if(knownIPValue(info.isp) && info.isp!==owner)facts.push(['运营商 ISP',info.isp,'']);
  if(knownIPValue(info.company) && info.company!==owner && info.company!==info.isp)facts.push(['IP 归属商',info.company+(knownIPValue(info.company_type)?' · '+info.company_type:''),'']);
  if(knownIPValue(info.network))facts.push(['网络范围',info.network,'wide']);
  const flags=[['机房',info.hosting],['移动',info.mobile],['代理',info.proxy],['VPN',info.vpn],['Tor',info.tor]];
  const sourceGroups=[['定位',info.geo_source || info.source],['ASN',info.asn_source || info.source],['属性',info.type_source || info.source]];
  const sourceMap=new Map();sourceGroups.forEach(([name,source])=>{if(knownIPValue(source))sourceMap.set(source,[...(sourceMap.get(source) || []),name]);});
  const sources=Array.from(sourceMap,([source,names])=>names.join(' / ')+'：'+source).join(' · ');
  const times=[['属性',ipCheckedTime(info.checked)],['定位',ipCheckedTime(info.geo_checked)]].filter(([,value])=>value).map(([name,value])=>name+' '+value).join(' · ');
  const attributes=[info.family,knownIPValue(info.attribute) && info.attribute!==typeLabel?info.attribute:''].filter(knownIPValue).join(' · ');
  $('#ipDetails').innerHTML=ipToolbar(ip)
    +'<div class="ip-overview"><div><h3>'+esc(ip)+'</h3>'+(attributes?'<p>'+esc(attributes)+'</p>':'')+'</div><span class="network-type '+type+'">'+esc(typeLabel)+'</span></div>'
    +'<dl class="ip-facts">'+facts.map(([label,value,style])=>'<div class="ip-fact'+(style==='wide'?' wide':'')+'"><dt>'+esc(label)+'</dt><dd'+(style==='asn-value'?' class="asn-value"':'')+'>'+esc(value)+'</dd></div>').join('')+'</dl>'
    +'<div class="ip-signals" aria-label="网络标记">'+flags.map(([label,value])=>'<span class="ip-signal'+(value===true?' flagged':value===false?'':' unspecified')+'">'+esc(label)+'<b>'+boolLabel(value)+'</b></span>').join('')+'</div>'
    +(knownIPValue(info.type_reason)?'<p class="ip-classification"><strong>分类依据'+(confidence?'（'+esc(confidence)+'）':'')+'：</strong>'+esc(info.type_reason)+'</p>':'')
    +'<div class="ip-provenance"><p>'+esc(sources || '数据来源暂未提供')+'</p>'+(times?'<p>更新于 '+esc(times)+'</p>':'')+'<p>“未提供”表示来源缺少该标记，不代表检测为否。</p></div>'
    +(info.error?'<p class="ip-error" role="status">'+esc(info.error)+'</p>':'');
}
async function lookupIP(){
  const sequence=++ipLookupSequence,ip=$('#ipAddress').value.trim();if(!ip)return;
  renderIPState(ip,'正在读取位置与网络属性，可先在 Ping0 中查看。');
  $('#ipDetails').setAttribute('aria-busy','true');
  try{
    for(let i=0;i<25;i++){
      try{const result=await api('/api/ipinfo?ip='+encodeURIComponent(ip));if(sequence!==ipLookupSequence)return;if(!result.pending){renderIPInfo(result);return;}}
      catch(err){if(sequence===ipLookupSequence)renderIPState(ip,err.message,true);return;}
      await new Promise(resolve=>setTimeout(resolve,2000));
      if(sequence!==ipLookupSequence)return;
    }
    renderIPState(ip,'查询仍在排队。可重新查询，或通过上方 Ping0 入口查看。',true);
  }finally{if(sequence===ipLookupSequence)$('#ipDetails').removeAttribute('aria-busy');}
}
$('#ipBtn').onclick=()=>{openModal('ipbox');$('#ipAddress').focus();};
$('#ipSearch').onsubmit=event=>{event.preventDefault();lookupIP();};
document.addEventListener('click',event=>{
  const button=event.target.closest('[data-ip]');
  if(button){$('#ipAddress').value=button.dataset.ip;openModal('ipbox');$('#ipAddress').focus();lookupIP();}
  const copyIP=event.target.closest('[data-copy-ip]');if(copyIP)copy(copyIP.dataset.copyIp);
});

poll();
measureBrowserPing();
setInterval(measureBrowserPing, BROWSER_LATENCY_INTERVAL);
setInterval(poll, 3000);
