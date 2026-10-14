package main

import "net/http"

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

const indexHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="theme-color" content="#0a1620">
<title>Miku 节点</title>
<script>` + themeInitJS + `</script>
<style>
:root{
  --bg:#0b1417; --panel:#122126; --line:#284047; --text:#e7f4f4;
  --dim:#9ab6bc; --accent:#39c5bb; --ok:#39c5bb; --warn:#eab96b; --bad:#f078a0;
}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--text);
  font:14px/1.6 system-ui,-apple-system,"Segoe UI",sans-serif}
header{display:flex;align-items:center;gap:16px;padding:10px 16px;
  border-bottom:1px solid var(--line);background:var(--panel)}
h1{font-size:13px;font-weight:600;margin:0;letter-spacing:0}
.spacer{flex:1}
button{font:inherit;color:var(--text);background:#222833;border:1px solid var(--line);
  border-radius:4px;padding:4px 10px;cursor:pointer;display:inline-flex;
  align-items:center;gap:5px;white-space:nowrap}
button:hover:not(:disabled){border-color:var(--accent)}
button:disabled{opacity:.45;cursor:default}
button.primary{background:var(--accent);border-color:var(--accent);color:#0b0e12;font-weight:600}
button.icon{padding:3px 6px;background:transparent;border-color:transparent;color:var(--dim)}
button.icon:hover:not(:disabled){color:var(--accent);border-color:var(--line)}
button.icon.danger:hover:not(:disabled){color:var(--bad);border-color:rgba(194,84,80,.35)}
svg{width:14px;height:14px;stroke:currentColor;fill:none;stroke-width:1.8;
  stroke-linecap:round;stroke-linejoin:round;flex:none}
main{padding:14px 16px 40px;max-width:1180px;margin:0 auto}
.bar{display:flex;align-items:center;gap:10px;margin-bottom:12px}
.bar h2{font-size:12px;margin:0;font-weight:600;color:var(--dim)}
.exit{border:1px solid var(--line);border-radius:6px;margin-bottom:8px;
  background:var(--panel);overflow:hidden}
.exit>.row{display:grid;gap:6px 12px;align-items:center;padding:9px 12px;
  grid-template-columns:14px auto minmax(95px,1fr) minmax(132px,auto) minmax(75px,.7fr) auto auto auto;
  grid-template-areas:"dot number location ip meta chips socks acts"}
.exit .dot{grid-area:dot}
.exit .exit-number{grid-area:number}
.exit .exit-location{grid-area:location}
.exit .ip{grid-area:ip}
.exit .meta{grid-area:meta}
.exit .chips{grid-area:chips}
.exit .socks{grid-area:socks}
.exit .acts{grid-area:acts}
.dot{width:8px;height:8px;border-radius:50%;background:var(--dim);justify-self:center}
.dot.up{background:var(--ok)}
.dot.starting{background:var(--warn);animation:pulse 1.2s ease-in-out infinite}
.dot.failed{background:var(--bad)}
@keyframes pulse{0%,100%{opacity:1}50%{opacity:.3}}
.ip{font-weight:600;font-variant-numeric:tabular-nums}
.meta{color:var(--dim);font-size:12px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.chips{display:flex;gap:6px;flex-wrap:wrap}
.chip{border:1px solid var(--line);border-radius:3px;padding:1px 7px;font-size:11px;
  color:var(--dim);cursor:pointer;background:#0e1116}
.chip:hover{border-color:var(--accent);color:var(--text)}
.chip.none{border-style:dashed;cursor:default}
.chip.none:hover{border-color:var(--line);color:var(--dim)}
.orphan{margin-top:18px;border:1px solid var(--line);border-radius:6px;
  background:var(--panel);padding:10px 12px}
.orphan .top{display:flex;align-items:center;gap:10px;margin-bottom:8px}
.orphan .top h3{font-size:12px;margin:0;font-weight:600;color:var(--dim)}
.socks{color:var(--dim);font-size:12px;font-variant-numeric:tabular-nums}
.socks button{background:transparent;border-color:transparent;color:var(--dim);
  font-size:12px;padding:2px 6px;font-variant-numeric:tabular-nums}
.socks button:hover:not(:disabled){color:var(--accent);border-color:var(--line)}
.socks button .lock{width:11px;height:11px;stroke-width:2}
.acts{display:flex;gap:2px;justify-self:end}
.errline{padding:0 12px 9px 38px;color:var(--bad);font-size:11px;
  overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.empty{border:1px dashed var(--line);border-radius:6px;padding:40px 20px;
  text-align:center;color:var(--dim)}
.empty button{margin-top:14px}
.jobs{margin-bottom:12px}
.job{border:1px solid var(--line);border-radius:6px;background:var(--panel);
  padding:10px 12px;margin-bottom:8px}
.job .top{display:flex;align-items:center;gap:10px;margin-bottom:8px}
.job .top strong{font-weight:600;font-size:12px}
.steps{display:flex;flex-wrap:wrap;gap:6px}
.step{display:flex;align-items:center;gap:5px;font-size:11px;color:var(--dim);
  border:1px solid var(--line);border-radius:3px;padding:2px 7px;background:#0e1116}
.step.ok{color:var(--ok);border-color:rgba(63,166,107,.35)}
.step.failed{color:var(--bad);border-color:rgba(194,84,80,.35)}
.step.running{color:var(--warn);border-color:rgba(201,144,58,.35)}
.spin{animation:rot 1s linear infinite;transform-origin:center}
@keyframes rot{to{transform:rotate(360deg)}}
.links{display:flex;gap:14px;margin-right:4px}
.links a{color:var(--dim);text-decoration:none;font-size:12px}
.links a:hover{color:var(--accent)}
@media(max-width:820px){.links{display:none}
  main{padding:12px 12px 40px}
  .exit>.row{grid-template-columns:14px auto 1fr auto;
    grid-template-areas:"dot number location acts" ". ip ip ip" ". meta meta meta" ". socks socks socks" ". chips chips chips"}
  .exit .chips{margin-top:2px}
  .bar{flex-wrap:wrap}}
.modal{position:fixed;inset:0;background:rgba(8,10,14,.72);display:none;
  align-items:center;justify-content:center;z-index:50;padding:20px}
.modal.open{display:flex}
.sheet{background:var(--bg);border:1px solid var(--line);border-radius:6px;
  width:min(680px,100%);max-height:86vh;display:flex;flex-direction:column}
.sheet .head{display:flex;align-items:center;gap:10px;padding:10px 14px;
  border-bottom:1px solid var(--line);background:var(--panel);border-radius:6px 6px 0 0}
.sheet .head h2{font-size:12px;margin:0;font-weight:600}
.sheet .body{overflow:auto;padding:14px}
.sheet .foot{display:flex;align-items:center;gap:10px;padding:10px 14px;
  border-top:1px solid var(--line);background:var(--panel);border-radius:0 0 6px 6px}
.count{color:var(--dim);font-size:11px}
label.f{display:block;margin-bottom:16px}
label.f[hidden]{display:none}
label.f>span{display:block;color:var(--dim);font-size:11px;margin-bottom:6px}
.regions{display:grid;grid-template-columns:repeat(auto-fill,minmax(148px,1fr));
  gap:6px;max-height:224px;overflow:auto}
.rg{border:1px solid var(--line);background:#0e1116;border-radius:4px;padding:7px 9px;
  cursor:pointer;text-align:left;display:block;width:100%}
.rg:hover{border-color:var(--accent)}
.rg.sel{border-color:var(--accent);background:rgba(74,158,218,.1)}
.rg b{font-weight:600;font-size:12px;display:block;overflow:hidden;
  text-overflow:ellipsis;white-space:nowrap}
.rg em{display:block;font-style:normal;color:var(--dim);font-size:11px;margin-top:2px}
.stepper{display:flex;align-items:center;gap:0;width:fit-content;
  border:1px solid var(--line);border-radius:4px;overflow:hidden;background:#0e1116}
.stepper button{border:0;border-radius:0;background:transparent;padding:5px 11px}
select,input:not([type]),input[type=email],input[type=url],input[type=search],input[type=text]{font:inherit;background:#0e1116;
  border:1px solid var(--line);color:var(--text);border-radius:4px;
  padding:5px 8px;width:100%}
select:focus,input:not([type]):focus,input[type=email]:focus,input[type=url]:focus,input[type=search]:focus,input[type=text]:focus{outline:none;border-color:var(--accent)}
.stepper input[type=text]{width:56px;text-align:center;font:inherit;background:transparent;
  border:0;border-left:1px solid var(--line);border-right:1px solid var(--line);
  color:var(--text);padding:5px 0;font-variant-numeric:tabular-nums}
.stepper input:focus{outline:none}
.hint{color:var(--dim);font-size:11px;margin-top:6px}
.setrow{display:grid;grid-template-columns:1fr 1fr;gap:12px;margin-top:16px}
.setrow input,.setrow select{width:100%}
.updsec{margin-top:18px;padding-top:14px;border-top:1px solid var(--line)}
.updrow{display:flex;align-items:center;gap:10px}
.updver{font-size:12px;color:var(--text)}
.updver b{font-weight:600}
.updver span{color:var(--dim);margin-left:8px}
.updnotes{margin-top:10px;padding:10px;background:#0e1116;border:1px solid var(--line);
  border-radius:4px;font-size:12px;line-height:1.6;color:var(--dim);white-space:pre-wrap;
  max-height:180px;overflow:auto}
label.chk{display:flex;align-items:center;gap:7px;color:var(--text);font-size:12px;
  cursor:pointer;margin:0}
label.chk input{margin:0}
.hint.bad{color:var(--bad)}
.kv{display:grid;grid-template-columns:76px 1fr;gap:5px 12px;margin:0 0 14px}
.kv dt{color:var(--dim)}
.kv dd{margin:0;word-break:break-all}
.share{padding:10px;background:#0e1116;border:1px solid var(--line);
  border-radius:4px;word-break:break-all;font-size:12px;line-height:1.7;margin-bottom:8px}
.editbar{display:flex;align-items:flex-end;gap:12px;flex-wrap:wrap;
  padding:12px 0;border-top:1px solid var(--line);margin-top:4px}
.ef{display:block}
.ef>span{display:block;color:var(--dim);font-size:11px;margin-bottom:4px}
.ef input{width:150px}
.credrow{display:flex;align-items:flex-end;gap:12px;flex-wrap:wrap;margin-bottom:10px}
.credrow .ef input{width:190px}
.chead{display:flex;align-items:center;gap:10px;margin:14px 0 8px;
  padding-top:12px;border-top:1px solid var(--line)}
.chead h3{font-size:12px;margin:0;font-weight:600;color:var(--dim)}
.client{border:1px solid var(--line);border-radius:4px;padding:8px 10px;margin-bottom:8px}
.orow{display:flex;align-items:center;gap:10px;padding:6px 0}
.orow select{width:200px}
.crow{display:flex;align-items:center;gap:10px}
.cemail{font-weight:600;font-size:12px}
.cid{color:var(--dim);font-size:11px;overflow:hidden;text-overflow:ellipsis;
  white-space:nowrap;max-width:280px}
.client .share{margin:8px 0 0}
.share button{margin-top:8px}
textarea{width:100%;min-height:300px;background:#0e1116;border:1px solid var(--line);
  color:var(--text);border-radius:4px;
  font:12px/1.8 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;
  padding:10px 12px;resize:vertical}
textarea:focus{outline:none;border-color:var(--accent)}
.toast{position:fixed;left:50%;bottom:24px;transform:translateX(-50%);
  background:var(--panel);border:1px solid var(--line);border-radius:4px;
  padding:8px 14px;font-size:12px;z-index:80;opacity:0;pointer-events:none;
  transition:opacity .18s}
.toast.show{opacity:1}
.toast.bad{border-color:rgba(194,84,80,.5);color:var(--bad)}

body{background:radial-gradient(ellipse at 75% 0%,rgba(57,197,187,.1),transparent 45%),var(--bg)}
header{max-width:100%;position:sticky;top:0;z-index:20;padding:12px 22px;backdrop-filter:blur(18px);background:rgba(18,33,38,.94)}
.brand-avatar{width:48px;height:52px;object-fit:cover;object-position:center 12%;border-radius:14px;border:1px solid rgba(57,197,187,.3)}
.brand-name h1{font-size:19px;font-weight:800;letter-spacing:2px}.brand-name h1 span{color:var(--accent);font-weight:500}.brand-name small{color:var(--dim);font-size:10px;letter-spacing:1px}
header .links{font-size:11px}header #panel{max-width:32%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
button,input,select,textarea{border-radius:8px}button{padding:7px 12px}h2,.bar h2{font-size:15px}.sheet{border-radius:18px}.exit,.orphan{border-radius:12px}.exit>.row{padding:13px 14px}.share,.addr{font-family:ui-monospace,monospace}
.miku-welcome{border:1px solid var(--line);border-radius:18px;padding:24px 28px;margin-bottom:24px;background:linear-gradient(110deg,#142b30,#142128);position:relative;overflow:hidden;min-height:150px}.miku-welcome h2{font-size:24px;color:var(--text);margin:6px 0}.miku-welcome p{color:var(--dim);margin:8px 0}.miku-welcome .eyebrow{font-size:10px;letter-spacing:3px;color:var(--accent)}.miku-welcome img{position:absolute;height:320px;right:25px;top:-10px;opacity:.85;pointer-events:none}.miku-welcome>div{position:relative;z-index:1;max-width:75%}
.ip-summary{display:flex;gap:9px;align-items:center;flex-wrap:wrap;padding:8px 14px 12px;border-top:1px solid rgba(40,64,71,.4);color:var(--dim);font-size:12px}.ip-summary strong{color:var(--accent)}.ip-summary button{margin-left:auto;font-size:11px;padding:3px 8px}.ipbadge{background:rgba(57,197,187,.08);border:1px solid rgba(57,197,187,.18);padding:2px 8px;border-radius:6px}
.exit-number{font-weight:700;color:var(--accent);font-variant-numeric:tabular-nums}.exit-location{font-weight:600;font-size:12px;color:var(--text);max-width:260px;overflow-wrap:anywhere}.exit-proxies{display:flex;gap:8px;align-items:center;flex-wrap:wrap;padding:0 14px 12px}.exit-proxies button,.exit-proxies a{font-size:11px;padding:5px 9px}.exit-proxies a{color:var(--accent);text-decoration:none;margin-left:auto}.ip-summary .ip-label{color:var(--dim)}#setSources{min-height:88px;height:100px;max-height:220px;resize:vertical}.f input[type=password]{width:100%;background:#0e1116;color:var(--text);border:1px solid var(--line);padding:10px;font:inherit}.ip-details-link{display:inline-block;color:var(--accent);margin-top:12px}
.source-list{font-size:11px;margin:12px 0}.source-list>div{display:flex;align-items:center;gap:8px;padding:5px 0;border-bottom:1px solid var(--line)}.source-list span{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis}.source-list b{font-weight:400;color:var(--dim);white-space:nowrap}.source-list button{flex:none;width:30px;padding:3px 5px}#nimport{min-height:90px;max-height:240px;resize:vertical}summary{cursor:pointer;color:var(--dim)}
.domain-result{border:1px solid var(--accent);border-radius:12px;padding:16px;margin:12px 0;background:rgba(57,197,187,.08)}.domain-login-link{display:grid;gap:9px;margin-top:10px}.domain-login-link a{color:var(--accent);overflow-wrap:anywhere;line-height:1.6}.domain-login-link button{justify-self:start}.entry-chip .entry-stack{display:block;font-size:10px;font-weight:650;line-height:1.3;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}.entry-chip small{display:block;font-size:9px;line-height:1.2;opacity:.78;margin:0}.chips .entry-chip,.orow>.entry-chip{display:inline-grid;gap:1px;min-height:32px;min-width:0;max-width:188px;padding:3px 6px;text-align:left;border-radius:5px}.orphan{padding:12px}.orow{padding:4px 0}.node-card-protocol{white-space:normal;overflow-wrap:anywhere}
.domain-card{border:1px solid var(--line);border-radius:10px;padding:13px;margin:10px 0}.domain-card strong{color:var(--accent)}.domain-card small{display:block;color:var(--dim);margin:4px 0}.domain-card .domain-actions{display:flex;gap:8px;flex-wrap:wrap;margin-top:8px}.domain-current{padding:12px;background:rgba(57,197,187,.07);border-radius:10px;overflow-wrap:anywhere}.kv dd{overflow-wrap:anywhere}
@media(max-width:760px){header{padding:9px 12px;gap:8px;flex-wrap:wrap}header #panel{display:none}header .links{display:none}.brand-name h1{font-size:16px}.brand-avatar{width:36px;height:40px}.miku-welcome{padding:20px;min-height:125px}.miku-welcome h2{font-size:19px}.miku-welcome img{right:-50px;height:260px;opacity:.45}.miku-welcome>div{max-width:90%}main{padding:12px}.bar{flex-wrap:wrap}.sheet{max-height:92vh}.ip-summary{font-size:11px}}
` + showcaseCSS + workspaceCSS + appearanceCSS + `
</style>
</head>
<body>
<header>
  <img class="brand-avatar" src="assets/miku-hero-v1.png" alt="Miku"><div class="brand-name"><h1>MIKU <span>GATE</span></h1><small>你的连接，你的节奏</small></div>
  <span class="count" id="panel" hidden></span>
  <span class="spacer"></span><div class="header-actions"><button id="domainBtn">域名 / 证书</button><button id="ipBtn">IP 查询</button>` + themeButtonHTML + `
  <button id="settingsBtn" title="设置">
    <svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
    设置
  </button>
  </div>
</header>

<main>
  <div id="managementMoved" class="domain-result" role="status" aria-live="polite" hidden></div>
  <div class="miku-welcome"><div><span class="eyebrow">连接工作台</span><h2>世界，等你连接。</h2><p id="mikuGreeting">准备好了吗？和我一起，把下一站点亮吧！</p></div><img src="assets/miku-hero-v1.png" alt="" aria-hidden="true"></div><div class="jobs" id="jobs"></div>

  <section class="bar" id="entranceBar"><h2>入口节点</h2><span class="count">自动检测面板；同页管理入口与落地</span><span class="spacer"></span><button onclick="document.querySelector('#newnode').click()">新建入口</button><button onclick="document.querySelector('#settingsBtn').click()">核心 / 面板 / 域名</button></section>
  <div id="orphans"></div>
  <div class="bar exit-toolbar">
    <div class="toolbar-title"><h2>出口与节点</h2><span class="count" id="ecount"></span></div>
    <div class="toolbar-actions">
    <button class="view-switch" id="displayModeBtn" aria-pressed="false">展示模式</button>
    <button id="subBtn" title="拿订阅地址">
      <svg viewBox="0 0 24 24"><path d="M4 11a9 9 0 0 1 9 9"/><path d="M4 4a16 16 0 0 1 16 16"/><circle cx="5" cy="19" r="1"/></svg>
      订阅
    </button>
    <button id="exportAll" title="导出全部节点链接">
      <svg viewBox="0 0 24 24"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="M7 10l5 5 5-5"/><path d="M12 15V3"/></svg>
      导出链接
    </button>
    <button id="stopall" title="停止所有出口">
      <svg viewBox="0 0 24 24"><rect x="6" y="6" width="12" height="12" rx="1"/></svg>
      全部停止
    </button>
    <button id="newnode" title="新建一个节点（协议与端口）">
      <svg viewBox="0 0 24 24"><path d="M4 7h16"/><path d="M4 12h16"/><path d="M4 17h10"/></svg>
      新建节点
    </button>
    <button class="primary" id="newexit">
      <svg viewBox="0 0 24 24"><path d="M12 5v14"/><path d="M5 12h14"/></svg>
      新建出口
    </button>
    </div>
  </div>

  <div id="list"></div>


</main>

<div class="modal" id="wizard">
  <div class="sheet">
    <div class="head">
      <h2>新建出口</h2>
      <span class="spacer"></span>
      <button class="icon" data-close="wizard" title="关闭">
        <svg viewBox="0 0 24 24"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>
      </button>
    </div>
    <div class="body">
      <label class="f">
        <span>地区</span>
        <input type="search" id="rgfilter" placeholder="筛选地区">
        <div class="regions" id="regions" style="margin-top:6px"></div>
      </label>
      <label class="f">
        <span id="countlabel">数量</span>
        <div class="stepper">
          <button id="minus" title="减少">
            <svg viewBox="0 0 24 24"><path d="M5 12h14"/></svg>
          </button>
          <input id="count" type="text" inputmode="numeric" value="3">
          <button id="plus" title="增加">
            <svg viewBox="0 0 24 24"><path d="M12 5v14"/><path d="M5 12h14"/></svg>
          </button>
        </div>
        <div class="hint" id="availhint"></div>
      </label>
      <details style="margin-top:12px"><summary>添加 VPN Gate 来源链接</summary><label class="f"><span>OpenVPN 节点清单（CSV）</span><input type="text" id="sourceQuickURL" placeholder="粘贴公网 CSV 链接"></label><button id="sourceQuickAdd">确认加入</button><div class="hint" id="sourceQuickHint"></div></details>
      <label class="f" id="tplwrap">
        <span>节点链接</span>
        <select id="tpl"></select>
        <div class="hint" id="tplhint"></div>
      </label>
    </div>
    <div class="foot">
      <span class="count" id="wzhint"></span>
      <span class="spacer"></span>
      <button data-close="wizard">取消</button>
      <button class="primary" id="go">开始</button>
    </div>
  </div>
</div>

<div class="modal" id="newnodebox">
  <div class="sheet">
    <div class="head">
      <h2>新建节点</h2>
      <span class="spacer"></span>
      <button class="icon" data-close="newnodebox" title="关闭">
        <svg viewBox="0 0 24 24"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>
      </button>
    </div>
    <div class="body">
      <details style="margin-bottom:16px"><summary>粘贴本 VPS 已生成的节点链接</summary><label class="f"><span>分享链接（每行一个）</span><textarea id="nimport" rows="3" placeholder="vless://… 或 vmess://…、trojan://…、socks5://…、http://…"></textarea></label><div class="hint">核对本 VPS 的发布地址、监听端口和凭据；识别现有入口后可用于批量创建出口。</div><button id="nimportConfirm">确认识别</button><div class="hint" id="nimportHint"></div></details>

      <label class="f">
        <span>协议</span>
        <select id="nproto">
          <option value="vless">VLESS</option>
          <option value="vmess">VMess</option>
          <option value="trojan">Trojan</option>
        </select>
      </label>
      <label class="f">
        <span>传输</span>
        <select id="nnet">
          <option value="tcp">TCP</option>
          <option value="ws">WebSocket</option>
          <option value="grpc">gRPC</option>
          <option value="httpupgrade">HTTPUpgrade</option>
          <option value="xhttp">XHTTP</option>
        </select>
      </label>
      <label class="f">
        <span>安全</span>
        <select id="nsec">
          <option value="none">无</option>
          <option value="tls">TLS</option>
          <option value="reality">REALITY</option>
        </select>
        <div class="hint" id="nsechint"></div>
      </label>
      <label class="f" id="nvisionwrap" hidden>
        <span>流控</span>
        <label class="chk"><input type="checkbox" id="nvision"> xtls-rprx-vision</label>
      </label>
      <label class="f" id="ncertificatewrap" hidden><span>TLS 域名与证书</span><select id="ncertificate"><option value="auto">自动读取服务器证书</option></select><div class="hint" id="ncertificatehint"></div><button type="button" onclick="openDomains()">绑定 / 管理域名</button></label><label class="f" id="nsniwrap" hidden>
        <span>域名 SNI</span>
        <input id="nsni" type="text" placeholder="留空用 localhost，将生成自签证书">
      </label>
      <label class="f" id="ncertwrap" hidden>
        <span>证书路径</span>
        <input id="ncert" type="text" placeholder="留空生成自签证书，如 /etc/ssl/x.crt">
      </label>
      <label class="f" id="nkeywrap" hidden>
        <span>私钥路径</span>
        <input id="nkey" type="text" placeholder="与证书成对填写，如 /etc/ssl/x.key">
      </label>
      <label class="f" id="ndestwrap" hidden>
        <span>借用站点</span>
        <input id="ndest" type="text" placeholder="留空用 www.tesla.com:443">
      </label>
      <label class="f" id="npathwrap" hidden>
        <span id="npathlabel">路径</span>
        <input id="npath" type="text" placeholder="留空自动生成">
      </label>
      <label class="f">
        <span>端口</span>
        <input id="nport" type="text" inputmode="numeric" placeholder="留空随机分配">
      </label>
      <label class="f">
        <span>备注</span>
        <input id="nremark" type="text" placeholder="留空自动命名">
      </label>
    </div>
    <div class="foot">
      <span class="count" id="nnhint"></span>
      <span class="spacer"></span>
      <button data-close="newnodebox">取消</button>
      <button class="primary" id="ncreate">创建</button>
    </div>
  </div>
</div>

<div class="modal" id="detail">
  <div class="sheet">
    <div class="head">
      <h2 id="dtitle">节点</h2>
      <span class="spacer"></span>
      <button class="icon danger" id="ddel" title="删除这个入站">
        <svg viewBox="0 0 24 24"><path d="M3 6h18"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
      </button>
      <button class="icon" data-close="detail" title="关闭">
        <svg viewBox="0 0 24 24"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>
      </button>
    </div>
    <div class="body" id="dbody"></div>
  </div>
</div>

<div class="modal" id="credbox">
  <div class="sheet">
    <div class="head">
      <h2>SOCKS5 / HTTP 访问凭据</h2>
      <span class="count" id="crtitle"></span>
      <span class="spacer"></span>
      <button class="icon" data-close="credbox" title="关闭">
        <svg viewBox="0 0 24 24"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>
      </button>
    </div>
    <div class="body">
      <div class="share" id="crurl"></div><div class="share" id="crhttpurl"></div><button id="crhttpcopy">复制 HTTP</button>
      <div class="credrow">
        <label class="ef"><span>用户名</span>
          <input id="cruser" type="text" spellcheck="false"></label>
        <label class="ef"><span>口令</span>
          <input id="crpass" type="text" spellcheck="false"></label>
        <button id="crrand" title="随机生成一套">
          <svg viewBox="0 0 24 24"><path d="M21 12a9 9 0 1 1-3-6.7L21 8"/><path d="M21 3v5h-5"/></svg>
          随机
        </button>
      </div>
      <div class="hint">改完立即生效，已连上的会话不断；用旧凭据的客户端要改配置。</div>
    </div>
    <div class="foot">
      <button id="crcopy">
        <svg viewBox="0 0 24 24"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
        复制 SOCKS5
      </button>
      <span class="spacer"></span>
      <button data-close="credbox">取消</button>
      <button class="primary" id="crsave">保存</button>
    </div>
  </div>
</div>

<div class="modal" id="export">
  <div class="sheet">
    <div class="head">
      <h2>节点链接</h2>
      <span class="count" id="excount"></span>
      <span class="spacer"></span>
      <button id="copyall">
        <svg viewBox="0 0 24 24"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
        全部复制
      </button>
      <button class="icon" data-close="export" title="关闭">
        <svg viewBox="0 0 24 24"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>
      </button>
    </div>
    <div class="body"><textarea id="exbox" spellcheck="false" readonly></textarea></div>
  </div>
</div>

<div class="modal" id="subbox">
  <div class="sheet">
    <div class="head">
      <h2>订阅</h2>
      <span class="count" id="subcount"></span>
      <span class="spacer"></span>
      <button class="icon" data-close="subbox" title="关闭">
        <svg viewBox="0 0 24 24"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>
      </button>
    </div>
    <div class="body">
      <label class="f"><span>客户端格式</span><select id="subformat"><option value="base64">通用（Base64）</option><option value="v2ray">V2Ray / v2rayN（Base64）</option><option value="clash">Clash / Mihomo（YAML）</option><option value="singbox">sing-box（JSON）</option><option value="links">明文分享链接</option></select></label>
      <label class="f"><span>订阅地址</span>
        <input id="suburl" type="text" spellcheck="false" readonly></label>
      <div class="hint">客户端里新建订阅填这条。以后加出口、删出口都会自己跟上，不用重新配。</div>
      <div class="hint bad">地址最后那串口令等于密码，别发群里。</div>
      <div class="hint" id="subFormatHint">选择客户端格式后复制地址即可。REALITY 需支持它的客户端；sing-box 自签证书指纹需 1.15+。</div>
    </div>
    <div class="foot">
      <span class="spacer"></span>
      <button id="subreset">换一串口令</button>
      <button class="primary" id="subcopy">复制地址</button>
    </div>
  </div>
</div>

<div class="modal" id="settings">
  <div class="sheet">
    <div class="head">
      <h2>设置</h2>
      <span class="spacer"></span>
      <button class="icon" data-close="settings" title="关闭">
        <svg viewBox="0 0 24 24"><path d="M18 6 6 18"/><path d="m6 6 12 12"/></svg>
      </button>
    </div>
    <div class="body">
      <label class="f"><span>访问口令</span>
        <input id="setPw" type="password" spellcheck="false" autocomplete="new-password" placeholder="留空则不改"></label>
      <div class="hint">改完只影响新登录，当前这个浏览器不会被踢下线。</div>

      <label class="f" style="margin-top:16px"><span>访问路径</span>
        <input id="setPath" type="text" spellcheck="false" placeholder="留空则去掉路径前缀"></label>
      <div class="hint" id="setPathHint">界面挂在这个路径下，扫端口的探不到。只能用字母数字和 - _。</div>

      <label class="f" style="margin-top:16px"><span>节点后端</span>
        <select id="setBackend"></select></label>
      <div class="hint" id="setBackendHint">节点从哪来。装了 3x-ui 或 xray-cf-lite 就能直接接管，都没有就用自建。</div>

      <label class="chk" style="margin-top:16px"><input type="checkbox" id="setResi"> 排除 VPN Gate 自营公共服务器</label>
      <div class="hint" id="setResiHint">默认包含全部来源；此选项只按主机名及已知网段过滤自营节点，不能证明其他节点是住宅IP。</div>

      <div class="setrow">
        <label class="f" style="margin:0"><span>监听端口</span>
          <input id="setPort" type="text" inputmode="numeric" spellcheck="false"></label>
        <label class="f" style="margin:0"><span>本地监听地址</span>
          <select id="setListen">
            <option value="0.0.0.0">所有网卡（0.0.0.0）</option>
            <option value="127.0.0.1">仅本机（127.0.0.1）</option>
          </select></label>
      </div>
      <div class="hint bad" id="setPortHint">改端口或监听地址会切换监听，保存后要用新地址重新打开界面。</div>

      <label class="f"><span>添加 VPN Gate 来源</span><input type="text" id="sourceAddURL" placeholder="粘贴公网 OpenVPN CSV 链接"></label><button id="sourceAddConfirm">确认加入</button><div class="hint" id="sourceAddHint"></div><textarea id="setSources" hidden></textarea><div class="hint">用 ↑ ↓ 调整优先顺序，立即保存并用于选节点。默认优先近期检测与开源快照，官方在最后兜底。</div><div class="source-list" id="sourceStatus"></div><label class="f"><span>ipapi.is API Key（可选，仅保存在服务器）</span><input id="setIPKey" type="password" autocomplete="off" placeholder="留空保持现有设置"></label><div class="hint" id="ipKeyHint">不填也能检测住宅 / 机房 / 移动类型；优先IP-API，必要时使用ipapi.is和ipwho.is复核。填Key可获得更完整的类型字段。</div><label class="f"><span>节点发布地址（可后改）</span><input id="setPublicHost" placeholder="留空用VPS IP，或填已解析的域名"></label><div class="hint"><button type="button" onclick="openDomains()">在网页绑定域名与证书</button> 没有域名先HTTP+IP；忘记口令可运行 <code>miku password</code>。</div>
      <div class="updsec">
        <div class="updrow">
          <div class="updver">版本 <b id="updCur">-</b><span id="updLatest"></span></div>
          <span class="spacer"></span>
          <button id="updCheck">检查更新</button>
          <button class="primary" id="updApply" hidden>更新到 <span id="updApplyVer"></span></button>
        </div>
        <div class="updnotes" id="updNotes" hidden></div>
      </div>
    </div>
    <div class="foot">
      <span class="spacer"></span>
      <button data-close="settings">取消</button>
      <button class="primary" id="setSave">保存</button>
    </div>
  </div>
</div>


<div class="modal" id="domainbox"><div class="sheet"><div class="head"><h2>域名与证书</h2><span class="spacer"></span><button class="icon" data-close="domainbox">关闭</button></div><div class="body"><div class="domain-current" id="domainCurrent"></div><div class="domain-result" id="domainResult" role="status" aria-live="polite" hidden></div><div id="domainList"></div><label class="f"><span>绑定域名</span><input id="domainName" placeholder="node.example.com" autocomplete="off"></label><label class="f"><span>绑定方式 / 已有证书</span><select id="domainCert"><option value="">申请新的证书</option></select></label><label class="chk"><input id="domainManagement" type="checkbox" checked> 同时更新管理登录地址</label><div class="hint">填写域名即可自动复用或申请证书，无需邮箱。申请 HTTPS 证书前请解析到 VPS 并放行 80 端口；取消勾选时仅供 TLS 入口使用。</div><div class="hint" id="domainHint"></div></div><div class="foot"><button id="domainUnbind">管理地址解绑回IP</button><button id="domainRefresh">重新扫描证书</button><span class="spacer"></span><button class="primary" id="domainSubmit">绑定域名</button></div></div></div>
<div class="modal" id="ipbox"><div class="sheet"><div class="head"><h2>IP / ASN 信息</h2><span class="spacer"></span><button class="icon" data-close="ipbox">关闭</button></div><div class="body"><div class="setrow"><label class="f"><span>公网IP</span><input id="ipAddress" placeholder="IPv4 / IPv6 地址"></label><button class="primary" id="ipLookup">查询</button></div><div id="ipDetails" class="domain-card">输入IP，或点击出口上的“查看IP”。</div><div class="hint">显示来源、时间、网络类型、判断依据与置信程度。结合机房 / 移动标记和运营商信息分类；检测失败会自动重试。</div></div></div></div>
<div class="toast" id="toast"></div>

<script>
` + themeControlsJS + `
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
const greetings=['准备好了吗？和我一起，把下一站点亮吧！','让我把小小的心跳，唱到更远的地方。','不管相隔多远，今天也想听见你的声音。'];
$('#mikuGreeting').textContent=greetings[Math.floor(Math.random()*greetings.length)];

// 自建模式下入站由 fanout 自己管，界面要提供新建入口；
// 接管 3x-ui 时入站归面板管，这里只读不写。
function isNative(){ return view.backend === 'native' || view.backend === 'sing-box'; }
// xray-cf-lite 模式下节点归它管，fanout 只改路由，界面不给新建入口
function isXCL(){ return view.backend === 'xray-cf-lite'; }
const BACKEND_NAME = {'native':'自建 Xray', 'sing-box':'自建 sing-box', 's-ui':'S-UI', '3x-ui':'3x-ui', 'xray-cf-lite':'xray-cf-lite'};
function backendName(){ return BACKEND_NAME[view.backend] || '3x-ui'; }

const STATUS = {up:'已连通', starting:'连接中', failed:'失败', stopped:'已停止'};

` + latencyJS + showcaseJS + `

function entranceLabel(i){ return i.stack || String(i.protocol || "入口").toUpperCase(); }
function entranceChip(i){ return '<span class="entry-stack">'+esc(entranceLabel(i))+'</span><small>#'+i.id+' · :'+i.port+'</small>'; }

function renderExits(){
  setShowcaseMode();
  const list = $('#list');
  const n = view.exits.length;
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
  list.innerHTML = view.exits.map(e => {
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
    const place = esc(e.country || e.region || '—');
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

function renderJobs(jobs){
  syncDomainJob(jobs);
  const box = $('#jobs');
  box.innerHTML = jobs.map(j => {
    const steps = j.steps.map(s => {
      const ic = {ok:ICON.ok, failed:ICON.bad, running:ICON.run}[s.status] || ICON.wait;
      const t = s.detail ? s.label + ' — ' + s.detail : s.label;
      return '<span class="step ' + s.status + '" title="' + esc(t) + '">' + ic
        + esc(s.status === 'ok' && s.detail ? s.detail : s.label) + '</span>';
    }).join('');
    const close = j.status === 'running' ? ''
      : '<button class="icon" data-job="' + esc(j.id) + '" title="关闭">' + ICON.x + '</button>';
    return '<div class="job"><div class="top"><strong>' + esc(j.summary) + '</strong>'
      + '<span class="count">' + j.done + '/' + j.total + '</span>'
      + '<span class="spacer"></span>' + close + '</div>'
      + '<div class="steps">' + steps + '</div>' + (j.result_url ? domainLink(j.result_url,'新的管理登录地址') : '') + '</div>';
  }).join('');
}

async function poll(){
  if(managementMovedURL) return;
  try{
    view = await api('/api/exits');
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
  }catch(e){}
  if(managementMovedURL) return;
  try{ renderJobs(await api('/api/jobs') || []); }catch(e){}
}

// ---- 新建向导 ----
let regions = [], region = '', regionsLoaded = false;

function openModal(id){ $('#' + id).classList.add('open'); }
function closeModal(id){ $('#' + id).classList.remove('open'); }

document.addEventListener('click', e => {
  const c = e.target.closest('[data-close]');
  if(c) closeModal(c.dataset.close);
});
document.addEventListener('keydown', e => {
  if(e.key === 'Escape') document.querySelectorAll('.modal.open')
    .forEach(m => m.classList.remove('open'));
});
document.querySelectorAll('.modal').forEach(m => {
  m.onclick = e => { if(e.target === m) m.classList.remove('open'); };
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
      + '" data-rg="' + esc(r.code) + '"><b>' + esc(r.name || r.code) + '</b>'
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
  if(!confirm('停止全部 ' + view.exits.length + ' 个出口？')) return;
  e.target.disabled = true;
  for(const x of view.exits){
    try{ await api('/api/stop?slot=' + x.slot, {method:'POST'}); }catch(err){}
  }
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
  const up = view.exits.filter(e => e.status === 'up');
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
      + (isNative() ? '<div class="orow"><span>此客户端落地</span><select class="client-landing" data-email="' + esc(c.email) + '">' + exitOptions((view.exits.find(e => e.host.replace(/[^A-Za-z0-9_-]/g,'-') === c.bound_to) || {}).host || '').replace('直连（不走隧道）','跟随入口默认落地') + '</select></div>' : '')
      + (link ? '<div class="share">' + esc(link) + '</div>' : '')
      + '</div>';
  }).join('');

  $('#dtitle').textContent = (d.remark || '节点') + '　:' + d.port;
  // xray-cf-lite 的节点归它自己管，这里只留出口选择，改端口/备注/客户端都不给
  const editable = !isXCL();
  $('#dbody').innerHTML = '<dl class="kv">'
    + '<dt>出口</dt><dd><select id="dbind" data-tag="' + esc(d.tag) + '">'
    +   exitOptions(owner ? owner.host : '') + '</select></dd>'
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
  try { await api('/api/panel/client/bind?id=' + curDetail.id + '&email=' + encodeURIComponent(select.dataset.email) + '&host=' + encodeURIComponent(select.value), {method:'POST'}); toast('此凭据已绑定落地，共用原入口端口'); await openDetail(curDetail.id); } catch(error) { toast(error.message,true); } finally { select.disabled=false; }
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
  }catch(err){ toast(err.message, true); }
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

$('#crcopy').onclick = () => { copy($('#crurl').textContent); };
$('#crhttpcopy').onclick = () => copy($('#crhttpurl').textContent);

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
  const ids = view.exits.flatMap(x => (x.inbounds || []).map(i => i.id));
  if(!ids.length){ toast('还没有节点可导出', true); return; }
  $('#exbox').value = '读取中…';
  $('#excount').textContent = '';
  openModal('export');
  try{
    const d = await api('/api/xui/links?ids=' + ids.join(','));
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
    $('#setPathHint').textContent = '界面挂在这个路径下，扫端口的探不到。只能用字母数字和 - _。';
    $('#updCur').textContent = s.version || '-';
    $('#updLatest').textContent = '';
    $('#updNotes').hidden = true;
    $('#updApply').hidden = true;
    $('#updCheck').disabled = false;
    $('#updCheck').textContent = '检查更新';
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
  const port = parseInt($('#setPort').value.trim(), 10);
  if(port) body.port = port;
  body.listen_addr = $('#setListen').value;
  body.residential_only = $('#setResi').checked;
body.vpngate_sources = $('#setSources').value.split(/\r?\n/).map(v => v.trim()).filter(Boolean);
if($('#setIPKey').value.trim()) body.ip_info_key = $('#setIPKey').value.trim();
  body.public_host = $('#setPublicHost').value.trim();

  const portChanged = curSettings && (port !== curSettings.port
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
    if(portChanged){
      const url = nextURL(port, body.listen_addr, body.base_path);
      $('#setPortHint').innerHTML = '监听已切换，请从新地址打开：<a href="' + esc(url) + '">' + esc(url) + '</a>';
      toast('监听已切换，用新地址重新打开');
      // 端口变了当前连接会断，不自动跳转，让用户看清新地址
    } else {
      toast('已保存');
      // 路径可能变了，重新加载到新路径下
      const np = body.base_path;
      const cur = (curSettings && curSettings.base_path || '').replace(/^\//, '');
      if(np !== cur){
        location.href = location.protocol + '//' + location.host
          + (np ? '/' + np : '') + '/';
        return;
      }
      closeModal('settings');
      poll();
    }
  }catch(err){ toast(err.message, true); }
  e.target.disabled = false;
};

let domainData = {certificates:[], registered:[]}, domainPending = null, managementMovedURL='';
function managementMoved(url){
  let address;try{address=new URL(url,location.href);}catch(e){return;}
  if(!['http:','https:'].includes(address.protocol)) return;
  managementMovedURL=address.href;
  domainPending=null;
  const box=$('#managementMoved');box.hidden=false;
  box.innerHTML=domainLink(address.href,'管理地址已更新')+'<p class="hint">请打开新地址登录。当前页面已暂停自动刷新。</p>';
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
    $('#domainCurrent').innerHTML = '<strong>当前管理地址</strong><div>' + esc(domainData.management_url || location.href) + '</div>';
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
function boolLabel(value){return value === true ? '是' : value === false ? '否' : '来源未提供';}
function renderIPInfo(info){
  const items=[['IP',info.ip],['IP位置',info.location || [info.country,info.city].filter(Boolean).join(' / ')],['ASN',info.asn ? 'AS'+info.asn : '未知'],['ASN归属',info.organization],['ASN类型',info.asn_type],['IP归属类型',info.company_type],['分类依据',info.type_reason],['分类置信程度',({high:'高',medium:'中（规则推断）',low:'低（待复核）'})[info.type_confidence]],['类型来源',info.type_source || info.source],['运营商 ISP',info.isp],['IP归属商',info.company],['网络',info.network],['地址类型',info.family],['IP属性',info.attribute],['机房',boolLabel(info.hosting)],['移动网络',boolLabel(info.mobile)],['代理标记',boolLabel(info.proxy)],['VPN标记',boolLabel(info.vpn)],['Tor标记',boolLabel(info.tor)],['定位来源',info.geo_source || info.source],['ASN来源',info.asn_source || info.source],['属性来源',info.source],['属性查询时间',info.checked && !info.checked.startsWith('0001-') ? new Date(info.checked).toLocaleString() : '查询中'],['定位查询时间',info.geo_checked && !info.geo_checked.startsWith('0001-') ? new Date(info.geo_checked).toLocaleString() : '']];
  $('#ipDetails').innerHTML='<dl class="kv">'+items.map(item=>'<dt>'+esc(item[0])+'</dt><dd>'+esc(item[1] || '未知')+'</dd>').join('')+'</dl>'+(info.error ? '<p class="hint">'+esc(info.error)+'</p>' : '')+(info.ip ? '<a class="ip-details-link" href="https://ping0.cc/ip/'+encodeURIComponent(info.ip)+'" target="_blank" rel="noopener noreferrer">在Ping0检测这个IP ↗</a>' : '');
}
async function lookupIP(){
  const sequence=++ipLookupSequence,ip=$('#ipAddress').value.trim();if(!ip)return;
  $('#ipDetails').textContent='查询中…';
  for(let i=0;i<25;i++){
    try{const result=await api('/api/ipinfo?ip='+encodeURIComponent(ip));if(sequence!==ipLookupSequence)return;if(!result.pending){renderIPInfo(result);return;}}
    catch(err){if(sequence===ipLookupSequence)$('#ipDetails').textContent=err.message;return;}
    await new Promise(resolve=>setTimeout(resolve,2000));
  }
  $('#ipDetails').textContent='查询任务仍在排队，可以稍后再查看。';
}
$('#ipBtn').onclick=()=>openModal('ipbox');$('#ipLookup').onclick=lookupIP;
document.addEventListener('click',event=>{const button=event.target.closest('[data-ip]');if(button){$('#ipAddress').value=button.dataset.ip;openModal('ipbox');lookupIP();}});

poll();
measureBrowserPing();
setInterval(measureBrowserPing, BROWSER_LATENCY_INTERVAL);
setInterval(poll, 3000);
</script>
</body>
</html>`
