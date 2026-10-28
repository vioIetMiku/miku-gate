window.MikuTheme=(function(){
  const root=document.documentElement,key='miku.theme';
  function apply(value){
    const light=value==='light',mode=light?'light':'dark',label=light?'夜间模式':'白天模式';
    root.setAttribute('data-theme',mode);root.style.colorScheme=mode;
    const meta=document.querySelector('meta[name="theme-color"]');
    if(meta) meta.setAttribute('content',light?'#f4f7f5':'#101a1c');
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
