package main

// Apply the saved theme before the first paint on both pages.
const themeInitJS = `
window.MikuTheme=(function(){
  const root=document.documentElement,key='miku.theme';
  function apply(value){
    const light=value==='light',mode=light?'light':'dark',label=light?'夜间模式':'白天模式';
    root.setAttribute('data-theme',mode);root.style.colorScheme=mode;
    const meta=document.querySelector('meta[name="theme-color"]');
    if(meta) meta.setAttribute('content',light?'#f3f7fa':'#0a1620');
    document.querySelectorAll('[data-theme-toggle]').forEach(button=>{
      button.setAttribute('aria-pressed',String(light));
      button.setAttribute('aria-label','切换到'+label);button.setAttribute('title','切换到'+label);
      button.querySelector('[data-theme-label]').textContent=label;
    });
  }
  let saved='dark';try{saved=localStorage.getItem(key);}catch(e){}
  apply(saved);
  function toggle(){
    const mode=root.getAttribute('data-theme')==='light'?'dark':'light';apply(mode);
    try{localStorage.setItem(key,mode);}catch(e){}
  }
  window.addEventListener('storage',event=>{if(event.key===key || event.key===null) apply(event.newValue);});
  return {bind:function(){
    document.querySelectorAll('[data-theme-toggle]').forEach(button=>button.addEventListener('click',toggle));
    apply(root.getAttribute('data-theme'));
  }};
})();
`

const themeControlsJS = `window.MikuTheme.bind();`

const themeButtonHTML = `<button type="button" class="theme-toggle" data-theme-toggle aria-pressed="false" aria-label="切换到白天模式" title="切换到白天模式"><svg class="theme-icon-sun" aria-hidden="true" viewBox="0 0 24 24"><circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M2 12h2m16 0h2M4.9 4.9l1.4 1.4m11.4 11.4 1.4 1.4m0-14.2-1.4 1.4M6.3 17.7l-1.4 1.4"/></svg><svg class="theme-icon-moon" aria-hidden="true" viewBox="0 0 24 24"><path d="M20.9 13A9 9 0 0 1 11 3.1 9 9 0 1 0 20.9 13Z"/></svg><span data-theme-label>白天模式</span></button>`

