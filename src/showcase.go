package main

const showcaseCSS = `
.view-switch{color:var(--dim)}.view-switch[aria-pressed=true]{background:var(--surface);color:var(--accent)}
.country-code{color:var(--dim);font-size:10px;font-weight:500;margin-left:5px}
.showcase-mode #entranceBar,.showcase-mode #orphans,.showcase-mode #importedNodes{display:none}
.showcase-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:18px;align-items:start}.showcase-grid>.empty{grid-column:1/-1}
.network-owner{display:flex;align-items:center;gap:10px;min-width:0;flex-wrap:wrap}.asn-tag{display:inline-flex;align-items:center;flex:none;color:var(--asn);background:var(--asn-bg);border:1px solid var(--line);border-radius:5px;padding:2px 6px;font-size:10px;font-weight:650;font-variant-numeric:tabular-nums;line-height:1.5}.asn-owner{color:var(--owner);font-size:13px;font-weight:550;line-height:1.5;overflow-wrap:anywhere;min-width:0}
.network-type{display:inline-flex;align-items:center;gap:5px;width:fit-content;max-width:100%;border:1px solid var(--line);border-radius:5px;padding:3px 7px;background:var(--surface);color:var(--dim);font-size:10px;font-weight:550;line-height:1.4;white-space:nowrap}.network-type:before{content:'';width:5px;height:5px;flex:none;border-radius:50%;background:currentColor}.network-type.residential{color:var(--residential);background:var(--residential-bg);border-color:#40765e}.network-type.datacenter{color:var(--datacenter);background:var(--datacenter-bg);border-color:#67567c}.network-type.mobile{color:var(--mobile);background:var(--mobile-bg);border-color:#9a7b3c}.network-type.unavailable{color:var(--bad);background:#412433;border-color:#966078}
.node-card{display:flex;flex-direction:column;min-width:0;background:var(--card);border:1px solid var(--line);border-radius:16px;padding:22px;position:relative;overflow:hidden;box-shadow:none}
.node-card.connected{border-color:var(--line)}
.node-card-head{display:flex;align-items:center;gap:10px;margin-bottom:12px;min-width:0}.node-card-country{font-size:16px;font-weight:650;min-width:0;overflow-wrap:anywhere;line-height:1.5}.node-card-state{margin-left:auto;display:inline-flex;align-items:center;gap:6px;font-size:10px;color:var(--dim);white-space:nowrap;padding:3px 0;line-height:1.4}.node-card-state.up{color:var(--residential)}.node-card-state.failed{color:var(--bad)}.node-card-state.starting{color:var(--warn)}.node-card-state .dot{width:5px;height:5px;flex:none}
button.node-card-ip{font-size:clamp(21px,2.1vw,28px);font-weight:650;letter-spacing:-.5px;overflow-wrap:anywhere;white-space:normal;line-height:1.35;margin:0 0 10px;padding:0;border:0;border-radius:5px;background:transparent;color:var(--text);min-height:44px;justify-content:flex-start;text-align:left;font-variant-numeric:tabular-nums;gap:12px;width:fit-content;max-width:100%}button.node-card-ip:hover:not(:disabled){color:var(--accent);background:transparent}button.node-card-ip svg{width:13px;height:13px;color:var(--dim)}
.node-card-meta{display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-bottom:16px}.node-card-id{font-size:10px;color:var(--dim);display:flex;gap:6px;align-items:center;flex-wrap:wrap;margin-left:auto}.node-card-protocol{font-size:9px;line-height:1.5;letter-spacing:.1px;color:var(--dim);overflow-wrap:anywhere}
.node-card-network{padding:13px 0;border-top:1px solid var(--line);margin-bottom:10px;min-height:79px}.node-card-network .network-owner{display:flex;gap:8px;align-items:baseline;flex-wrap:wrap}.node-card-network .asn-tag{justify-self:start}
.node-card-location{display:flex;align-items:flex-start;gap:6px;font-size:11px;color:var(--dim);line-height:1.6;margin-top:8px;overflow-wrap:anywhere}.node-card-location svg{width:12px;height:12px;margin-top:3px;flex:none}
.node-card-facts{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:16px;margin-bottom:18px;padding-top:4px}.node-card-facts>div{min-width:0}.node-card-facts small{display:block;color:var(--dim);font-size:10px;margin-bottom:5px}.node-card-facts strong{font-size:22px;font-weight:600;line-height:1.35;letter-spacing:-.5px;overflow-wrap:anywhere;font-variant-numeric:tabular-nums;color:var(--text)}.node-card-facts .latency-unavailable{font-size:13px;color:var(--warn);letter-spacing:0}.node-card-latency-parts{display:block;color:var(--dim);font-size:9px;margin-top:4px;line-height:1.5;overflow-wrap:anywhere}.latency-method{font-size:9px;color:var(--dim);font-weight:500;margin-left:5px;white-space:nowrap}
.exit-stability-notice{display:flex;align-items:flex-start;gap:10px;padding:12px 14px;border:1px solid #82613b;border-radius:10px;background:#332c23;color:#ffe1a7;font-size:12px;line-height:1.65;overflow-wrap:anywhere}.exit-stability-notice>svg{width:18px;height:18px;flex:none;margin-top:3px;stroke:currentColor;fill:none;stroke-width:1.8}.exit-stability-notice>div{min-width:0}.exit-stability-heading{display:flex;align-items:baseline;flex-wrap:wrap;gap:3px 10px}.exit-stability-heading strong{font-size:12px;font-weight:650}.exit-stability-heading span{font-size:11px;color:#f0c88c}.exit-stability-notice p{margin:5px 0 0;color:#f6dfbd}.exit>.exit-stability-notice{margin:0 18px 16px}.node-card .exit-stability-notice{margin-bottom:14px}
.node-card-entry{display:flex;align-items:center;gap:8px;color:var(--dim);font-size:11px;margin-bottom:14px}.node-card-entry select{min-width:0;flex:1;width:auto;font-size:11px;border-radius:8px;min-height:44px}.node-card-entry>span{flex:none}
.node-card-actions{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px;margin-top:auto}.node-card-actions button{min-width:0;min-height:44px;justify-content:center;padding:8px 6px;border-radius:9px;font-size:12px;font-weight:600;background:var(--surface);color:var(--text);line-height:1.4;white-space:normal;gap:6px}.node-card-actions .copy-node{color:#0c302d;border-color:var(--accent);background:var(--accent);gap:6px}.node-card-actions .copy-node small{font-size:9px;font-weight:400;line-height:1.1;color:#214e48}.node-card-actions .copy-node:hover:not(:disabled){background:#a0e9df}.node-card-actions .switch-ip{color:var(--text);border-color:var(--line);background:var(--surface)}
.node-card-more{margin-top:14px;border-top:1px solid var(--line);padding-top:3px}.node-card-more>summary{display:flex;align-items:center;justify-content:space-between;gap:5px;min-height:40px;color:var(--dim);font-size:11px;list-style:none;cursor:pointer}.node-card-more>summary::-webkit-details-marker{display:none}.node-card-more>summary svg{width:12px;height:12px;transition:transform .2s}.node-card-more[open]>summary svg{transform:rotate(180deg)}
.node-card-proxies{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px;padding:0 0 14px}.node-card-proxies .node-card-proxy-label{text-align:left;font-size:10px;font-weight:500;color:var(--dim);margin:0}.node-card-proxies button{justify-content:center;min-height:44px;font-size:11px;background:var(--surface)}
.node-card-tools{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:7px;padding-top:12px;border-top:1px solid var(--line)}.node-card-tools button{min-width:0;min-height:44px;justify-content:center;font-size:11px;color:var(--text);background:var(--surface);padding:6px;white-space:normal}.node-card-tools .stop-exit{color:var(--bad);background:transparent}.node-card .errline{font-size:11px;margin:10px 0 0;padding:0;white-space:normal;overflow-wrap:anywhere}
@media(max-width:900px){.node-card{padding:18px}.showcase-grid{gap:16px}.node-card-meta{gap:7px}.node-card-id{margin-left:0}}
@media(max-width:620px){.showcase-grid{grid-template-columns:minmax(0,1fr);gap:14px}.node-card{padding:20px;border-radius:15px}.node-card-country{font-size:16px}.node-card-state{font-size:11px}.node-card-meta{margin-bottom:14px}.node-card-id{margin-left:auto}button.node-card-ip{font-size:27px}.asn-owner{font-size:13px}.node-card-facts{margin-bottom:16px}.node-card-facts strong{font-size:23px}.node-card-facts small{font-size:11px}.node-card-latency-parts{font-size:10px}.node-card-actions button{font-size:12px}.node-card-network{min-height:0}}
@media(max-width:360px){.node-card{padding:16px}.node-card-id{margin-left:0}button.node-card-ip{font-size:24px}.node-card-facts strong{font-size:21px}.node-card-entry{display:block}.node-card-entry>span{display:block;margin-bottom:5px}.node-card-entry select{width:100%}}
`

const showcaseJS = `
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
`
