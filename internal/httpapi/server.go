package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"mikugate/internal/diagnostics"
	"mikugate/internal/domain"
	"mikugate/internal/executoripc"
	"mikugate/internal/gateway"
	"mikugate/internal/netexec"
	"mikugate/internal/security"
	"mikugate/internal/store"
	"mikugate/internal/vpngate"
	"mikugate/internal/vpnipc"
)

type attempt struct {
	count int
	until time.Time
}
type SandboxClient interface {
	executoripc.Runtime
	Capabilities(context.Context) (executoripc.Capabilities, error)
}
type Options struct {
	InspectNodeIdentity func(context.Context, domain.Node) (domain.NodeIdentity, error)
	Sandbox             SandboxClient
	Gateway             *gateway.Manager
	VPN                 *vpnipc.Client
}
type Server struct {
	nodeIdentities  map[string]domain.NodeIdentity
	identitySlot    chan struct{}
	lastIdentity    time.Time
	inspectIdentity func(context.Context, domain.Node) (domain.NodeIdentity, error)
	gateway         *gateway.Manager
	vpnClient       *vpnipc.Client
	sandbox         SandboxClient
	sandboxSlot     chan struct{}
	store           *store.Store
	assets          fs.FS
	dummyHash       string
	authSlots       chan struct{}
	writeSlots      chan struct{}
	refreshSlot     chan struct{}
	mu              sync.Mutex
	attempts        map[string]attempt
	lastRefresh     time.Time
}