const appearanceCSS = `
:root{color-scheme:dark}
button.theme-toggle{width:auto;min-height:44px;margin:0;display:inline-flex;align-items:center;justify-content:center;gap:6px;padding:8px 12px;border:1px solid var(--line,#3a5366);border-radius:9px;background:var(--panel,#1c3040);color:var(--text,#eef6ff);font-size:13px;font-weight:600;line-height:1.5;cursor:pointer;white-space:nowrap}
.theme-toggle svg{width:16px;height:16px;stroke:currentColor;fill:none;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round;flex:none}.theme-toggle .theme-icon-moon{display:none}
:root[data-theme=light] .theme-icon-sun{display:none}:root[data-theme=light] .theme-icon-moon{display:inline}
.login-theme{position:fixed;top:20px;right:20px;z-index:3}.login-theme button:focus-visible{outline:2px solid #67e2d8;outline-offset:3px}
:root[data-theme=light]{
  color-scheme:light;
  --bg:#f3f7fa;--panel:#ffffff;--card:#ffffff;--line:#c9d9e4;
  --text:#183447;--dim:#526b7a;--accent:#087f79;--ok:#06795e;--warn:#8a5608;--bad:#b3264d;
  --asn:#174f93;--asn-bg:#eaf2ff;--owner:#805412;
  --residential:#116b48;--residential-bg:#e7f6ed;
  --datacenter:#69439f;--datacenter-bg:#f0eafa;
  --mobile:#805405;--mobile-bg:#fff4d6;
}
:root[data-theme=light] body{color:var(--text);background:var(--bg)}
:root[data-theme=light] ::selection{color:#183447;background:#bbeee2}
:root[data-theme=light] header{background:rgba(249,252,254,.97);border-color:var(--line);box-shadow:0 2px 14px #18344708}
:root[data-theme=light] .brand-avatar{border-color:#91c9c1;background:#e8f6f3}
:root[data-theme=light] .brand-name h1{color:var(--text)}:root[data-theme=light] .brand-name small{color:var(--dim)}
:root[data-theme=light] button{color:var(--text);background:#edf4f8;border-color:#b7ccd9}
:root[data-theme=light] button:hover:not(:disabled){border-color:var(--accent);background:#e2f0ef}
:root[data-theme=light] button.primary{color:#ffffff;background:var(--accent);border-color:var(--accent)}
:root[data-theme=light] button.primary:hover:not(:disabled){background:#066a65}
:root[data-theme=light] button.icon,:root[data-theme=light] .socks button{background:transparent;color:var(--dim);border-color:transparent}
:root[data-theme=light] button.icon:hover:not(:disabled),:root[data-theme=light] .socks button:hover:not(:disabled){color:var(--accent);background:#e7f3f0;border-color:var(--line)}
:root[data-theme=light] button:focus-visible,:root[data-theme=light] input:focus-visible,:root[data-theme=light] select:focus-visible,:root[data-theme=light] textarea:focus-visible,:root[data-theme=light] summary:focus-visible,:root[data-theme=light] a:focus-visible{outline:2px solid var(--accent);outline-offset:3px}
:root[data-theme=light] .miku-welcome{background:#eaf6f4;border-color:#a8d4ce}
:root[data-theme=light] .miku-welcome h2,:root[data-theme=light] .bar h2{color:var(--text)}
:root[data-theme=light] .miku-welcome .eyebrow{color:#0b756e}:root[data-theme=light] .miku-welcome p{color:#415f6b}
:root[data-theme=light] #ecount{color:#076b62;background:#e3f4ef;border-color:#a3cfc2}
:root[data-theme=light] .exit,:root[data-theme=light] .orphan,:root[data-theme=light] .job{background:var(--panel);border-color:var(--line);box-shadow:0 4px 18px #24465c08}
:root[data-theme=light] .exit .ip,:root[data-theme=light] .exit-location,:root[data-theme=light] .orphan .top h3,:root[data-theme=light] .job .top strong{color:var(--text)}
:root[data-theme=light] .exit-number{color:#08766d}:root[data-theme=light] .exit .meta{color:var(--dim)}
:root[data-theme=light] .chip,:root[data-theme=light] .ip-summary button{color:#254e68;background:#edf4f9;border-color:#bacfdf}
:root[data-theme=light] .ip-summary{border-color:var(--line)}
:root[data-theme=light] .ip-summary .summary-latency{color:#345669}:root[data-theme=light] .ip-summary .summary-latency.failed{color:var(--warn)}
:root[data-theme=light] .view-switch[aria-pressed=true]{color:#076b62;background:#e3f4ef;border-color:#79b6a9}
:root[data-theme=light] .toolbar-actions #stopall,:root[data-theme=light] button.danger{color:#9b1f43;background:#fff0f3;border-color:#dca3b3}
:root[data-theme=light] .asn-tag{border-color:#a9c6e9}
:root[data-theme=light] .network-type{color:#496170;background:#edf2f6;border-color:#bdcdd8}
:root[data-theme=light] .network-type.residential{color:var(--residential);background:var(--residential-bg);border-color:#94c9ab}
:root[data-theme=light] .network-type.datacenter{color:var(--datacenter);background:var(--datacenter-bg);border-color:#c7b4e3}
:root[data-theme=light] .network-type.mobile{color:var(--mobile);background:var(--mobile-bg);border-color:#d9bc73}
:root[data-theme=light] .network-type.unavailable{color:var(--bad);background:#fff0f3;border-color:#dca3b3}
:root[data-theme=light] .node-card{box-shadow:0 6px 24px #24465c0b}:root[data-theme=light] .node-card.connected{border-color:#9ecbc4}
:root[data-theme=light] .node-card-ip,:root[data-theme=light] .node-card-facts strong{color:var(--text)}
:root[data-theme=light] .node-card-state{color:var(--dim);background:#edf2f6}
:root[data-theme=light] .node-card-state.up{color:#116b48;background:#e7f6ed}
:root[data-theme=light] .node-card-state.starting{color:var(--warn);background:#fff4d6}
:root[data-theme=light] .node-card-state.failed{color:var(--bad);background:#fff0f3}
:root[data-theme=light] .node-card-location,:root[data-theme=light] .node-card-protocol{color:var(--dim)}
:root[data-theme=light] .node-card-facts .latency-unavailable{color:var(--warn)}:root[data-theme=light] .latency-method{color:#245e86}
:root[data-theme=light] .node-card-actions button,:root[data-theme=light] .node-card-tools button{color:#254e68;background:#edf4f9;border-color:#bacfdf}
:root[data-theme=light] .node-card-actions .copy-node{color:#ffffff;background:var(--accent);border-color:var(--accent)}
:root[data-theme=light] .node-card-actions .copy-node small{color:#e4fffa}
:root[data-theme=light] .node-card-actions .copy-node:hover:not(:disabled){background:#066a65}
:root[data-theme=light] .node-card-actions .switch-ip{color:#593582;background:#eee8fa;border-color:#c3b3de}
:root[data-theme=light] .node-card-tools .stop-exit{color:#9b1f43;background:#fff0f3;border-color:#dca3b3}
:root[data-theme=light] .exit-stability-notice{color:#7a4e0d;background:#fff6e6;border-color:#d2a762}
:root[data-theme=light] .exit-stability-heading span{color:#815108}:root[data-theme=light] .exit-stability-notice p{color:#72501f}
:root[data-theme=light] .modal{background:rgba(28,51,70,.3)}
:root[data-theme=light] .sheet{background:var(--panel);border-color:#b5cad9;box-shadow:0 22px 70px #23445930}
:root[data-theme=light] .sheet .head,:root[data-theme=light] .sheet .foot{background:#f0f6f9}
:root[data-theme=light] .sheet .head h2,:root[data-theme=light] .kv dd{color:var(--text)}
:root[data-theme=light] label.f>span,:root[data-theme=light] .hint,:root[data-theme=light] .kv dt{color:var(--dim)}:root[data-theme=light] .hint.bad{color:var(--bad)}
:root[data-theme=light] input:not([type=checkbox]):not([type=radio]):not([type=range]),:root[data-theme=light] select,:root[data-theme=light] textarea{color:var(--text);background:#ffffff;border-color:#9fb8c9}
:root[data-theme=light] input::placeholder,:root[data-theme=light] textarea::placeholder{color:#617887;opacity:1}
:root[data-theme=light] input,:root[data-theme=light] select{accent-color:var(--accent)}
:root[data-theme=light] .share,:root[data-theme=light] .updnotes,:root[data-theme=light] .rg,:root[data-theme=light] .stepper{color:var(--text);background:#f7fafc;border-color:#bfd1df}
:root[data-theme=light] .rg.sel{color:#075f58;background:#e3f4ef;border-color:var(--accent)}
:root[data-theme=light] .step{background:#f2f6f9}
:root[data-theme=light] .step.ok{color:#116b48;background:#e7f6ed;border-color:#94c9ab}
:root[data-theme=light] .step.running{color:var(--warn);background:#fff4d6;border-color:#d9bc73}
:root[data-theme=light] .step.failed{color:var(--bad);background:#fff0f3;border-color:#dca3b3}
:root[data-theme=light] .domain-card{background:#f6fafc;border-color:#bfd1df}:root[data-theme=light] .domain-card strong{color:#08766d}
:root[data-theme=light] .domain-current{color:var(--text);background:#e8f4f2}
:root[data-theme=light] .toast{color:var(--text);background:#ffffff;box-shadow:0 8px 28px #24465c20}
:root[data-theme=light] .toast.bad{color:var(--bad);border-color:#dca3b3}
:root[data-theme=light] .login-shell{color:var(--text)}
:root[data-theme=light] body:has(.login-shell){background:radial-gradient(ellipse at 65% 35%,#dcefeb 0,#f3f7fa 50%,#f8fafc 100%)}
:root[data-theme=light] .login-shell form{background:rgba(255,255,255,.96);border-color:#b8d2d9;box-shadow:0 18px 55px #315e7017}
:root[data-theme=light] .login-shell .eyebrow,:root[data-theme=light] .login-shell .logo span{color:var(--accent)}
:root[data-theme=light] .login-shell .subtitle,:root[data-theme=light] .login-shell .intro,:root[data-theme=light] .login-shell label,:root[data-theme=light] .login-shell .help,:root[data-theme=light] .login-shell footer{color:var(--dim)}
:root[data-theme=light] .login-shell .help code{color:#116b61}
:root[data-theme=light] .login-shell form button{color:#ffffff;background:var(--accent)}
:root[data-theme=light] .login-shell .err{color:var(--bad)}:root[data-theme=light] .login-shell .signal i{background:var(--accent)}
@media(max-width:620px){.header-actions .theme-toggle{font-size:12px;padding:6px 4px}.theme-toggle svg{width:14px;height:14px}}
@media(max-width:360px){.header-actions .theme-toggle{font-size:11px;gap:3px}.theme-toggle svg{width:12px;height:12px}}
`
