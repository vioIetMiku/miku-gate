package main

const showcaseCSS = `
.view-switch{border-color:#428987;color:var(--accent)}.view-switch[aria-pressed=true]{background:#153c40;color:#8bf4e7}
.country-code{color:var(--dim);font-size:12px;font-weight:600;margin-left:5px}
.showcase-mode #entranceBar,.showcase-mode #orphans,.showcase-mode #importedNodes{display:none}
.showcase-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:18px;align-items:stretch}.showcase-grid>.empty{grid-column:1/-1}
.network-owner{display:flex;align-items:center;gap:10px;min-width:0;flex-wrap:wrap}
.asn-tag{display:inline-flex;align-items:center;flex-shrink:0;color:var(--asn);background:var(--asn-bg);border:1px solid #3a6080;border-radius:7px;padding:3px 8px;font-size:12px;font-weight:750;font-variant-numeric:tabular-nums;line-height:1.5}
.asn-owner{color:var(--owner);font-size:14px;font-weight:650;line-height:1.5;overflow-wrap:anywhere;min-width:0}
.network-type{display:inline-flex;align-items:center;gap:6px;width:fit-content;max-width:100%;border:1px solid #587084;border-radius:8px;padding:4px 9px;background:#233444;color:#d3e1ee;font-size:12px;font-weight:750;line-height:1.4;white-space:nowrap}
.network-type:before{content:'';width:6px;height:6px;flex-shrink:0;border-radius:50%;background:currentColor}
.network-type.residential{color:var(--residential);background:var(--residential-bg);border-color:#408b70}
.network-type.datacenter{color:var(--datacenter);background:var(--datacenter-bg);border-color:#785e9b}
.network-type.mobile{color:var(--mobile);background:var(--mobile-bg);border-color:#9a7b3c}
.network-type.unavailable{color:var(--bad);background:#412433;border-color:#966078}
.node-card{display:flex;flex-direction:column;min-width:0;background:var(--card);border:1px solid var(--line);border-radius:18px;padding:22px;position:relative;overflow:hidden;box-shadow:0 8px 26px #0002}
.node-card.connected{border-color:#3c666f}.node-card.connected:before{content:'';position:absolute;top:0;left:22px;right:22px;height:3px;background:var(--accent);border-radius:0 0 3px 3px}
.node-card-head{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin-bottom:14px;min-width:0}.node-card-country{font-size:18px;font-weight:750;min-width:0;overflow-wrap:anywhere;line-height:1.4}.node-card-state{margin-left:auto;display:inline-flex;align-items:center;gap:6px;font-size:12px;color:var(--dim);white-space:nowrap;padding:4px 8px;border-radius:999px;background:#1c3040;line-height:1.4}.node-card-state.up{color:#94f4d7;background:#173c35}.node-card-state.failed{color:var(--bad);background:#412433}.node-card-state.starting{color:var(--warn);background:#3c321f}.node-card-state .dot{width:6px;height:6px;flex-shrink:0}
.node-card-ip{font-size:clamp(14px,2.5vw,29px);font-weight:750;letter-spacing:.2px;overflow-wrap:anywhere;line-height:1.35;margin:0 0 13px;font-variant-numeric:tabular-nums;color:#f4f9ff}
.node-card-meta{display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-bottom:16px}.node-card-id{font-size:12px;font-weight:650;color:var(--dim);margin-left:auto;display:flex;gap:6px;flex-wrap:wrap}.node-card-protocol{color:#d8e9f5}
.node-card-network{padding:13px 0;border-top:1px solid var(--line);border-bottom:1px solid var(--line);margin-bottom:14px}.node-card-network .network-owner{display:grid;grid-template-columns:auto minmax(0,1fr);gap:9px;align-items:start}.node-card-network .asn-tag{justify-self:start}
.node-card-location{display:flex;align-items:flex-start;gap:6px;font-size:12px;color:#c7d8e6;line-height:1.6;margin-top:9px;overflow-wrap:anywhere}.node-card-location svg{width:14px;height:14px;margin-top:2px;flex:none}
.node-card-facts{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:16px;margin-bottom:16px}.node-card-facts>div{min-width:0}.node-card-facts small{display:block;color:var(--dim);font-size:12px;margin-bottom:4px}.node-card-facts strong{font-size:21px;font-weight:750;line-height:1.4;overflow-wrap:anywhere;font-variant-numeric:tabular-nums;color:#f1f8ff}.node-card-facts .latency-unavailable{font-size:14px;color:var(--warn)}
.node-card-latency-parts{display:block;color:var(--dim);font-size:11px;margin-top:3px;line-height:1.5;overflow-wrap:anywhere}
.latency-method{font-size:10px;color:#b4e4f4;font-weight:650;margin-left:4px;white-space:nowrap}
.exit-stability-notice{display:flex;align-items:flex-start;gap:10px;padding:12px 14px;border:1px solid #82613b;border-radius:10px;background:#332c23;color:#ffe1a7;font-size:12px;line-height:1.65;overflow-wrap:anywhere}.exit-stability-notice>svg{width:18px;height:18px;flex:none;margin-top:3px;stroke:currentColor;fill:none;stroke-width:1.8}.exit-stability-notice>div{min-width:0}.exit-stability-heading{display:flex;align-items:baseline;flex-wrap:wrap;gap:3px 10px}.exit-stability-heading strong{font-size:12px;font-weight:750}.exit-stability-heading span{font-size:11px;color:#f0c88c}.exit-stability-notice p{margin:5px 0 0;color:#f6dfbd}.exit>.exit-stability-notice{margin:0 18px 16px}.node-card .exit-stability-notice{margin-bottom:14px}
.node-card-entry{display:flex;align-items:center;gap:8px;color:var(--dim);font-size:12px;margin-bottom:14px}.node-card-entry select{min-width:0;flex:1;width:auto;font-size:12px;border-radius:8px;min-height:44px}.node-card-entry>span{flex:none}
.node-card-actions{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px;margin-top:auto}.node-card-actions button{min-width:0;min-height:44px;justify-content:center;padding:8px 6px;border-radius:10px;font-size:12px;font-weight:650;background:#203747;color:#eef6ff;line-height:1.4;white-space:normal;gap:6px}.node-card-actions .copy-node{color:#071e23;border-color:var(--accent);background:var(--accent);flex-direction:column;gap:1px}.node-card-actions .copy-node small{font-size:10px;line-height:1.1;color:#173b41}.node-card-actions .copy-node:hover:not(:disabled){background:#90f2e9}.node-card-actions .switch-ip{color:#e4d6ff;border-color:#736297;background:#302c49}
.node-card-more{margin-top:12px;border-top:1px solid var(--line);padding-top:3px}.node-card-more>summary{display:flex;align-items:center;justify-content:space-between;gap:5px;min-height:44px;color:var(--dim);font-size:12px;list-style:none;cursor:pointer}.node-card-more>summary::-webkit-details-marker{display:none}.node-card-more>summary svg{width:12px;height:12px;transition:transform .16s}.node-card-more[open]>summary svg{transform:rotate(180deg)}
.node-card-tools{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:7px}.node-card-tools button{min-width:0;min-height:44px;justify-content:center;font-size:12px;color:#d8e8f4;background:#142d3e;padding:6px 3px;white-space:normal}.node-card-tools .stop-exit{color:var(--bad);border-color:#75485d;background:#332332}.node-card .errline{font-size:12px;margin:12px 0 0;padding:0;white-space:normal;overflow-wrap:anywhere}
@media(max-width:700px){.showcase-grid{gap:12px}.node-card{padding:15px;border-radius:14px}.node-card.connected:before{left:15px;right:15px}.node-card-country{font-size:15px}.node-card-state{font-size:11px;padding:3px 6px;gap:5px}.node-card-head{gap:7px;margin-bottom:11px}.node-card-meta{gap:7px;margin-bottom:12px}.node-card-id{font-size:11px;margin-left:0}.node-card-network .network-owner{grid-template-columns:minmax(0,1fr);gap:6px}.asn-owner{font-size:13px}.node-card-facts{gap:8px}.node-card-facts strong{font-size:18px}.node-card-facts .latency-unavailable{font-size:12px}.node-card-actions button svg{display:none}.node-card-entry{display:block}.node-card-entry>span{display:block;margin-bottom:5px}.node-card-entry select{width:100%}.node-card-location{font-size:11px}.node-card-network{padding:11px 0}.node-card-facts small{font-size:11px}}
@media(max-width:420px){.showcase-grid{gap:10px}.node-card{padding:11px}.node-card.connected:before{left:11px;right:11px}.node-card-country{font-size:13px}.country-code{font-size:10px;margin-left:3px}.node-card-state{font-size:10px}.network-type{font-size:11px;padding:4px 7px;gap:5px}.asn-tag{font-size:11px;padding:3px 6px}.asn-owner{font-size:12px}.node-card-facts strong{font-size:16px}.node-card-actions button{font-size:11px}.node-card-tools button{font-size:11px}.node-card-head{gap:6px}.node-card-ip{font-size:14px}.node-card-latency-parts{font-size:10px}}
`

