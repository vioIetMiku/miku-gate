package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Auth 给管理界面加一层登录。
// 口令存在工作目录下，首次启动自动生成，避免公网上裸奔。
type Auth struct {
	dir      string
	password string
	mu       sync.RWMutex
	sessions map[string]time.Time
	// 按来源 IP 记录登录失败，挡低速凭据喷洒
	fails map[string]*loginFails
}

// loginFails 跟踪单个来源 IP 的连续失败。
type loginFails struct {
	count   int
	last    time.Time
	blocked time.Time
}

const sessionTTL = 12 * time.Hour

// 登录失败节流：同一 IP 连续错 loginMaxFails 次后，锁 loginBlockFor。
// 阈值给得宽松，正常用户偶尔输错不受影响；成功登录会清零。
const (
	loginMaxFails  = 8
	loginBlockFor  = 2 * time.Minute
	loginFailReset = 10 * time.Minute
)

// NewAuth 载入或生成访问口令。返回口令是否为本次新建。
func NewAuth(dir string) (*Auth, bool, error) {
	path := filepath.Join(dir, "password")
	created := false

	blob, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		pw, gerr := randomToken(9)
		if gerr != nil {
			return nil, false, gerr
		}
		if werr := os.WriteFile(path, []byte(pw+"\n"), 0600); werr != nil {
			return nil, false, fmt.Errorf("写口令文件失败: %w", werr)
		}
		blob = []byte(pw)
		created = true
	} else if err != nil {
		return nil, false, err
	}

	return &Auth{
		dir:      dir,
		password: strings.TrimSpace(string(blob)),
		sessions: map[string]time.Time{},
		fails:    map[string]*loginFails{},
	}, created, nil
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// check 比对口令，用恒定时间比较避免时序泄漏。
func (a *Auth) check(pw string) bool {
	a.mu.RLock()
	cur := a.password
	a.mu.RUnlock()
	want := sha256.Sum256([]byte(cur))
	got := sha256.Sum256([]byte(pw))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
}

// SetPassword 改访问口令并落盘。空口令拒绝，避免误改成无密码裸奔。
// 改完不动已有会话：当前登录的浏览器不会被踢，新登录才用新口令。
func (a *Auth) SetPassword(pw string) error {
	pw = strings.TrimSpace(pw)
	if pw == "" {
		return fmt.Errorf("口令不能为空")
	}
	if len(pw) < 4 {
		return fmt.Errorf("口令至少 4 位")
	}
	path := filepath.Join(a.dir, "password")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(pw+"\n"), 0600); err != nil {
		return fmt.Errorf("写口令文件失败: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("保存口令失败: %w", err)
	}
	a.mu.Lock()
	a.password = pw
	a.mu.Unlock()
	return nil
}

// issue 发一个会话 token。
func (a *Auth) issue() (string, error) {
	tok, err := randomToken(16)
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.sessions[tok] = time.Now().Add(sessionTTL)
	// 顺手清掉过期会话
	for k, exp := range a.sessions {
		if time.Now().After(exp) {
			delete(a.sessions, k)
		}
	}
	a.mu.Unlock()
	return tok, nil
}

func (a *Auth) valid(tok string) bool {
	a.mu.RLock()
	exp, ok := a.sessions[tok]
	a.mu.RUnlock()
	return ok && time.Now().Before(exp)
}

const sessionCookie = "miku_session"

// Wrap 保护一个 handler，未登录时 API 返回 401、页面跳登录。
func (a *Auth) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writePaths := map[string]bool{"/api/start": true, "/api/stop": true, "/api/swap": true, "/api/cred": true, "/api/refresh": true, "/api/provision": true, "/api/jobs/dismiss": true, "/api/xui/bind": true, "/api/xui/clone": true, "/api/xui/delete": true, "/api/panel/inbound/new": true, "/api/panel/inbound/update": true, "/api/panel/client/add": true, "/api/panel/client/del": true, "/api/panel/client/reset": true, "/api/panel/client/bind": true, "/api/sub/reset": true, "/api/update/apply": true}
			if writePaths[r.URL.Path] && r.Method != http.MethodPost {
				writeJSON(w, 405, map[string]string{"error": "此操作请用POST"})
				return
			}
			if r.Method == http.MethodPost && r.Header.Get("Origin") != "" {
				scheme := "http"
				if r.TLS != nil {
					scheme = "https"
				}
				if r.Header.Get("Origin") != scheme+"://"+r.Host {
					writeJSON(w, 403, map[string]string{"error": "请求来源不匹配"})
					return
				}
			}
		}
		if r.URL.Path == "/login" {
			a.handleLogin(w, r)
			return
		}
		// 订阅得让客户端直接拉，带不了登录态，所以这条路放行。
		// 它不是无门槛：handleSub 自己校验一串独立口令，且仍在访问路径之后。
		if r.URL.Path == "/sub" {
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie(sessionCookie); err == nil && a.valid(c.Value) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "未登录"})
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(loginHTML))
	})
}

// blocked 判断某来源 IP 是否处于登录冷却期。
func (a *Auth) blocked(ip string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	f, ok := a.fails[ip]
	return ok && time.Now().Before(f.blocked)
}

// recordFail 记一次失败，达到阈值就进入冷却。
func (a *Auth) recordFail(ip string) {
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	f, ok := a.fails[ip]
	// 距上次失败太久就重新计数，避免长期累积误伤
	if !ok || (f.blocked.IsZero() && now.Sub(f.last) > loginFailReset) {
		f = &loginFails{}
		a.fails[ip] = f
	}
	f.count++
	f.last = now
	if f.count >= loginMaxFails {
		f.blocked = now.Add(loginBlockFor)
		f.count = 0
	}
	// 顺手清掉早已过期的记录，别让 map 无限增长
	for k, v := range a.fails {
		if now.Sub(v.last) > loginFailReset && now.After(v.blocked) {
			delete(a.fails, k)
		}
	}
}

