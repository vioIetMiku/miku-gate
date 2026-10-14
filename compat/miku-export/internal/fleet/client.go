// Package fleet connects explicitly registered HTTPS agents with bounded scope.
package fleet

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/egress"
	"mikugate/internal/gateway"
)

type State struct {
	Version    string          `json:"version"`
	Instance   string          `json:"instance"`
	Role       string          `json:"role"`
	Exits      []domain.Exit   `json:"exits"`
	Runtimes   []gateway.State `json:"runtimes"`
	ObservedAt time.Time       `json:"observed_at"`
}

func Validate(h domain.FleetHost) error {
	u, err := url.Parse(h.Origin)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(u.Host, "\x00\r\n ") || len(h.Origin) > 300 {
		return errors.New("远端需精确HTTPS Origin，不允许路径、用户信息或查询参数")
	}
	if u.Port() != "" {
		n, err := strconv.Atoi(u.Port())
		if err != nil || n < 1 || n > 65535 {
			return errors.New("远端端口无效")
		}
	}
	if len(h.Token) != 43 || strings.ContainsAny(h.Token, "\x00\r\n ") {
		return errors.New("远端令牌格式无效")
	}
	if len([]rune(h.Name)) < 1 || len([]rune(h.Name)) > 48 || strings.ContainsAny(h.Name, "\x00\r\n") {
		return errors.New("远端名称需1–48字符")
	}
	if len(h.CA) > 16384 {
		return errors.New("自有CA超过预算")
	}
	if h.CA != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(h.CA)) {
			return errors.New("CA证书无效")
		}
	}
	return nil
}
func Request(ctx context.Context, h domain.FleetHost, method, path string) ([]byte, error) {
	if err := Validate(h); err != nil {
		return nil, err
	}
	if path != "/agent/v1/state" && !strings.HasPrefix(path, "/agent/v1/exits/") {
		return nil, errors.New("远端操作路径无效")
	}
	u, _ := url.Parse(h.Origin)
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if h.CA != "" && !roots.AppendCertsFromPEM([]byte(h.CA)) {
		return nil, errors.New("自有CA无效")
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true, MaxResponseHeaderBytes: 8192, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: u.Hostname()}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || !strings.EqualFold(host, u.Hostname()) {
			return nil, errors.New("远端地址发生变化")
		}
		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(addresses) == 0 || len(addresses) > 16 {
			return nil, errors.New("远端解析失败")
		}
		for _, ip := range addresses {
			ip = ip.Unmap()
			if !allowed(ip, h.AllowPrivate) {
				return nil, errors.New("远端解析包含未授权本地/私网/特殊地址")
			}
		}
		var last error
		for _, ip := range addresses {
			d := net.Dialer{Timeout: 4 * time.Second}
			conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("拒绝远端重定向") }}
	request, err := http.NewRequestWithContext(ctx, method, h.Origin+path, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+h.Token)
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("远端连接或TLS验证失败")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, errors.New("远端响应超预算")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, errors.New("远端拒绝操作，请检查令牌角色、范围、有效期和运行状态")
	}
	return raw, nil
}
func allowed(ip netip.Addr, private bool) bool {
	if !ip.IsValid() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	if ip.IsPrivate() {
		return private
	}
	if ip.Is4() {
		return egress.PublicIPv4(ip.String()) == nil
	}
	return ip.IsGlobalUnicast()
}
func Inspect(ctx context.Context, h domain.FleetHost) (State, error) {
	raw, err := Request(ctx, h, "GET", "/agent/v1/state")
	if err != nil {
		return State{}, err
	}
	var s State
	if json.Unmarshal(raw, &s) != nil || s.Instance == "" || s.ObservedAt.IsZero() || len(s.Exits) > 100 || len(s.Runtimes) > 100 || time.Since(s.ObservedAt) > time.Minute || time.Until(s.ObservedAt) > time.Minute {
		return State{}, errors.New("远端状态无效或时间过期")
	}
	return s, nil
}