const showcaseJS = `
let showcaseMode=false;
let preferredTemplateID=0;
const selectedEntrances=new Map();
const expandedCardTools=new Set();
try{showcaseMode=localStorage.getItem('miku.showcase')==='1';}catch(e){}
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
  $('#list').innerHTML=view.exits.map(e=>{
    const entrances=enabledEntrances(e),entry=selectedEntrance(e),up=e.status==='up';
    const latency=latencyOf(e);
    const ready=up && entry,disabled=up?'':' disabled',error=e.entry_error || e.err;
    const selector=entrances.length>1 ? '<label class="node-card-entry"><span>选择入口</span><select data-card-entry="'+e.slot+'" aria-label="选择此出口的入口">'+entrances.map(i=>'<option value="'+i.id+'"'+(entry.id===i.id?' selected':'')+'>'+esc(i.protocol.toUpperCase()+' :'+i.port+(i.remark?' · '+i.remark:''))+'</option>').join('')+'</select></label>' : '';
    return '<article class="node-card'+(up?' connected':'')+'" data-exit-card="'+e.slot+'">'
      +'<div class="node-card-head"><span class="node-card-country">'+esc(countryName(e))+' <span class="country-code">'+esc(countryCode(e))+'</span></span><span class="node-card-state '+esc(e.status)+'"><span class="dot '+esc(e.status)+'"></span>'+esc(STATUS[e.status] || e.status)+'</span></div>'
      +'<div class="node-card-ip">'+esc(e.exit_ip || (e.status==='starting'?'连接中…':'—'))+'</div>'
      +'<div class="node-card-meta">'+typeBadge(e)+'<span class="node-card-id"><span>#'+(e.order || e.slot)+'</span><span class="node-card-protocol">'+esc(entry?entry.protocol.toUpperCase():'SOCKS5 / HTTP')+'</span></span></div>'
      +'<div class="node-card-network">'+networkOwner(e)+'<div class="node-card-location">'+LOCATION_ICON+'<span>'+esc(exitLocation(e))+'</span></div></div>'
      +'<div class="node-card-facts"><div><small>'+(entry?'节点端口':'代理端口')+'</small><strong>'+(entry?entry.port:e.port)+'</strong></div><div title="'+esc(latency.title)+'"><small>'+(latency.approx?'参考总延迟':'总延迟')+(latency.method?'<span class="latency-method">'+esc(latency.method)+'</span>':'')+'</small><strong'+(/^\d/.test(latency.label)?'':' class="latency-unavailable"')+'>'+esc(latency.label)+'</strong>'+(latency.parts?'<span class="node-card-latency-parts">'+esc(latency.parts)+'</span>':'')+'</div></div>'
      +stabilityNotice(e)+selector+'<div class="node-card-actions"><button class="copy-node" data-copy-node="'+e.slot+'" title="复制当前'+esc(entry?entry.protocol.toUpperCase():'')+'节点"'+(ready?'':' disabled')+'><span>复制节点</span>'+(entrances.length>1?'<small>'+esc(entry.protocol.toUpperCase())+'</small>':'')+'</button><button class="switch-ip" data-swap="'+e.slot+'"'+(e.status==='starting'?' disabled':'')+'>'+cardIcon('redo')+'<span>切换 IP</span></button><button data-proxy-copy="socks5" data-slot="'+e.slot+'" title="复制 SOCKS5 代理地址" aria-label="复制 SOCKS5 代理地址"'+disabled+'>'+cardIcon('copy')+'<span>SOCKS5</span></button><button data-proxy-copy="http" data-slot="'+e.slot+'" title="复制 HTTP 代理地址" aria-label="复制 HTTP 代理地址"'+disabled+'>'+cardIcon('copy')+'<span>HTTP</span></button></div>'
      +'<details class="node-card-more" data-card-tools="'+e.slot+'"'+(expandedCardTools.has(e.slot)?' open':'')+'><summary><span>详情与管理</span>'+MORE_ICON+'</summary><div class="node-card-tools">'+(e.exit_ip?'<button data-ip="'+esc(e.exit_ip)+'">IP 详情</button>':'')+'<button data-cred="'+e.slot+'">代理凭据</button>'+(entry?'<button data-detail="'+entry.id+'">节点详情</button>':e.needs_node?'<button data-repair="'+e.slot+'"'+disabled+'>'+(e.entry_error?'修复入口':'入口准备中')+'</button>':'')+'<button class="stop-exit" data-stop="'+e.slot+'">停止出口</button></div></details>'
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
    return copy(socksURL(credHost(exit),exit.port,exit.socks_user,exit.socks_pass).replace(/^socks5:/,proxy.dataset.proxyCopy+':'));
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
