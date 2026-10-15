package main

// Self-hosted SVG flags work on Windows without regional-indicator fonts.
const countryFlagCSS = `
.country-flag{display:inline-block;width:24px;height:18px;object-fit:cover;vertical-align:-3px;border-radius:2px;margin-right:7px;box-shadow:0 0 0 1px #7a929744;flex:none}
.node-card-country .country-flag{width:28px;height:21px;vertical-align:-3px}
.exit-sort{width:auto;max-width:125px;font-size:12px;padding:7px;border-radius:8px}
.exit-picker{min-width:0;flex:1}.exit-picker-menu{position:relative;min-width:0}.exit-picker-menu summary{list-style:none;cursor:pointer;padding:7px 10px;border:1px solid var(--line);border-radius:7px;background:var(--panel);display:flex;align-items:center;gap:5px;flex-wrap:wrap}.exit-picker-menu summary:after{content:'▾';margin-left:auto;color:var(--dim)}.exit-picker-menu summary::-webkit-details-marker{display:none}.exit-choice-list{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:5px;max-height:230px;overflow:auto;margin:7px 0;padding:7px;border:1px solid var(--line);border-radius:7px;background:var(--bg)}.exit-choice-list button{justify-content:flex-start;white-space:normal;text-align:left;padding:8px;font-size:12px}.exit-choice-list button[aria-pressed=true]{border-color:var(--accent);color:var(--accent)}.exit-choice-list small{color:var(--dim);margin-left:auto;font-size:11px}
`

const countryFlagJS = `
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
`
