// Optional DOM interaction checks. Requires Node.js and jsdom.
// Render HTML with MIKU_UI_OUTPUT=/tmp/miku-ui go test ./src -run TestPanelBrowserFixture
// Then: node scripts/test-ui.cjs /tmp/miku-ui/index.html
const assert = require('node:assert/strict');
const fs = require('node:fs');
const {JSDOM, VirtualConsole} = require(process.env.MIKU_JSDOM_MODULE || 'jsdom');
const html = fs.readFileSync(process.argv[2] || 'qa/index.html','utf8');
const pause=()=>new Promise(resolve=>setImmediate(resolve));
const settle=async()=>{for(let i=0;i<12;i++) await pause();};
const entryLink=(id)=>'vless://fixture-'+id+'@node.example.com:30001?type=tcp&security=tls&flow=xtls-rprx-vision&sni=node.example.com#Japan';
const exit=(slot,ids)=>({slot,order:slot,region:slot===1?'JP':'KR',country:slot===1?'日本':'韩国',exit_ip:'198.51.100.'+slot,status:'up',port:31000+slot,socks_user:'user'+slot,socks_pass:'pass'+slot,inbounds:ids.map(id=>({id,port:30000+id,enable:true,stack:'VLESS + TCP + TLS + VISION',protocol:'vless'}))});
const runningJob=()=>({id:'quick-123',kind:'quick-setup',summary:'一键配置',status:'running',done:2,total:16,steps:Array.from({length:16},(_,i)=>({label:'步骤 '+i,status:i<2?'ok':i===2?'running':'pending',detail:i===2?'正在读取可用节点，请稍等一下…':''}))});
const finishedJob=(ready=11)=>({...runningJob(),status:ready===11?'done':'failed',done:16,steps:Array.from({length:16},(_,i)=>({label:'步骤 '+i,status:'ok',detail:i===15?'节点链接已准备好':''})),quick_result:{ready,target:11,regions:[{code:'JP',name:'日本',ready:Math.min(5,ready),target:5},{code:'KR',name:'韩国',ready:ready===11?3:0,target:3},{code:'US',name:'美国',ready:ready===11?3:0,target:3}]}});
async function setup({savedMode,job=null,sessionSeen=''}={}){
 const state={jobs:job?[job]:[],requests:[],copied:[],errors:[],linkMissing:false,subError:false,linksError:false,bindError:false,ipError:false,ipReply:null,exits:[exit(1,[501,502]),exit(2,[601])]};
 const vc=new VirtualConsole();vc.on('jsdomError',e=>state.errors.push(e.message));
 const dom=new JSDOM(html,{url:'https://panel.example.com/private-path/',runScripts:'dangerously',pretendToBeVisual:true,virtualConsole:vc,beforeParse(w){
  if(savedMode!==undefined) w.localStorage.setItem('miku.showcase',savedMode);
  if(sessionSeen) w.sessionStorage.setItem('miku.quickResult./private-path/',sessionSeen);
  w.HTMLElement.prototype.scrollIntoView=function(){};
  w.matchMedia=()=>({matches:false,addEventListener(){},removeEventListener(){}});
  w.confirm=()=>{throw Error('unexpected extra confirmation')};
  Object.defineProperty(w,'isSecureContext',{value:true});
  Object.defineProperty(w.navigator,'clipboard',{value:{writeText:async v=>state.copied.push(v)}});
  w.fetch=async (path,opts={})=>{
   const u=new URL(path,w.location.href);state.requests.push({url:u,opts});
   let body={},status=200;
   if(u.pathname.endsWith('/api/exits')) body={exits:state.exits,direct:[],public_ip:'203.0.113.10',backend:'native'};
   else if(u.pathname.endsWith('/api/jobs')) body=state.jobs;
   else if(u.pathname.endsWith('/api/xui/bind') && state.bindError){status=502;body={error:'切换失败，原落地保留'};}
   else if(u.pathname.endsWith('/api/ping'))status=204;
   else if(u.pathname.endsWith('/api/ipinfo')){
    if(state.ipError){status=502;body={error:'属性查询暂不可用'};}
    else body=state.ipReply?await state.ipReply(u.searchParams.get('ip')):{ip:u.searchParams.get('ip'),location:'美国 加利福尼亚州 圣何塞',asn:46375,organization:'Fixture Telecom',isp:'Fixture ISP',company:'Fixture Network',ip_type:'residential',family:'IPv4',attribute:'住宅 IP',hosting:false,mobile:false,proxy:true,type_reason:'根据运营商信息推断',type_confidence:'medium',geo_source:'ping0.cc',asn_source:'ping0.cc',source:'ip-api.com',checked:'2026-10-15T09:20:00Z',geo_checked:'2026-10-15T09:21:00Z'};
   }
   else if(u.pathname.endsWith('/api/sub')){
    if(state.subError){status=502;body={error:'订阅读取暂不可用'};}
    else body={path:'/private-path/sub?token=fixture-token'};
   }else if(u.pathname.endsWith('/api/sub/links')){
    if(state.linksError){status=502;body={error:'节点链接读取暂不可用'};}
    else body={links:[entryLink(501),entryLink(502),entryLink(601)]};
   }else if(u.pathname.endsWith('/api/regions')) body=[{code:'JP',name:'日本',available:4,best_ping:80,best_speed_mbps:100}];
   else if(u.pathname.endsWith('/api/quick-setup')){
    const input=JSON.parse(opts.body);
    if(input.domain.startsWith('https://')){status=400;body={error:'请填写有效域名'};}
    else{body={job:'quick-123'};status=202;state.jobs=[runningJob()];}
   }else if(u.pathname.endsWith('/api/quick-setup/stop')){
    assert.equal(opts.method,'POST');assert.equal(u.searchParams.get('id'),'quick-123');
    body={ok:'正在停止后台补齐'};status=202;state.jobs=[finishedJob(3)];
   }else if(u.pathname.endsWith('/api/provision/stop')){
    assert.equal(opts.method,'POST');assert.equal(u.searchParams.get('id'),'ordinary-123');
    state.jobs=state.jobs.map(j=>({...j,status:'failed'}));status=202;
   }else if(u.pathname.endsWith('/api/stop-all')){
    assert.equal(opts.method,'POST');state.jobs=state.jobs.map(j=>({...j,status:'failed'}));state.exits=[];
   }else if(u.pathname.endsWith('/api/exits/links')){
    if(state.linkMissing){status=404;body={error:'此出口没有可复制的启用节点，请先创建或修复入口'};}
    else body={links:[entryLink(u.searchParams.get('id') || '501')]};
   }
   return {ok:status>=200&&status<300,status,json:async()=>body,arrayBuffer:async()=>new ArrayBuffer(0)};
  };
 }});
 await settle();return {dom,w:dom.window,d:dom.window.document,state};
}
(async()=>{
 let a=await setup();
 assert.equal(a.state.errors.length,0,a.state.errors.join('\n'));
 assert(a.d.body.classList.contains('showcase-mode'),'new browser should use showcase');
 assert.equal(a.d.querySelectorAll('.node-card').length,2);
 assert.equal(a.d.querySelector('.node-card .country-flag').getAttribute('src'),'assets/flags/jp.svg','desktop flags must use local SVGs');
 a.d.querySelector('#quickSetupBtn').click();assert(a.d.querySelector('#quickbox').classList.contains('open'));
 a.d.querySelector('#quickBind').click();await settle();assert.match(a.d.querySelector('#quickHint').textContent,/填入域名/);
 assert.equal(a.state.requests.filter(x=>x.url.pathname.endsWith('/api/quick-setup')).length,0);
 a.d.querySelector('#quickDomain').value='https://example.com';a.d.querySelector('#quickBind').click();await settle();
 assert(a.d.querySelector('#quickbox').classList.contains('open'));assert.match(a.d.querySelector('#quickHint').textContent,/域名/);
 a.d.querySelector('#quickDomain').value='node.example.com';a.d.querySelector('#quickBind').click();await settle();
 assert(!a.d.querySelector('#quickbox').classList.contains('open'));
 assert(!a.d.querySelector('#quickStatus').hidden);assert(a.d.querySelector('#quickSetupBtn').disabled);
 assert.match(a.d.querySelector('#quickDetail').textContent,/请稍等/);
 assert.deepEqual(JSON.parse(a.state.requests.filter(x=>x.url.pathname.endsWith('/api/quick-setup')).at(-1).opts.body),{domain:'node.example.com'});
 assert.equal(a.state.requests.filter(x=>x.url.pathname.endsWith('/api/panel/mode')).length,0,'core change belongs in backend task');
 assert.equal(a.state.errors.length,0,a.state.errors.join('\n'));a.dom.window.close();
 a=await setup({savedMode:'0'});assert(!a.d.body.classList.contains('showcase-mode'));
 a.d.querySelector('#quickSetupBtn').click();a.d.querySelector('#quickDomain').value='ignored.example.com';a.d.querySelector('#quickSkip').click();await settle();
 assert.deepEqual(JSON.parse(a.state.requests.find(x=>x.url.pathname.endsWith('/api/quick-setup')).opts.body),{domain:''});
 assert(a.d.body.classList.contains('showcase-mode'));a.dom.window.close();
 a=await setup({job:runningJob()});
 assert(!a.d.querySelector('#quickStop').hidden,'running background task must be stoppable');
 assert.match(a.d.querySelector('#quickCount').textContent,/后台持续补齐/);
 assert(!a.d.querySelector('#quickresult').classList.contains('open'),'running partial task must not show a finished result');
 a.d.querySelector('#quickStop').click();await settle();
 assert.equal(a.state.requests.filter(x=>x.url.pathname.endsWith('/api/quick-setup/stop')).length,1);
 assert(a.d.querySelector('#quickStop').hidden,'finished task must hide stop control');
 assert(!a.d.querySelector('#quickSetupBtn').disabled,'user can start another top-up after stopping');
 assert.equal(a.d.querySelectorAll('.node-card').length,2,'stopping top-up must preserve existing exits');
 assert(a.d.querySelector('#quickresult').classList.contains('open'),'stopping should offer retained links');
 assert.equal(a.state.errors.length,0,a.state.errors.join('\n'));a.dom.window.close();
 a=await setup({savedMode:'0',job:runningJob()});
 assert(!a.d.querySelector('#quickStatus').hidden,'refresh should restore task');assert(a.d.body.classList.contains('showcase-mode'));
 a.d.querySelector('[data-card-entry="1"]').value='502';a.d.querySelector('[data-card-entry="1"]').dispatchEvent(new a.w.Event('change',{bubbles:true}));
 a.d.querySelector('[data-proxy-copy="socks5"][data-slot="1"]').click();await settle();
 assert(a.d.querySelector('#proxyguide').classList.contains('open'));
 assert.match(a.d.querySelector('.proxy-highlight strong').textContent,/链式代理.*指纹浏览器/);
 assert.equal(a.d.querySelectorAll('#proxyguide .guide-step').length,2);
 assert.equal(a.state.copied.at(-1),'socks5://user1:pass1@203.0.113.10:31001');
 assert.equal(a.d.querySelector('#proxyguideLinks').value,entryLink(502));assert(!a.d.querySelector('#proxyguideCopyEntry').disabled);
 a.d.querySelector('#proxyguideCopyEntry').click();await settle();assert.equal(a.state.copied.at(-1),entryLink(502));
 a.w.closeModal('proxyguide');a.w.openCred(1);a.d.querySelector('#cruser').value='unsaved';a.d.querySelector('#crhttpcopy').click();await settle();
 assert(!a.d.querySelector('#credbox').classList.contains('open'),'credential dialog should not cover the guide');
 assert(a.d.querySelector('#proxyguide').classList.contains('open'));
 assert.equal(a.state.copied.at(-1),'http://user1:pass1@203.0.113.10:31001','copy must use saved credentials');
 a.state.linkMissing=true;await a.w.openProxyGuide(2,'http');
 assert.equal(a.d.querySelector('#proxyguideLinks').value,'');assert(a.d.querySelector('#proxyguideCopyEntry').disabled);
 assert(!a.d.querySelector('#proxyguideRetry').hidden);assert.match(a.d.querySelector('#proxyguideStatus').textContent,/修复入口/);
 a.state.linkMissing=false;a.d.querySelector('#proxyguideRetry').click();await settle();assert.equal(a.d.querySelector('#proxyguideLinks').value,entryLink(601));
 a.w.closeModal('proxyguide');
 a.state.jobs[0].status='failed';a.state.jobs[0].done=16;a.state.jobs[0].steps[4]={label:'日本 1',status:'failed',detail:'日本暂时没有更多可用节点'};
 await a.w.poll();assert(!a.d.querySelector('#quickSetupBtn').disabled);assert(a.d.querySelector('#quickStatus').classList.contains('finished'));
 assert.match(a.d.querySelector('.step-detail').textContent,/没有更多可用节点/,'failure must be readable without hover');
 a.d.querySelector('#quickHide').click();assert(a.d.querySelector('#quickStatus').hidden);
 assert.equal(a.state.errors.length,0,a.state.errors.join('\n'));a.dom.window.close();
 a=await setup({job:runningJob()});a.state.jobs=[finishedJob()];await a.w.poll();await settle();
 assert(a.d.querySelector('#quickresult').classList.contains('open'),'completion should automatically show both link types');
 assert.equal(a.d.querySelector('#quickresultSub').value,'https://panel.example.com/private-path/sub?token=fixture-token');
 assert.equal(a.d.querySelector('#quickresultLinks').value,[entryLink(501),entryLink(502),entryLink(601)].join('\n'));
 assert.equal(a.d.querySelectorAll('#quickresultRegions span').length,3);
 a.d.querySelector('#quickresultFormat').value='clash';a.d.querySelector('#quickresultFormat').dispatchEvent(new a.w.Event('change'));
 assert.equal(new URL(a.d.querySelector('#quickresultSub').value).searchParams.get('target'),'clash');
 a.d.querySelector('#quickresultCopySub').click();await settle();assert.equal(a.state.copied.at(-1),a.d.querySelector('#quickresultSub').value);
 a.d.querySelector('#quickresultCopyLinks').click();await settle();assert.equal(a.state.copied.at(-1),a.d.querySelector('#quickresultLinks').value);
 a.w.closeModal('quickresult');await a.w.poll();await settle();assert(!a.d.querySelector('#quickresult').classList.contains('open'),'polling must not reopen dismissed results');
 a.d.querySelector('#quickResultBtn').click();await settle();assert(a.d.querySelector('#quickresult').classList.contains('open'),'result can be reopened');
 a.d.querySelector('#quickresultAdd').click();await settle();assert(a.d.querySelector('#wizard').classList.contains('open'),'add exit shortcut should open existing wizard');
 assert.equal(a.state.errors.length,0,a.state.errors.join('\n'));a.dom.window.close();
 a=await setup({job:finishedJob(),sessionSeen:'quick-123'});assert(!a.d.querySelector('#quickresult').classList.contains('open'),'refresh should respect viewed result');
 a.state.subError=true;a.d.querySelector('#quickResultBtn').click();await settle();
 assert(a.d.querySelector('#quickresultCopySub').disabled);assert(!a.d.querySelector('#quickresultCopyLinks').disabled,'subscription error should not discard available node links');
 assert(!a.d.querySelector('#quickresultRetry').hidden);assert.match(a.d.querySelector('#quickresultSubError').textContent,/暂不可用/);
 a.state.subError=false;a.state.linksError=true;a.d.querySelector('#quickresultRetry').click();await settle();
 assert(!a.d.querySelector('#quickresultCopySub').disabled);assert(a.d.querySelector('#quickresultCopyLinks').disabled,'errors must not become copyable node links');
 a.state.linksError=false;a.d.querySelector('#quickresultRetry').click();await settle();assert(!a.d.querySelector('#quickresultCopyLinks').disabled);
 assert.equal(a.state.errors.length,0,a.state.errors.join('\n'));a.dom.window.close();
 a=await setup({job:finishedJob(4)});assert(a.d.querySelector('#quickresult').classList.contains('open'),'partial success should still offer usable links');
 assert.match(a.d.querySelector('#quickresultHeading').textContent,/4 个可用节点/);a.dom.window.close();
 a=await setup({job:runningJob()});a.w.openModal('settings');a.state.jobs=[finishedJob()];await a.w.poll();await settle();
 assert(!a.d.querySelector('#quickresult').classList.contains('open'),'completion must wait for an existing dialog');
 a.w.closeModal('settings');await a.w.poll();await settle();assert(a.d.querySelector('#quickresult').classList.contains('open'));
 assert.equal(a.state.errors.length,0,a.state.errors.join('\n'));a.dom.window.close();
 a=await setup();
 const checked=new Date().toISOString();
 a.state.exits[0].gate_latency={ms:250,checked,method:'tcp'};a.state.exits[1].gate_latency={ms:35,checked,method:'tcp'};
 await a.w.poll();await settle();assert.equal(a.d.querySelector('.node-card').dataset.exitCard,'2','measured fastest should lead');
 a.d.querySelector('#exitSort').value='order';a.d.querySelector('#exitSort').dispatchEvent(new a.w.Event('change'));
 assert.equal(a.d.querySelector('.node-card').dataset.exitCard,'1','creation order remains available');
 a.state.exits[1].gate_latency.checked=new Date(Date.now()-120000).toISOString();
 a.d.querySelector('#exitSort').value='fast';a.d.querySelector('#exitSort').dispatchEvent(new a.w.Event('change'));
 assert.equal(a.d.querySelector('.node-card').dataset.exitCard,'1','expired measurements cannot lead');
 a.d.querySelector('#newexit').click();await settle();
 assert.equal(a.d.querySelector('#regions [data-rg="JP"] .country-flag').getAttribute('src'),'assets/flags/jp.svg','country picker must show local flags');
 a.w.closeModal('wizard');a.state.exits[0].host='jp-fixture';a.state.exits[1].host='kr-fixture';await a.w.poll();await settle();
 a.w.renderDetail({id:501,tag:'fixture-tag',remark:'Fixture',port:30001,protocol:'vless',network:'tcp',clients:[{email:'fixture-client',id:'fixture-uuid',bound_to:'jp-fixture'}]});
 const picker=a.d.querySelector('#dbind').closest('.exit-picker');
 assert.equal(picker.querySelector('[data-exit-choice="kr-fixture"] .country-flag').getAttribute('src'),'assets/flags/kr.svg','landing dropdown must render country images');
 picker.querySelector('[data-exit-choice="kr-fixture"]').click();await settle();
 assert.equal(a.d.querySelector('#dbind').value,'kr-fixture');
 assert(a.state.requests.some(x=>x.url.pathname.endsWith('/api/xui/bind') && x.url.searchParams.get('host')==='kr-fixture' && x.opts.method==='POST'),'flag choice should use the existing landing API');
 a.state.bindError=true;picker.querySelector('[data-exit-choice="jp-fixture"]').click();await settle();
 assert.equal(a.d.querySelector('#dbind').value,'kr-fixture','failed landing changes must restore the actual selection');
 assert.equal(picker.querySelector('summary .country-flag').getAttribute('src'),'assets/flags/kr.svg');
 assert.equal(a.state.errors.length,0,a.state.errors.join('\n'));a.dom.window.close();
 a=await setup({job:{...runningJob(),quick_result:finishedJob(2).quick_result}});
 assert(!a.d.querySelector('#quickResultBtn').hidden,'partial running results must be immediately available');
 assert(!a.d.querySelector('#quickresult').classList.contains('open'),'partial results should not interrupt management');
 a.d.querySelector('#quickResultBtn').click();await settle();
 assert.match(a.d.querySelector('#quickresultIntro').textContent,/后台补齐/);
 a.w.closeModal('quickresult');a.state.jobs=[finishedJob()];await a.w.poll();await settle();
 assert(a.d.querySelector('#quickresult').classList.contains('open'),'viewing early links must not suppress final result');
 assert.equal(a.state.errors.length,0,a.state.errors.join('\n'));a.dom.window.close();
 a=await setup();
 assert.equal(a.d.querySelector('#onlineCount').textContent,'2');
 assert.equal(a.d.querySelector('#regionCount').textContent,'2');
 assert.equal(a.d.querySelector('#entryCount').textContent,'3');
 a.d.querySelector('[data-theme-toggle]').click();assert.equal(a.d.documentElement.dataset.theme,'light');
 a.d.querySelector('[data-theme-toggle]').click();assert.equal(a.d.documentElement.dataset.theme,'dark');
 a.d.querySelector('.node-card-ip').click();await settle();
 assert(a.d.querySelector('#ipbox').classList.contains('open'));
 assert(a.d.body.classList.contains('modal-active'),'dialog must lock page scrolling');
 assert.equal(a.d.querySelector('#ipDetails').firstElementChild.className,'ip-result-toolbar','Ping0 must precede the IP facts');
 assert.equal(a.d.querySelector('#ipDetails a').href,'https://ping0.cc/ip/198.51.100.1');
 assert.equal(a.d.querySelector('.ip-overview h3').textContent,'198.51.100.1');
 assert.equal(a.d.querySelectorAll('.ip-fact').length,5,'unavailable fields should not become a long table');
 const flags=Array.from(a.d.querySelectorAll('.ip-signal')).map(x=>x.textContent);
 assert(flags.includes('机房否'));assert(flags.includes('代理是'));assert(flags.includes('VPN未提供'));assert(flags.includes('Tor未提供'));
 assert.match(a.d.querySelector('.ip-provenance').textContent,/定位 \/ ASN：ping0.cc/);
 assert.match(a.d.querySelector('.ip-classification').textContent,/规则推断/);
 a.d.querySelector('[data-copy-ip]').click();await settle();assert.equal(a.state.copied.at(-1),'198.51.100.1');
 a.state.ipReply=async ip=>({ip,location:'<img src=x onerror="window.injected=true">',organization:'<b>Untrusted operator</b>',type_reason:'<script>window.injected=true</script>'});
 a.d.querySelector('#ipAddress').value='2001:db8::1';
 a.d.querySelector('#ipSearch').dispatchEvent(new a.w.Event('submit',{bubbles:true,cancelable:true}));await settle();
 assert.equal(a.d.querySelector('.ip-overview h3').textContent,'2001:db8::1');
 assert.match(a.d.querySelector('.ip-facts').textContent,/<img src=x/);
 assert.equal(a.d.querySelectorAll('#ipDetails img,#ipDetails script,#ipDetails b').length,5,'only the five trusted signal values may use b tags');
 assert.equal(a.w.injected,undefined);
 let releaseOld;
 a.state.ipReply=ip=>ip==='198.51.100.8'?new Promise(resolve=>releaseOld=resolve):Promise.resolve({ip,organization:'Newest operator'});
 a.d.querySelector('#ipAddress').value='198.51.100.8';const old=a.w.lookupIP();await settle();
 a.d.querySelector('#ipAddress').value='198.51.100.9';await a.w.lookupIP();
 releaseOld({ip:'198.51.100.8',organization:'Stale operator'});await old;
 assert.equal(a.d.querySelector('.ip-overview h3').textContent,'198.51.100.9','a late query must not replace newer results');
 a.state.ipError=true;await a.w.lookupIP();
 assert.match(a.d.querySelector('.ip-state.bad').textContent,/暂不可用/);
 assert.equal(a.d.querySelector('#ipDetails a').href,'https://ping0.cc/ip/198.51.100.9','failed queries must retain the direct Ping0 action');
 assert(!a.d.querySelector('#ipDetails').hasAttribute('aria-busy'));
 a.w.closeModal('ipbox');assert(!a.d.body.classList.contains('modal-active'));
 a.d.querySelector('#ipBtn').focus();a.d.querySelector('#ipBtn').click();
 a.d.dispatchEvent(new a.w.KeyboardEvent('keydown',{key:'Escape',bubbles:true}));
 assert.equal(a.d.activeElement,a.d.querySelector('#ipBtn'),'closing IP info should return keyboard focus');
 a.state.exits=[];await a.w.poll();assert.equal(a.d.querySelector('#onlineCount').textContent,'0');
 assert.equal(a.state.errors.length,0,a.state.errors.join('\n'));a.dom.window.close();
 a=await setup({job:{id:'ordinary-123',kind:'provision',summary:'开3个日本出口',status:'running',done:1,total:3,steps:[{label:'日本1',status:'ok',detail:'198.51.100.1'},{label:'日本2',status:'running'},{label:'日本3',status:'running'}]}});
 assert.match(a.d.querySelector('#wizard').textContent,/3 倍/);
 assert(a.d.querySelector('[data-provision-stop]'),'ordinary provisioning must be stoppable');
 a.d.querySelector('[data-provision-stop]').click();await settle();
 assert(a.state.requests.some(x=>x.url.pathname.endsWith('/api/provision/stop')));
 assert.equal(a.state.exits.length,2,'stop top-up must keep successful exits');
 a.state.jobs=[{...runningJob(),kind:'provision'}];a.state.exits=[];await a.w.poll();
 assert(!a.d.querySelector('#stopall').disabled,'hidden candidates must still allow stop-all');
 a.w.confirm=()=>true;a.d.querySelector('#stopall').click();await settle();
 assert(a.state.requests.some(x=>x.url.pathname.endsWith('/api/stop-all')),'stop-all must cancel workers as well as visible exits');
 assert.equal(a.state.errors.length,0,a.state.errors.join('\n'));a.dom.window.close();
 console.log('PASS: panel flows, partial results, ordinary provisioning cancellation, stop-all, IP queries, escaped metadata, stale response isolation, dialog focus, counts and themes.');
})().catch(e=>{console.error(e);process.exitCode=1;setTimeout(()=>process.exit(1),100)});
