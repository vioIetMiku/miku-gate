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

	mu          sync.Mutex
	ln          net.Listener
	srv         *http.Server
	addr        string
	secure      bool
	certificate *tls.Certificate
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
	if s.ln != nil && addr == s.addr && secure == s.secure {
		s.certificate = pair
		return nil
	}
	oldSrv, oldLn, oldAddr, oldSecure, oldPair := s.srv, s.ln, s.addr, s.secure, s.certificate
	if oldLn != nil && oldAddr == addr {
		_ = oldLn.Close()
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if oldLn != nil && oldAddr == addr {
			if restored, restoreErr := net.Listen("tcp", oldAddr); restoreErr == nil {
				s.certificate = oldPair
				if oldSecure {
					restored = tls.NewListener(restored, &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
						s.mu.Lock()
						defer s.mu.Unlock()
						return s.certificate, nil
					}})
				}
				server := &http.Server{Handler: s.handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
				s.srv, s.ln = server, restored
				go server.Serve(restored)
			}
		}
		return fmt.Errorf("无法监听 %s", addr)
	}
	s.certificate = pair
	s.secure = secure
	var tlsConfig *tls.Config
	if secure {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.certificate, nil
		}}
		ln = tls.NewListener(ln, tlsConfig)
	}
	server := &http.Server{Handler: s.handler, TLSConfig: tlsConfig, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 64 << 10}
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

// applyWebSettings 校验、落盘并切换监听。任一步失败都不改动线上监听。
func (s *webServer) applyWebSettings(next WebSettings) error {
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
