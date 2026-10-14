package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// webServer 管理 HTTP 监听，支持在运行时切换端口/监听地址而不重启进程。
// 切换端口或监听地址会新起一个 net.Listener，旧的优雅关闭。
type webServer struct {
	handler http.Handler

	mu                  sync.Mutex
	ln                  net.Listener
	srv                 *http.Server
	addr                string
	secure              bool
	certificate         *tls.Certificate
	previousCertificate *tls.Certificate
	previousUntil       time.Time
	settings            WebSettings
	applyMu             sync.Mutex
}

func newWebServer(h http.Handler) *webServer {
	return &webServer{handler: h}
}

// serve 用当前 WebSettings 起第一个监听并阻塞。返回时说明监听彻底退出。
func (s *webServer) serve() error {
	cfg := getWebSettings()
	if err := s.reload(cfg); err != nil {
		return err
	}
	// 主 goroutine 就地阻塞，等监听被 reload 或退出替换。
	// 这里靠一个永不返回的 select 挂住：真正的 Serve 在 reload 里各自的 goroutine 跑。
	select {}
}

// reload 切换到新的监听地址：先探测能否绑上，绑得上再关旧的、启新的。
// 绑不上就保持旧监听不动，返回错误让调用方回报给用户。
func (s *webServer) reload(cfg WebSettings) error {
	addr := cfg.listenAddrString()
	secure := cfg.TLSCert != "" || cfg.TLSKey != ""
	var pair *tls.Certificate
	if secure {
		value, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			return fmt.Errorf("管理证书或私钥无效，保留原监听")
		}
		pair = &value
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil && addr == s.addr {
		s.updateProtocol(cfg, pair)
		return nil
	}
	oldSrv, oldLn := s.srv, s.ln
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("无法监听 %s", addr)
	}
	s.updateProtocol(cfg, pair)
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: s.getCertificate}
	ln = newWebProtocolListener(ln, tlsConfig)
	server := &http.Server{Handler: http.HandlerFunc(s.handleWebRequest), TLSConfig: tlsConfig, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 64 << 10}
	s.srv, s.ln, s.addr = server, ln, addr
	go func() {
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed && !strings.Contains(err.Error(), "closed network connection") {
			log.Printf("HTTP监听退出: %v", err)
		}
	}()
	if oldSrv != nil {
		go func() {
			time.Sleep(time.Second)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = oldSrv.Shutdown(ctx)
			_ = oldLn.Close()
		}()
	}
	return nil
}

// Caller holds s.mu. Retain the prior certificate briefly so a page open on
// HTTPS can receive the new address after unbinding instead of a TLS failure.
func (s *webServer) updateProtocol(cfg WebSettings, pair *tls.Certificate) {
	if s.certificate != nil {
		s.previousCertificate = s.certificate
		s.previousUntil = time.Now().Add(10 * time.Minute)
	}
	s.certificate = pair
	s.secure = pair != nil
	s.settings = cfg
}

func (s *webServer) getCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.certificate != nil {
		if hello.ServerName != "" && hello.SupportsCertificate(s.certificate) != nil && s.previousCertificate != nil && time.Now().Before(s.previousUntil) && hello.SupportsCertificate(s.previousCertificate) == nil {
			return s.previousCertificate, nil
		}
		return s.certificate, nil
	}
	if s.previousCertificate != nil && time.Now().Before(s.previousUntil) {
		return s.previousCertificate, nil
	}
	return nil, fmt.Errorf("管理地址当前未启用HTTPS")
}

func (s *webServer) handleWebRequest(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	cfg, secure := s.settings, s.secure
	s.mu.Unlock()
	requestHost, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		requestHost = r.Host
	}
	hostChanged := validDomain(cfg.PublicHost) && !strings.EqualFold(strings.TrimSuffix(requestHost, "."), strings.TrimSuffix(cfg.PublicHost, "."))
	// An HTTPS-offloading proxy may use plaintext to the backend even when
	// application TLS is disabled. Actual old TLS connections still hand off.
	protocolChanged := secure && !requestHTTPS(r) || !secure && r.TLS != nil
	if protocolChanged || hostChanged {
		base := currentBasePath()
		if base != "" && r.URL.Path != base && !strings.HasPrefix(r.URL.Path, base+"/") {
			http.NotFound(w, r)
			return
		}
		host := cfg.PublicIP
		if host == "" {
			host, _, _ = net.SplitHostPort(r.Host)
			if host == "" {
				host = r.Host
			}
		}
		url := cfg.origin(host) + base + "/"
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Miku-Management-URL", url)
		path := r.URL.Path
		if base != "" {
			path = strings.TrimPrefix(path, base)
		}
		if strings.HasPrefix(path, "/api/") || r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeJSON(w, http.StatusConflict, map[string]string{"code": "management_moved", "error": "管理地址已更新，请打开新的登录地址继续操作", "url": url})
			return
		}
		http.Redirect(w, r, url, http.StatusTemporaryRedirect)
		return
	}
	s.handler.ServeHTTP(w, r)
}

// Forwarded protocol is honored only for a proxy on this VPS's loopback.
// Remote clients cannot use a header to bypass HTTPS or Origin checks.
func requestHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return r.Header.Get("X-Forwarded-Proto") == "https"
		}
	}
	return false
}

// applyWebSettings 校验、落盘并切换监听。任一步失败都不改动线上监听。
func (s *webServer) applyWebSettings(next WebSettings) error {
	s.applyMu.Lock()
	defer s.applyMu.Unlock()
	if err := validatePort(next.Port); err != nil {
		return err
	}
	norm, err := normalizeListenAddr(next.ListenAddr)
	if err != nil {
		return err
	}
	next.ListenAddr = norm

	cur := getWebSettings()
	webSettingsMu.Lock()
	webSettingsCur = next
	webSettingsMu.Unlock()
	if err := saveWebSettings(); err != nil {
		webSettingsMu.Lock()
		webSettingsCur = cur
		webSettingsMu.Unlock()
		return err
	}
	if err := s.reload(next); err != nil {
		webSettingsMu.Lock()
		webSettingsCur = cur
		webSettingsMu.Unlock()
		_ = saveWebSettings()
		return err
	}
	return nil
}