// clearFails 登录成功后清掉该 IP 的失败记录。
func (a *Auth) clearFails(ip string) {
	a.mu.Lock()
	delete(a.fails, ip)
	a.mu.Unlock()
}

// clientIP 从 RemoteAddr 取来源 IP。服务直接监听公网端口、不在反代后，
// 所以不采信 X-Forwarded-For 之类可伪造的头。
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (a *Auth) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(loginHTML))
		return
	}
	ip := clientIP(r)
	if a.blocked(ip) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "登录失败次数过多，请稍后再试"})
		return
	}
	if !a.check(r.FormValue("password")) {
		a.recordFail(ip)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "口令不对"})
		return
	}
	a.clearFails(ip)
	tok, err := a.issue()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   r.TLS != nil,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]string{"ok": "已登录"})
}

const loginHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="theme-color" content="#0a1620">
<title>Miku</title>
<script>` + themeInitJS + `</script>
<style>
*{box-sizing:border-box}body{margin:0;min-height:100svh;color:#e7f4f4;background:radial-gradient(ellipse at 65% 35%,#163e43 0,#0b171c 42%,#080f13 85%);font:15px/1.6 system-ui,-apple-system,"Segoe UI",sans-serif;display:grid;place-items:center;padding:28px}.login-shell{width:min(960px,100%);display:grid;grid-template-columns:1fr 1fr;gap:24px;align-items:center}.login-copy{position:relative;z-index:2}.eyebrow{color:#39c5bb;font-size:11px;letter-spacing:4px}.logo{font-size:44px;letter-spacing:5px;margin:12px 0 0;font-weight:800}.logo span{color:#39c5bb;font-weight:500}.subtitle{color:#b0c8cc;margin:8px 0 30px;font-size:16px}form{background:rgba(18,36,42,.82);border:1px solid #315159;border-radius:20px;padding:28px;backdrop-filter:blur(20px);box-shadow:0 20px 70px #0004}h2{font-size:19px;margin:0 0 8px}.intro{color:#92afb6;font-size:12px;margin-bottom:24px}label{display:block;color:#bbd6da;margin:10px 0;font-size:13px}input{width:100%;border:1px solid #35535c;background:#0a181d;color:#e7f4f4;padding:13px 15px;border-radius:10px;font:inherit}input:focus{outline:2px solid #39c5bb55;border-color:#39c5bb}button{width:100%;margin-top:18px;padding:12px;border:0;border-radius:10px;background:#39c5bb;color:#061d21;font:inherit;font-weight:700;cursor:pointer}button:disabled{opacity:.6}.err{color:#ff92b8;font-size:12px;min-height:20px;margin-top:12px}.help{font-size:11px;color:#79979f;margin-top:8px}.help code{color:#b5dbd8}.login-art{width:100%;height:min(75vh,730px);object-fit:contain;filter:drop-shadow(0 16px 50px #0005)}footer{margin-top:22px;color:#63878f;font-size:11px}footer a{color:#8ac3c3;text-decoration:none}.signal{display:flex;gap:4px;align-items:center;height:16px;margin-top:24px}.signal i{width:3px;background:#39c5bb;border-radius:3px;opacity:.65}.signal i:nth-child(1),.signal i:nth-child(5){height:5px}.signal i:nth-child(2),.signal i:nth-child(4){height:11px}.signal i:nth-child(3){height:16px}@media(max-width:680px){body{padding:20px;overflow-x:hidden}.login-shell{grid-template-columns:1fr;max-width:430px;position:relative}.login-art{position:absolute;right:-130px;top:-75px;height:440px;width:340px;opacity:.24;z-index:0}.logo{font-size:34px}.login-copy{padding-top:50px}form{background:rgba(11,28,34,.9)}.subtitle{font-size:14px}}
` + appearanceCSS + `
</style></head><body><div class="login-theme">` + themeButtonHTML + `</div><main class="login-shell"><section class="login-copy"><span class="eyebrow">CONNECTION STUDIO / 01</span><h1 class="logo">MIKU <span>GATE</span></h1><p class="subtitle">准备好了吗？一起点亮下一站吧。</p><form id="f"><h2>欢迎回来</h2><p class="intro">世界就在那一端，和我一起，让连接的旋律响起来吧。</p><label for="pw">访问口令</label><input type="password" id="pw" autofocus autocomplete="current-password" placeholder="输入你的访问口令"><button type="submit" id="loginButton">一起连接世界 →</button><div class="err" id="err"></div><div class="help">忘记口令？在VPS运行 <code>miku password</code></div></form><div class="signal" aria-hidden="true"><i></i><i></i><i></i><i></i><i></i></div><footer>Miku Gate · 世界，等你连接。</footer></section><img class="login-art" src="assets/miku-hero-v1.png" alt="Miku"></main>
<script>
` + themeControlsJS + `
document.getElementById('f').onsubmit = async e => {
  e.preventDefault();
  const body = new URLSearchParams({password: document.getElementById('pw').value});
  const button=document.getElementById('loginButton');button.disabled=true;
  try { const r=await fetch('login',{method:'POST',body});if(r.ok){location.reload();return;}const d=await r.json().catch(()=>({}));document.getElementById('err').textContent=d.error || '登录失败'; }
  catch(err){document.getElementById('err').textContent='连接失败，请稍后重试';}
  finally{button.disabled=false;}
};
</script>
</body>
</html>`
