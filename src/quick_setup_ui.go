package main

const quickSetupCSS = `
.step{max-width:100%;overflow-wrap:anywhere;white-space:normal}.step-detail{display:block;line-height:1.7;font-size:11px;max-width:34em}
.quick-launch{box-shadow:none}
.quick-intro{font-size:15px;line-height:1.8;margin:0 0 18px}.quick-regions{display:flex;gap:8px;flex-wrap:wrap;margin:16px 0}.quick-regions span{padding:7px 12px;border-radius:999px;color:var(--accent);background:#39c5bb12;border:1px solid var(--line);font-size:13px}.quick-note{padding:12px 15px;border-radius:12px;border:1px solid var(--line);background:var(--bg);color:var(--dim);font-size:12px;line-height:1.8}.quick-note strong{color:var(--text)}
.quick-status{display:grid;grid-template-columns:88px minmax(0,1fr);gap:20px;align-items:center;border:1px solid var(--line);border-radius:16px;padding:18px 22px;margin:0 0 18px;background:var(--panel);overflow:hidden}.quick-status[hidden]{display:none}.quick-status h3{font-size:17px;margin:0 0 6px}.quick-status p{color:var(--dim);margin:5px 0;font-size:13px;overflow-wrap:anywhere}.quick-status small{color:var(--dim)}.quick-status-top{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.quick-status-top button{margin-left:auto}.quick-status .quick-eyebrow{color:var(--accent);letter-spacing:2px;font-size:10px;font-weight:700}.quick-progress{width:100%;height:5px;accent-color:var(--accent);margin:12px 0 3px;border:0;border-radius:5px;overflow:hidden}.quick-progress::-webkit-progress-bar{background:var(--line)}.quick-progress::-webkit-progress-value{background:var(--accent);transition:width .5s}
.miku-stage{height:104px;position:relative;isolation:isolate;display:grid;place-items:center}.miku-stage:before{content:'';position:absolute;width:82px;height:82px;top:8px;border-radius:50%;border:1px dashed #39c5bb70;animation:miku-orbit 22s linear infinite;box-shadow:0 0 25px #39c5bb13,inset 0 0 25px #39c5bb10}.miku-stage:after{content:'';position:absolute;bottom:0;width:72px;height:9px;background:#39c5bb35;filter:blur(5px);border-radius:50%;animation:miku-shadow 3.4s ease-in-out infinite}.miku-stage img{height:104px;max-width:88px;object-fit:contain;position:relative;z-index:1;filter:drop-shadow(0 8px 12px #0002);animation:miku-float 3.4s ease-in-out infinite;transform-origin:50% 90%}.miku-note{position:absolute;color:var(--accent);font-size:24px;z-index:2;animation:miku-note 3s ease-in-out infinite}.miku-note.one{left:8px;top:40px}.miku-note.two{right:3px;top:15px;animation-delay:-1.5s;color:#e988b7}.quick-status.finished .miku-stage *,.quick-status.finished .miku-stage:before,.quick-status.finished .miku-stage:after{animation:none}.quick-status.finished .miku-note{opacity:.6}
@keyframes miku-float{0%,100%{transform:translateY(0) rotate(-2deg)}50%{transform:translateY(-8px) rotate(2deg)}}@keyframes miku-shadow{0%,100%{transform:scaleX(1);opacity:.8}50%{transform:scaleX(.75);opacity:.4}}@keyframes miku-orbit{to{transform:rotate(360deg)}}@keyframes miku-note{0%,100%{transform:translateY(7px) rotate(-12deg);opacity:.3}50%{transform:translateY(-10px) rotate(12deg);opacity:1}}
.proxy-guide-links{width:100%;min-height:110px;resize:vertical;font:12px/1.7 ui-monospace,monospace;overflow-wrap:anywhere;white-space:pre-wrap}.proxy-guide-status{color:var(--dim);font-size:12px;line-height:1.8;margin:12px 0}.proxy-guide-state{padding:10px 12px;border:1px solid var(--line);border-radius:9px;color:var(--dim);font-size:12px;overflow-wrap:anywhere}
.quick-status-actions{display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin-top:12px}.quick-status-actions button{font-size:12px}.quick-status-actions button[hidden]{display:none}
.proxy-highlight{padding:16px 18px;border:1px solid var(--datacenter);border-left:4px solid var(--datacenter);border-radius:12px;background:var(--datacenter-bg);margin-bottom:18px}.proxy-highlight strong{display:block;color:var(--datacenter);font-size:20px;line-height:1.45}.proxy-highlight p{margin:7px 0 0;color:var(--text);font-size:13px;line-height:1.7}
.guide-step{border:1px solid var(--line);border-radius:12px;padding:16px;margin-top:14px;background:var(--panel);min-width:0}.guide-step-heading{display:flex;align-items:center;gap:10px;margin-bottom:10px}.guide-step-heading b{display:grid;place-items:center;width:26px;height:26px;flex:none;border-radius:50%;background:var(--accent);color:var(--bg);font-size:13px}.guide-step-heading h3{font-size:15px;margin:0;color:var(--text)}.guide-step-heading button{margin-left:auto;font-size:12px}.guide-step p{font-size:12px;line-height:1.75;color:var(--dim);margin:7px 0 12px}.guide-step label.f{margin-bottom:0}.proxy-guide-state{background:var(--bg);color:var(--text);font:12px/1.8 ui-monospace,monospace}.proxy-guide-links{min-height:96px;max-height:180px}
.quick-result-hero{display:flex;align-items:flex-start;gap:12px;margin-bottom:12px}.quick-result-mark{display:grid;place-items:center;flex:none;width:38px;height:38px;border-radius:12px;background:var(--residential-bg);color:var(--residential);font-size:23px;font-weight:700}.quick-result-hero h3{font-size:18px;margin:0 0 5px;line-height:1.4;color:var(--text)}.quick-result-hero p{color:var(--dim);font-size:12px;line-height:1.7;margin:0}.quick-result-section{margin-top:16px;padding-top:16px;border-top:1px solid var(--line)}.quick-result-heading{display:flex;align-items:center;gap:8px;margin-bottom:10px;flex-wrap:wrap}.quick-result-heading h3{margin:0;font-size:14px;color:var(--text)}.quick-result-heading .spacer{min-width:0}.quick-result-heading button{font-size:12px}.quick-result-section label.f{margin-bottom:10px}.quick-result-links{width:100%;min-height:140px;max-height:220px;resize:vertical;font:12px/1.7 ui-monospace,monospace;white-space:pre-wrap;overflow-wrap:anywhere}.quick-add-note{display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin-top:16px;padding:14px 16px;border:1px solid var(--accent);border-radius:12px;background:var(--panel)}.quick-add-note div{flex:1;min-width:150px}.quick-add-note strong{display:block;font-size:13px;color:var(--accent);margin-bottom:4px}.quick-add-note p{font-size:12px;line-height:1.65;color:var(--dim);margin:0}.quick-add-note button{font-size:12px}.quick-result-error{color:var(--bad);font-size:12px;line-height:1.7;margin:8px 0}.quick-result-error:empty{display:none}
.node-card-proxy-label{grid-column:1/-1;margin:4px 0 0;font-size:11px;line-height:1.5;font-weight:700;letter-spacing:.3px;color:var(--datacenter);text-align:center}
@media(max-width:600px){#proxyguide .sheet .head h2{font-size:14px}.proxy-highlight{padding:13px 14px}.proxy-highlight strong{font-size:18px}.guide-step{padding:12px}.guide-step-heading{gap:7px;flex-wrap:wrap}.guide-step-heading h3{font-size:14px}.guide-step-heading button{padding:6px 8px}.quick-result-hero h3{font-size:16px}.quick-add-note{padding:12px}.quick-result-links{min-height:120px;max-height:190px}#quickresult .sheet .foot{flex-wrap:wrap}.node-card-proxy-label{font-size:10px}}
@media(max-width:600px){.quick-status{grid-template-columns:86px minmax(0,1fr);gap:13px;padding:16px 13px;border-radius:14px}.miku-stage{height:120px}.miku-stage img{height:120px;max-width:90px}.miku-stage:before{width:82px;height:82px;top:21px}.miku-note{font-size:18px}.miku-note.one{left:-3px;top:47px}.miku-note.two{right:-4px;top:25px}.quick-status h3{font-size:16px}.quick-status p{font-size:12px}.quick-status-top button{padding:4px 7px;font-size:11px}.quick-regions{gap:6px}.quick-regions span{font-size:12px;padding:6px 10px}}
@media(prefers-reduced-motion:reduce){.miku-stage *,.miku-stage:before,.miku-stage:after{animation:none!important}.quick-progress::-webkit-progress-value{transition:none}}
`

