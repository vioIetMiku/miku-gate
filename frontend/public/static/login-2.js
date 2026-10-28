window.MikuTheme.bind();
document.getElementById('f').onsubmit = async e => {
  e.preventDefault();
  const body = new URLSearchParams({password: document.getElementById('pw').value});
  const button=document.getElementById('loginButton');button.disabled=true;
  try { const r=await fetch('login',{method:'POST',body});if(r.ok){location.reload();return;}const d=await r.json().catch(()=>({}));document.getElementById('err').textContent=d.error || '登录失败'; }
  catch(err){document.getElementById('err').textContent='连接失败，请稍后重试';}
  finally{button.disabled=false;}
};
