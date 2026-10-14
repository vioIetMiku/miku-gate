package main

// Miku branding stays teal; ownership, network type and actions each have a
// distinct role so connected exits can be scanned without reading every label.
const workspaceCSS = `
:root{
  --bg:#0a1620;--panel:#10212d;--card:#122633;--line:#344e60;
  --text:#eef6ff;--dim:#b5cad8;--accent:#67e2d8;--ok:#67e2d8;--warn:#ffdc91;--bad:#ffb3c4;
  --asn:#acd4ff;--asn-bg:#17324b;--owner:#ffe1a7;
  --residential:#9cffd2;--residential-bg:#173d32;
  --datacenter:#e2ccff;--datacenter-bg:#352944;
  --mobile:#ffe399;--mobile-bg:#40361e;
}
html{scroll-padding-top:92px}
body{background:var(--bg);font-family:"PingFang SC","Noto Sans SC","Microsoft YaHei",system-ui,sans-serif}
main{max-width:1200px;padding:22px 24px 44px}
button{min-height:44px;transition:background-color .16s,border-color .16s,color .16s;background:#1c3040;border-color:#3a5366;color:var(--text);font-weight:550}
button.icon{min-width:44px;min-height:44px}
button.primary{color:#071e23;background:var(--accent);border-color:var(--accent)}
button.primary:hover:not(:disabled){background:#8ef0e6}
button:focus-visible,input:focus-visible,select:focus-visible,textarea:focus-visible,summary:focus-visible,a:focus-visible{outline:2px solid #9af7e9;outline-offset:3px}
header{gap:14px;padding:12px 24px;border-bottom-color:#34515f;background:rgba(10,25,35,.97)}
.brand-avatar{width:44px;height:48px;border-color:#438681;border-radius:12px}
.brand-name h1{font-size:20px;letter-spacing:1.5px;color:#f4fbff}.brand-name h1 span{color:var(--accent);font-weight:700}
.brand-name small{font-size:11px;letter-spacing:.6px;color:#c2d5df}
.header-actions{display:flex;gap:8px;align-items:center;flex-wrap:wrap}.header-actions button{font-size:13px;border-radius:9px}
.miku-welcome{min-height:142px;border-color:#3d6472;border-left:3px solid var(--accent);border-radius:18px;padding:22px 26px;margin-bottom:20px;background:#132a37}
.miku-welcome .eyebrow{font-size:12px;font-weight:650;letter-spacing:.5px;color:#a2d8dc}
.miku-welcome h2{font-size:27px;line-height:1.35;margin:8px 0;color:#f2f9ff}
.miku-welcome p{font-size:14px;line-height:1.6;color:#bfd2df}.miku-welcome img{height:286px;top:-15px;right:16px;opacity:.9}
.miku-welcome>div{max-width:66%}
.bar{gap:12px;margin-bottom:15px}.bar h2{color:#e5f3fc;font-size:16px;font-weight:750;letter-spacing:.2px}
.exit-toolbar{display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:14px;margin:20px 0 16px;padding:0}
.toolbar-title{display:flex;align-items:center;gap:10px;min-width:0}.toolbar-actions{display:flex;align-items:center;gap:8px;flex-wrap:wrap;justify-content:flex-end}
.toolbar-actions button{font-size:12px;padding:8px 11px;border-radius:9px}.toolbar-actions #stopall{color:#ffbeca;border-color:#805367;background:#302332}
#ecount{font-size:12px;font-weight:650;color:#b5efeb;background:#193c42;border:1px solid #3f6b70;border-radius:999px;padding:2px 9px;min-height:25px;display:inline-flex;align-items:center;font-variant-numeric:tabular-nums}
#ecount:empty{display:none}
.exit{background:#11242f;border-color:#34505f;border-radius:14px;margin-bottom:13px;box-shadow:0 5px 16px #0001}
.exit>.row{padding:14px 16px}.exit .ip{font-size:15px;color:#f5fbff}.exit-number{font-size:14px;color:#8af2e5}.exit-location{color:#e1eef8;font-size:13px}
.exit .meta{font-size:12px;color:#aec6d6}.chip{border-color:#425e70;background:#142b3b;color:#d8e9f5;border-radius:6px}
.ip-summary{gap:11px;padding:12px 16px;border-color:#344d5c;align-items:center;font-size:12px}
.ip-summary .network-owner{flex:1 1 260px;min-width:0;gap:9px}.ip-summary .asn-owner{font-size:13px;color:var(--owner)}.ip-summary .summary-latency{color:#d3e5f2;font-size:12px;white-space:normal;overflow-wrap:anywhere}.ip-summary .summary-latency.failed{color:var(--warn)}
.ip-summary button{min-height:44px;font-size:12px;border-color:#416478;background:#1b3446;color:#daefff;padding:5px 10px}
.orphan{padding:16px;margin-top:4px;margin-bottom:18px;background:#112633;border-color:#395869;border-radius:14px}.orphan .top{gap:12px}.orphan .top h3{font-size:13px;font-weight:650;color:#d6e7f3}.orow{gap:12px;flex-wrap:wrap}.orow select{max-width:100%;min-height:40px}.orow>.chip{white-space:normal}
.job{padding:13px 16px;border-color:#345465;background:#102632;border-radius:12px}.job .top strong{font-size:13px;color:#eef8ff}.step{font-size:12px;background:#10212d;border-radius:6px;padding:4px 8px}.step.ok{color:#8aefcd;border-color:#396b5c}.step.running{color:var(--warn);border-color:#8c7039}.step.failed{color:var(--bad);border-color:#82536c}
.sheet{border-color:#456073;overflow:hidden;box-shadow:0 22px 70px #0008}.sheet .head{background:#152c3a;padding:14px 18px}.sheet .head h2{font-size:15px;color:#eef6ff}.sheet .body{padding:18px}.sheet .foot{background:#132936;padding:12px 18px}
label.f>span{font-size:12px;font-weight:550;color:#c0d5e3}input:not([type]),input[type=email],input[type=url],input[type=text],input[type=search],select{background:#0f2230;border-color:#405e72;color:var(--text);min-height:40px}.hint{font-size:12px;color:#b8cddd}.kv{gap:8px 14px}.kv dt{color:#aec9db}.kv dd{color:#eef6ff}.domain-card{background:#122936;border-color:#3a596c}.domain-card strong{color:#8cece2}
@media(max-width:900px){main{padding:18px 18px 38px}.toolbar-actions{justify-content:flex-start}.exit-toolbar{gap:12px}}
@media(max-width:620px){
  html{scroll-padding-top:145px}
  main{padding:15px 12px 30px}header{gap:10px;padding:10px 12px;flex-wrap:wrap}.brand-avatar{width:35px;height:40px}.brand-name{flex:1;min-width:0}.brand-name h1{font-size:17px}.brand-name small{font-size:10px}header>.spacer{display:none}
  .header-actions{width:100%;display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:7px}.header-actions button{justify-content:center;font-size:12px;padding:6px 4px;min-height:44px}
  .miku-welcome{padding:17px 18px;min-height:126px;border-radius:14px;margin-bottom:17px}.miku-welcome h2{font-size:22px}.miku-welcome p{font-size:12px}.miku-welcome .eyebrow{font-size:11px}.miku-welcome>div{max-width:74%}.miku-welcome img{height:245px;right:-38px;top:-12px;opacity:.58}
  .exit-toolbar{display:grid;grid-template-columns:minmax(0,1fr);gap:11px;margin:18px 0 13px}.toolbar-title{gap:9px}.toolbar-actions{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:7px;width:100%}.toolbar-actions button{font-size:11px;justify-content:center;padding:7px 3px;gap:4px;min-width:0;min-height:44px}.toolbar-actions button svg{width:12px;height:12px}
  .bar h2{font-size:15px}.ip-summary{padding:11px 12px;gap:9px}.ip-summary .network-owner{flex-basis:100%;gap:8px}.ip-summary .asn-owner{font-size:12px}.ip-summary .summary-latency{font-size:11px}.ip-summary button{margin-left:0}.exit>.row{padding:12px}.exit-location{font-size:12px}
  .orphan{padding:13px}.orphan .top{flex-wrap:wrap}.orphan .top .spacer{display:none}.orphan .top .count{flex-basis:100%}.orow select{flex:1;min-width:0}.sheet .body{padding:15px}.sheet .head,.sheet .foot{padding:12px 15px}
}
@media(max-width:360px){main{padding-left:10px;padding-right:10px}.header-actions button{font-size:11px}.toolbar-actions button{font-size:10px}.toolbar-actions button svg{display:none}}
@media(prefers-reduced-motion:reduce){*,*:before,*:after{animation:none !important;transition:none !important;scroll-behavior:auto !important}}
`