const quickSetupStatusHTML = `
<section id="quickStatus" class="quick-status" aria-label="一键配置进度" hidden>
 <div class="miku-stage" aria-hidden="true"><span class="miku-note one">♪</span><span class="miku-note two">♫</span><img src="assets/miku-hero-v1.png" alt=""></div>
 <div><div class="quick-status-top"><span class="quick-eyebrow">MIKU IS CONNECTING</span><button id="quickHide" hidden>收起</button></div><h3 id="quickTitle">Miku 正在为你点亮下一站</h3><p id="quickDetail" role="status" aria-live="polite">请稍等一下，正在准备连接…</p><progress class="quick-progress" id="quickProgress" max="16" value="0" aria-label="配置进度"></progress><small id="quickCount">多路并行连接，优先保留各地区先连通的节点。</small><div class="quick-status-actions"><button id="quickStop" hidden>停止补齐</button><button id="quickResultBtn" hidden>订阅与全部节点链接</button></div></div>
</section>
`

const quickSetupModalHTML = `
<div class="modal" id="quickbox" role="dialog" aria-modal="true" aria-labelledby="quickboxTitle"><div class="sheet"><div class="head"><h2 id="quickboxTitle">一键配置 · 让 Miku 帮你准备好</h2><span class="spacer"></span><button class="icon" data-close="quickbox" aria-label="关闭一键配置">关闭</button></div><div class="body">
 <p class="quick-intro">想为节点绑定一个域名吗？<br><span class="dim">有域名就交给 Miku 准备证书；暂时没有，也可以直接开始。</span></p>
 <label class="f"><span>节点域名（可选）</span><input id="quickDomain" type="text" inputmode="url" autocomplete="off" autocapitalize="none" spellcheck="false" placeholder="node.example.com" aria-describedby="quickHint"></label>
 <div class="quick-regions"><span><img class="country-flag" src="assets/flags/jp.svg" alt="JP">日本 × 5</span><span><img class="country-flag" src="assets/flags/kr.svg" alt="KR">韩国 × 3</span><span><img class="country-flag" src="assets/flags/us.svg" alt="US">美国 × 3</span></div>
 <div class="quick-note"><strong>自动筛选，连通即保留</strong><br>后台最多 33 路候选并行竞速，优先凑齐日本 5、韩国 3、美国 3 个可用节点。失败或超时自动换下一个，来源先到先用；各地区达标后停止多余连接。已就绪节点立即显示，可提前复制订阅；需要更多可到「出口与节点 → 新建出口」添加。<br><br><strong>绑定域名</strong>：VLESS + TCP + TLS + Vision，自动复用或申请证书。请先将域名解析到本机，并放行公网 80 端口。<br><strong>暂不绑定</strong>：VLESS + TCP + Reality + Vision，密钥自动生成。<br>再次执行可更换一键节点域名或改回 Reality，自动沿用现有出口、端口和 UUID，只补齐缺少的数量；更换后请在客户端更新订阅。后台补齐时已连通节点即可使用，也可随时停止补齐。节点域名与面板登录地址可分别设置。</div>
 <div id="quickHint" class="hint" role="status" aria-live="polite"></div>
 </div><div class="foot"><button id="quickSkip">暂不绑定，直接开始</button><span class="spacer"></span><button class="primary" id="quickBind">绑定域名并开始</button></div></div></div>
<div class="modal" id="proxyguide" role="dialog" aria-modal="true" aria-labelledby="proxyguideTitle"><div class="sheet"><div class="head"><h2 id="proxyguideTitle">代理使用指南</h2><span class="spacer"></span><button class="icon" data-close="proxyguide" aria-label="关闭代理使用说明">关闭</button></div><div class="body">
 <div class="proxy-highlight"><strong>链式代理 / 指纹浏览器</strong><p>先连接入口，再填写代理信息。两步完成这个出口的配置。</p></div><p class="proxy-guide-status" id="proxyguideIntro"></p>
 <section class="guide-step"><div class="guide-step-heading"><b aria-hidden="true">1</b><h3>连接对应的入口节点</h3><button class="primary" id="proxyguideCopyEntry" disabled>复制入口节点</button></div><p>将下方节点链接导入代理客户端，并连接成功。</p><label class="f"><span>此出口对应的入口节点链接</span><textarea id="proxyguideLinks" class="proxy-guide-links" readonly spellcheck="false" aria-label="此出口对应的入口节点链接"></textarea></label><p class="proxy-guide-status" id="proxyguideStatus" role="status" aria-live="polite"></p><button id="proxyguideRetry" hidden>重新读取入口</button></section>
 <section class="guide-step"><div class="guide-step-heading"><b aria-hidden="true">2</b><h3>填写应用代理设置</h3><button id="proxyguideCopyProxy">复制代理地址</button></div><p>在链式代理或指纹浏览器中选择对应协议，填写代理地址、端口、用户名和密码。</p><div class="proxy-guide-state" id="proxyguideProxy"></div></section>
 <p class="proxy-guide-status">SOCKS5 / HTTP 地址用于应用的代理设置；VLESS 节点链接用于代理客户端。当前网络能直达代理端口时，也可直接配置应用。</p>
 </div><div class="foot"><span class="spacer"></span><button data-close="proxyguide">知道了</button></div></div></div>
<div class="modal" id="quickresult" role="dialog" aria-modal="true" aria-labelledby="quickresultTitle"><div class="sheet"><div class="head"><h2 id="quickresultTitle">订阅与全部节点链接</h2><span class="spacer"></span><button class="icon" data-close="quickresult" aria-label="关闭配置结果">关闭</button></div><div class="body">
 <div class="quick-result-hero"><span class="quick-result-mark" aria-hidden="true">✓</span><div><h3 id="quickresultHeading">节点已就绪</h3><p id="quickresultIntro"></p></div></div><div class="quick-regions" id="quickresultRegions"></div>
 <section class="quick-result-section"><div class="quick-result-heading"><h3>订阅地址</h3><span class="spacer"></span><button class="primary" id="quickresultCopySub" disabled>复制订阅地址</button></div><label class="f"><span>客户端格式</span><select id="quickresultFormat"><option value="base64">通用（Base64）</option><option value="v2ray">V2Ray / v2rayN（Base64）</option><option value="clash">Clash / Mihomo（YAML）</option><option value="singbox">sing-box（JSON）</option><option value="links">明文分享链接</option></select></label><label class="f"><span>在客户端中新建订阅，粘贴此地址</span><input id="quickresultSub" type="text" readonly spellcheck="false"></label><p class="hint">出口增删后，更新订阅即可同步。订阅地址包含访问口令，请妥善保管。</p><p class="quick-result-error" id="quickresultSubError" role="status" aria-live="polite"></p></section>
 <section class="quick-result-section"><div class="quick-result-heading"><h3>全部节点链接</h3><span class="count" id="quickresultLinkCount"></span><span class="spacer"></span><button id="quickresultCopyLinks" disabled>全部复制</button></div><label class="f"><span>也可复制以下链接，批量导入客户端</span><textarea id="quickresultLinks" class="quick-result-links" readonly spellcheck="false"></textarea></label><p class="quick-result-error" id="quickresultLinksError" role="status" aria-live="polite"></p></section>
 <aside class="quick-add-note"><div><strong>需要更多或其他地区的节点？</strong><p>到「出口与节点 → 新建出口」继续添加，再更新订阅即可。</p></div><button id="quickresultAdd">去添加出口</button></aside>
 </div><div class="foot"><button id="quickresultRetry" hidden>重新读取</button><span class="spacer"></span><button data-close="quickresult">完成</button></div></div></div>
`

const quickSetupJS = `
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
`