func New(s *store.Store, assets fs.FS) (http.Handler, error) {
	return NewWithOptions(s, assets, Options{})
}
func NewWithOptions(s *store.Store, assets fs.FS, options Options) (http.Handler, error) {
	dummy, err := security.HashPassword("not-an-account-" + time.Now().String())
	if err != nil {
		return nil, err
	}
	inspect := options.InspectNodeIdentity
	if inspect == nil {
		inspect = vpngate.InspectIdentity
	}
	a := &Server{nodeIdentities: map[string]domain.NodeIdentity{}, identitySlot: make(chan struct{}, 1), inspectIdentity: inspect, gateway: options.Gateway, vpnClient: options.VPN, sandbox: options.Sandbox, sandboxSlot: make(chan struct{}, 1), store: s, assets: assets, dummyHash: dummy, authSlots: make(chan struct{}, 2), writeSlots: make(chan struct{}, 2), refreshSlot: make(chan struct{}, 1), attempts: map[string]attempt{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]any{"version": domain.Version, "status": a.mode()})
	})
	mux.HandleFunc("POST /api/auth/login", a.login)
	mux.HandleFunc("GET /api/auth/session", a.auth(func(w http.ResponseWriter, r *http.Request, s domain.Session) {
		write(w, 200, map[string]any{"username": s.Username, "csrf": s.CSRF, "expires_at": s.ExpiresAt})
	}))
	mux.HandleFunc("POST /api/auth/logout", a.auth(func(w http.ResponseWriter, r *http.Request, s domain.Session) {
		c, _ := r.Cookie("miku_session")
		if err := a.store.DeleteSession(c.Value); err != nil {
			a.internal(w, err)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "miku_session", Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		w.WriteHeader(204)
	}))
	mux.HandleFunc("GET /api/overview", a.auth(a.overview))
	mux.HandleFunc("GET /api/exits", a.auth(a.listExits))
	mux.HandleFunc("POST /api/exits", a.auth(a.createExit))
	mux.HandleFunc("PATCH /api/exits/{id}", a.auth(a.stopped(a.updateExit)))
	mux.HandleFunc("DELETE /api/exits/{id}", a.auth(a.stopped(a.deleteExit)))
	mux.HandleFunc("GET /api/vpn-executor", a.auth(a.vpnStatus))
	mux.HandleFunc("GET /api/exits/{id}/vpn", a.auth(a.getVPN))
	mux.HandleFunc("PUT /api/exits/{id}/vpn", a.auth(a.stopped(a.saveVPN)))
	mux.HandleFunc("DELETE /api/exits/{id}/vpn", a.auth(a.stopped(a.removeVPN)))
	mux.HandleFunc("GET /api/executor", a.auth(a.executorStatus))
	mux.HandleFunc("GET /api/exits/{id}/sandbox", a.auth(a.sandboxInspect))
	mux.HandleFunc("POST /api/exits/{id}/sandbox/prepare", a.auth(a.stopped(a.sandboxPrepare)))
	mux.HandleFunc("POST /api/exits/{id}/sandbox/cleanup", a.auth(a.stopped(a.sandboxCleanup)))
	mux.HandleFunc("GET /api/exits/{id}/network-plan", a.auth(a.networkPlan))
	mux.HandleFunc("GET /api/exits/{id}/upstream", a.auth(a.getUpstream))
	mux.HandleFunc("PUT /api/exits/{id}/upstream", a.auth(a.stopped(a.saveUpstream)))
	mux.HandleFunc("DELETE /api/exits/{id}/upstream", a.auth(a.stopped(a.removeUpstream)))
	mux.HandleFunc("GET /api/exits/{id}/runtime", a.auth(a.runtimeState))
	mux.HandleFunc("POST /api/exits/{id}/probe", a.auth(a.checkRuntime))
	mux.HandleFunc("POST /api/exits/{id}/start", a.auth(a.startExit))
	mux.HandleFunc("POST /api/exits/{id}/stop", a.auth(a.stopExit))
	mux.HandleFunc("POST /api/exits/{id}/credentials", a.auth(a.stopped(a.rotateCredentials)))
	mux.HandleFunc("GET /api/nodes", a.auth(func(w http.ResponseWriter, r *http.Request, s domain.Session) {
		v, err := a.store.ListNodes()
		if err != nil {
			a.internal(w, err)
			return
		}
		write(w, 200, v)
	}))
	mux.HandleFunc("POST /api/nodes/{id}/identity", a.auth(a.nodeIdentity))
	mux.HandleFunc("GET /api/nodes/{id}/profile", a.auth(a.nodeProfile))
	mux.HandleFunc("POST /api/nodes/refresh", a.auth(a.refreshNodes))
	mux.HandleFunc("GET /api/events", a.auth(func(w http.ResponseWriter, r *http.Request, s domain.Session) {
		v, err := a.store.Events()
		if err != nil {
			a.internal(w, err)
			return
		}
		write(w, 200, v)
	}))
	mux.HandleFunc("GET /api/diagnostics", a.auth(func(w http.ResponseWriter, r *http.Request, s domain.Session) { write(w, 200, diagnostics.Inspect()) }))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "not_found", "接口不存在") })
	mux.HandleFunc("/", a.static)
	return a.guard(mux), nil
}
func (a *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("Cache-Control", "no-store")
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			fail(w, 403, "invalid_host", "开发版本只接受本机访问")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host != r.Host {
				fail(w, 403, "invalid_origin", "跨站请求被拒绝")
				return
			}
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			fail(w, 403, "invalid_origin", "跨站请求被拒绝")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (a *Server) auth(fn func(http.ResponseWriter, *http.Request, domain.Session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("miku_session")
		if err != nil || len(c.Value) > 128 {
			fail(w, 401, "unauthenticated", "请先登录")
			return
		}
		s, err := a.store.Session(c.Value)
		if err != nil {
			fail(w, 401, "unauthenticated", "会话已失效，请重新登录")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			token := r.Header.Get("X-CSRF-Token")
			if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.CSRF)) != 1 {
				fail(w, 403, "invalid_csrf", "安全校验失败，请刷新页面")
				return
			}
			select {
			case a.writeSlots <- struct{}{}:
				defer func() { <-a.writeSlots }()
			default:
				fail(w, 429, "busy", "操作繁忙，请稍后重试")
				return
			}
		}
		fn(w, r, s)
	}
}
func (a *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.Username) > 64 || len(in.Password) > 1024 {
		fail(w, 400, "invalid_input", "输入过长")
		return
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	a.mu.Lock()
	now := time.Now()
	for k, v := range a.attempts {
		if now.After(v.until) {
			delete(a.attempts, k)
		}
	}
	v := a.attempts[ip]
	if v.count >= 5 || len(a.attempts) > 1024 {
		a.mu.Unlock()
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "rate_limited", "尝试过于频繁，请一分钟后再试")
		return
	}
	if v.count == 0 {
		v.until = now.Add(time.Minute)
	}
	v.count++
	a.attempts[ip] = v
	a.mu.Unlock()
	select {
	case a.authSlots <- struct{}{}:
		defer func() { <-a.authSlots }()
	default:
		fail(w, 429, "busy", "认证繁忙，请稍后再试")
		return
	}
	hash, err := a.store.PasswordHash(in.Username)
	if err != nil {
		hash = a.dummyHash
	}
	valid := security.VerifyPassword(hash, in.Password)
	if !valid || err != nil {
		fail(w, 401, "invalid_credentials", "用户名或密码不正确")
		return
	}
	token, err := security.Token(32)
	if err != nil {
		a.internal(w, err)
		return
	}
	csrf, err := security.Token(24)
	if err != nil {
		a.internal(w, err)
		return
	}
	expiry := time.Now().Add(8 * time.Hour)
	if err = a.store.CreateSession(in.Username, token, csrf, expiry); err != nil {
		a.internal(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "miku_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil, MaxAge: 8 * 3600, Expires: expiry})
	write(w, 200, map[string]any{"username": in.Username, "csrf": csrf, "expires_at": expiry})
}
func (a *Server) overview(w http.ResponseWriter, r *http.Request, s domain.Session) {
	exits, err := a.store.ListExits()
	if err != nil {
		a.internal(w, err)
		return
	}
	nodes, err := a.store.ListNodes()
	if err != nil {
		a.internal(w, err)
		return
	}
	countries := map[string]bool{}
	for _, e := range exits {
		countries[e.Country] = true
	}
	active, healthy, verifiedVPN := 0, 0, 0
	message := "控制台与配置管理可用；VPN 数据面尚未接入，没有活动代理或隧道。"
	if a.gateway != nil {
		for _, state := range a.gateway.States() {
			if state.ProxyListening {
				active++
			}
			if state.Proof.Kind == "openvpn" && state.Proof.KernelVerified && state.Healthy {
				verifiedVPN++
			}
			if state.Healthy {
				healthy++
			}
		}
		message = "HTTP/SOCKS/HTTPS代理上游运行时已启用，入口仅本机；隔离OpenVPN执行器需要另行明确授权。"
	}
	vpnReachable := false
	if a.vpnClient != nil {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		_, err := a.vpnClient.Capabilities(ctx)
		cancel()
		vpnReachable = err == nil
		message = "已配置隔离OpenVPN执行器；代理/隧道启动必须各自通过实际路径验证。"
	}
	write(w, 200, map[string]any{"version": domain.Version, "mode": a.mode(), "exit_count": len(exits), "active_count": active, "healthy_count": healthy, "country_count": len(countries), "node_count": len(nodes), "executor_available": vpnReachable, "vpn_runtime_configured": a.vpnClient != nil, "vpn_verified_count": verifiedVPN, "proxy_runtime_available": a.gateway != nil, "protection_verified": active > 0 && verifiedVPN == active, "message": message})
}
func (a *Server) createExit(w http.ResponseWriter, r *http.Request, s domain.Session) {
	var in domain.ExitInput
	if !decode(w, r, &in) {
		return
	}
	if err := in.Validate(); err != nil {
		fail(w, 400, "invalid_input", err.Error())
		return
	}
	e, pass, err := a.store.CreateExit(in)
	if err != nil {
		a.storeError(w, err)
		return
	}
	write(w, 201, map[string]any{"exit": e, "password": pass, "notice": "密码只显示本次，且尚未启动任何代理监听。"})
}
func (a *Server) updateExit(w http.ResponseWriter, r *http.Request, s domain.Session) {
	var in domain.ExitInput
	if !decode(w, r, &in) {
		return
	}
	if in.Port != 0 {
		fail(w, 400, "immutable_port", "更新保留原端口；需要其他端口请新建出口")
		return
	}
	if err := in.Validate(); err != nil {
		fail(w, 400, "invalid_input", err.Error())
		return
	}
	if err := a.store.UpdateExit(r.PathValue("id"), in); err != nil {
		a.storeError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (a *Server) deleteExit(w http.ResponseWriter, r *http.Request, s domain.Session) {
	if !a.acquireSandbox(w) {
		return
	}
	defer func() { <-a.sandboxSlot }()
	if a.sandbox != nil {
		if !a.requireExit(w, r.PathValue("id")) {
			return
		}
		actual, err := a.sandbox.Inspect(r.Context(), r.PathValue("id"))
		if err != nil {
			a.sandboxError(w, err)
			return
		}
		if actual.Phase != "absent" {
			fail(w, 409, "sandbox_cleanup_required", "请先清理并确认本出口隔离沙箱不存在；未删除配置。")
			return
		}
	}
	if err := a.store.DeleteExit(r.PathValue("id")); err != nil {
		a.storeError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (a *Server) startExit(w http.ResponseWriter, r *http.Request, s domain.Session) {
	if a.gateway != nil {
		if !a.acquireSandbox(w) {
			return
		}
		defer func() { <-a.sandboxSlot }()
		if !a.requireExit(w, r.PathValue("id")) {
			return
		}
		if a.sandbox != nil {
			actual, err := a.sandbox.Inspect(r.Context(), r.PathValue("id"))
			if err != nil {
				a.sandboxError(w, err)
				return
			}
			if actual.Phase != "absent" {
				fail(w, 409, "sandbox_cleanup_required", "请先清理本出口的隔离沙箱，再启动代理上游模式")
				return
			}
		}
		state, err := a.gateway.Start(r.Context(), r.PathValue("id"))
		if err != nil {
			a.runtimeError(w, err)
			return
		}
		write(w, 200, state)
		return
	}

	reason := "启动已阻止：VPN 数据面尚未接入，未建立隧道、未监听代理端口。"
	if err := a.store.SetStatus(r.PathValue("id"), "blocked", reason); err != nil {
		a.storeError(w, err)
		return
	}
	fail(w, 409, "executor_unavailable", reason)
}
func (a *Server) stopExit(w http.ResponseWriter, r *http.Request, s domain.Session) {
	if a.gateway != nil {
		if err := a.gateway.Stop(r.Context(), r.PathValue("id")); err != nil {
			a.runtimeError(w, err)
			return
		}
		w.WriteHeader(204)
		return
	}

	if err := a.store.SetStatus(r.PathValue("id"), "stopped", "已停止；当前版本无活动代理监听。"); err != nil {
		a.storeError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (a *Server) rotateCredentials(w http.ResponseWriter, r *http.Request, s domain.Session) {
	pass, err := a.store.RotateCredentials(r.PathValue("id"))
	if err != nil {
		a.storeError(w, err)
		return
	}
	write(w, 200, map[string]string{"password": pass})
}
func (a *Server) refreshNodes(w http.ResponseWriter, r *http.Request, s domain.Session) {
	select {
	case a.refreshSlot <- struct{}{}:
		defer func() { <-a.refreshSlot }()
	default:
		fail(w, 409, "refresh_running", "已有节点同步任务运行中")
		return
	}
	a.mu.Lock()
	if time.Since(a.lastRefresh) < time.Minute {
		a.mu.Unlock()
		fail(w, 429, "refresh_limited", "节点同步至少间隔一分钟")
		return
	}
	a.lastRefresh = time.Now()
	a.mu.Unlock()
	nodes, skipped, err := vpngate.Fetch(r.Context())
	if err != nil {
		a.store.AddEvent("warning", "nodes.refresh_failed", "节点同步失败，已保留原缓存。", "")
		fail(w, 502, "source_unavailable", err.Error())
		return
	}
	if err = a.store.ReplaceNodes(r.Context(), nodes); err != nil {
		a.internal(w, err)
		return
	}
	write(w, 200, map[string]any{"count": len(nodes), "skipped": skipped, "tested": false})
}
func (a *Server) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		fail(w, 405, "method_not_allowed", "方法不支持")
		return
	}
	if a.assets == nil {
		fail(w, 503, "frontend_missing", "请先构建前端")
		return
	}
	p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if p == "" || p == "." {
		p = "index.html"
	}
	if strings.HasPrefix(p, ".") || strings.Contains(p, "/.") {
		http.NotFound(w, r)
		return
	}
	if _, err := fs.Stat(a.assets, p); err != nil {
		if strings.HasPrefix(p, "assets/") || path.Ext(p) != "" {
			http.NotFound(w, r)
			return
		}
		p = "index.html"
	}
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/" + p
	if p == "index.html" {
		b, err := fs.ReadFile(a.assets, p)
		if err != nil {
			fail(w, 503, "frontend_missing", "请先构建前端")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method != "HEAD" {
			w.Write(b)
		}
		return
	}
	http.FileServer(http.FS(a.assets)).ServeHTTP(w, r2)
}
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		fail(w, 415, "content_type", "请求须为 application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		fail(w, 400, "invalid_json", "请求格式无效或超出大小限制")
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		fail(w, 400, "invalid_json", "请求只能包含一个 JSON 对象")
		return false
	}
	return true
}
func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, code, message string) {
	write(w, status, map[string]string{"code": code, "message": message})
}
func (a *Server) internal(w http.ResponseWriter, err error) {
	log.Printf("internal operation failed: %T", err)
	fail(w, 500, "internal_error", "操作失败，请查看本机日志并重试")
}
func (a *Server) storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		fail(w, 404, "not_found", "出口不存在")
	case errors.Is(err, domain.ErrConflict):
		fail(w, 409, "conflict", err.Error())
	default:
		a.internal(w, err)
	}
}

// networkPlan only renders a deterministic plan. It never opens the privileged
// helper, modifies exit state, or uses an operating-system command.
func (a *Server) networkPlan(w http.ResponseWriter, r *http.Request, session domain.Session) {
	exits, err := a.store.ListExits()
	if err != nil {
		a.internal(w, err)
		return
	}
	for _, exit := range exits {
		if exit.ID != r.PathValue("id") {
			continue
		}
		instanceID, err := a.store.InstanceID()
		if err != nil {
			a.internal(w, err)
			return
		}
		plan, err := netexec.BuildPlan(instanceID, exit.ID)
		if err != nil {
			a.internal(w, err)
			return
		}
		write(w, http.StatusOK, plan)
		return
	}
	a.storeError(w, domain.ErrNotFound)
}
