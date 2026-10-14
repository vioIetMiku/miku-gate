// Package paneladapter uses the panel's persistent API; never writes generated files.
package paneladapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mikugate/internal/certassets"
	"mikugate/internal/domain"
	"mikugate/internal/ingress"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"time"
)

var ErrPanel = errors.New("本机面板接口、CSRF或持久配置未验证；不回显原始响应/凭据")
var ErrChanged = errors.New("面板配置在预览后变化，未应用；重新检测并确认")

type Endpoint struct {
	HTTPS   bool   `json:"https,omitempty"`
	TLSName string `json:"tls_name,omitempty"`
	Port    int    `json:"port"`
	Base    string `json:"base"`
	Mode    string `json:"mode"`
}

func (e Endpoint) Validate() error {
	if e.Port < 1024 || e.Port > 65535 || !regexp.MustCompile(`^/(?:[A-Za-z0-9_-]+/)*$`).MatchString(e.Base) || len(e.Base) > 128 || (e.Mode != "current" && e.Mode != "legacy") {
		return ErrPanel
	}
	if !e.HTTPS && e.TLSName != "" {
		return ErrPanel
	}
	if e.HTTPS && e.TLSName != "" {
		host, err := certassets.Hostname(e.TLSName)
		if err != nil || host != e.TLSName || !domain.PublicHostname(host) {
			return ErrPanel
		}
	}
	return nil
}

type Snapshot struct {
	Template json.RawMessage `json:"template"`
	Rendered json.RawMessage `json:"rendered"`
	TestURL  string          `json:"test_url"`
}

func Canonical(raw []byte) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > ingress.MaxConfig {
		return nil, ErrPanel
	}
	value, e := ingress.CanonicalJSON(raw)
	if e != nil {
		return nil, ErrPanel
	}
	return value, nil
}
func Hash(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func (s Snapshot) Same(other Snapshot) bool {
	return bytes.Equal(s.Template, other.Template) && bytes.Equal(s.Rendered, other.Rendered) && s.TestURL == other.TestURL
}

type Runtime interface {
	Login(context.Context, string, string, string) error
	Read(context.Context) (Snapshot, error)
	Save(context.Context, Snapshot) error
}
type Client struct {
	endpoint           Endpoint
	http               *http.Client
	csrf               string
	origin, base, xray string
}

func New(endpoint Endpoint) (Runtime, error) {
	if endpoint.Validate() != nil {
		return nil, ErrPanel
	}
	jar, _ := cookiejar.New(nil)
	scheme := "http"
	if endpoint.HTTPS {
		scheme = "https"
	}
	origin := fmt.Sprintf("%s://127.0.0.1:%d", scheme, endpoint.Port)
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: endpoint.TLSName}, DisableKeepAlives: true, ResponseHeaderTimeout: 8 * time.Second, DialContext: func(ctx context.Context, _ string, address string) (net.Conn, error) {
		if address != fmt.Sprintf("127.0.0.1:%d", endpoint.Port) {
			return nil, ErrPanel
		}
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp4", address)
	}}
	client := &Client{endpoint: endpoint, http: &http.Client{Jar: jar, Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrPanel }}, origin: origin, base: origin + endpoint.Base, xray: "panel/api/xray/"}
	if endpoint.Mode == "legacy" {
		client.xray = "panel/xray/"
	}
	return client, nil
}
func (c *Client) request(ctx context.Context, path string, form url.Values) (json.RawMessage, error) {
	if path != "csrf-token" && path != "login" && path != c.xray && path != c.xray+"update" && path != "panel/api/server/getConfigJson" && path != "panel/api/server/restartXrayService" && path != "panel/api/inbounds/list" && path != "panel/api/inbounds/addClient" {
		return nil, ErrPanel
	}
	method := "GET"
	var body io.Reader
	if form != nil {
		method = "POST"
		body = bytes.NewBufferString(form.Encode())
	}
	req, e := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if e != nil {
		return nil, ErrPanel
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", c.origin)
	req.Header.Set("Referer", c.base)
	req.Header.Set("User-Agent", "MikuGate-explicit-local-panel")
	if method == "POST" && c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	response, e := c.http.Do(req)
	if e != nil {
		return nil, ErrPanel
	}
	defer response.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(response.Body, (3<<20)+1))
	if e != nil || len(raw) > 3<<20 || response.StatusCode != 200 {
		return nil, ErrPanel
	}
	var result struct {
		Success bool            `json:"success"`
		Object  json.RawMessage `json:"obj"`
	}
	if json.Unmarshal(raw, &result) != nil || !result.Success {
		return nil, ErrPanel
	}
	return result.Object, nil
}

// EnsureInboundClient adds only our independent landing identity to the exact
// selected inbound. Other clients and their traffic/expiry settings are kept.
func (c *Client) EnsureInboundClient(ctx context.Context, tag string, port int, uuid, password, email string, trojan bool) error {
	if c.endpoint.Mode != "current" {
		return ErrPanel
	}
	raw, err := c.request(ctx, "panel/api/inbounds/list", nil)
	if err != nil {
		return err
	}
	var inbounds []struct {
		ID       int             `json:"id"`
		Tag      string          `json:"tag"`
		Port     int             `json:"port"`
		Protocol string          `json:"protocol"`
		Settings json.RawMessage `json:"settings"`
	}
	if json.Unmarshal(raw, &inbounds) != nil {
		return ErrPanel
	}
	for _, in := range inbounds {
		if (in.Tag != tag && !(in.Tag == "" && tag == fmt.Sprintf("inbound-%d", in.Port))) || in.Port != port {
			continue
		}
		settings, err := unwrap(in.Settings)
		if err != nil {
			return err
		}
		var clients struct {
			Clients []struct {
				ID       string `json:"id"`
				Password string `json:"password"`
				Email    string `json:"email"`
				Enable   bool   `json:"enable"`
			} `json:"clients"`
		}
		if json.Unmarshal(settings, &clients) != nil {
			return ErrPanel
		}
		for _, client := range clients.Clients {
			if client.Email == email {
				if !client.Enable || (trojan && client.Password != password) || (!trojan && client.ID != uuid) {
					return ErrChanged
				}
				return nil
			}
		}
		client := map[string]any{"email": email, "enable": true, "totalGB": 0, "expiryTime": 0, "limitIp": 0}
		if trojan {
			if in.Protocol != "trojan" {
				return ErrPanel
			}
			client["password"] = password
		} else {
			if in.Protocol != "vless" {
				return ErrPanel
			}
			client["id"], client["flow"] = uuid, "xtls-rprx-vision"
		}
		payload, _ := json.Marshal(map[string]any{"clients": []any{client}})
		_, err = c.request(ctx, "panel/api/inbounds/addClient", url.Values{"id": {fmt.Sprint(in.ID)}, "settings": {string(payload)}})
		return err
	}
	return ErrPanel
}
func (c *Client) token(ctx context.Context) error {
	raw, e := c.request(ctx, "csrf-token", nil)
	var token string
	if e != nil || json.Unmarshal(raw, &token) != nil || len(token) < 16 || len(token) > 256 {
		return ErrPanel
	}
	c.csrf = token
	return nil
}
func (c *Client) Login(ctx context.Context, user, password, twoFactor string) error {
	if len(user) < 1 || len(user) > 255 || len(password) < 1 || len(password) > 512 || len(twoFactor) > 32 {
		return ErrPanel
	}
	if c.endpoint.Mode == "current" {
		if e := c.token(ctx); e != nil {
			return e
		}
	}
	if _, e := c.request(ctx, "login", url.Values{"username": {user}, "password": {password}, "twoFactorCode": {twoFactor}}); e != nil {
		return e
	}
	if c.endpoint.Mode == "current" {
		return c.token(ctx)
	}
	return nil
}
func unwrap(raw json.RawMessage) (json.RawMessage, error) {
	for i := 0; i < 3; i++ {
		var str string
		if json.Unmarshal(raw, &str) != nil {
			return Canonical(raw)
		}
		raw = []byte(str)
		if len(raw) > ingress.MaxConfig {
			return nil, ErrPanel
		}
	}
	return nil, ErrPanel
}
func (c *Client) Read(ctx context.Context) (Snapshot, error) {
	raw, e := c.request(ctx, c.xray, url.Values{})
	if e != nil {
		return Snapshot{}, e
	}
	raw, e = unwrap(raw)
	if e != nil {
		return Snapshot{}, e
	}
	var settings struct {
		Template json.RawMessage `json:"xraySetting"`
		TestURL  string          `json:"outboundTestUrl"`
	}
	if json.Unmarshal(raw, &settings) != nil {
		return Snapshot{}, ErrPanel
	}
	template, e := unwrap(settings.Template)
	if e != nil {
		return Snapshot{}, e
	}
	rendered, e := c.request(ctx, "panel/api/server/getConfigJson", nil)
	if e != nil {
		return Snapshot{}, e
	}
	rendered, e = unwrap(rendered)
	if e != nil || len(settings.TestURL) > 1024 {
		return Snapshot{}, ErrPanel
	}
	if settings.TestURL == "" {
		settings.TestURL = "https://www.google.com/generate_204"
	}
	return Snapshot{template, rendered, settings.TestURL}, nil
}
func (c *Client) Save(ctx context.Context, snapshot Snapshot) error {
	if _, e := Canonical(snapshot.Template); e != nil {
		return e
	}
	if _, e := c.request(ctx, c.xray+"update", url.Values{"xraySetting": {string(snapshot.Template)}, "outboundTestUrl": {snapshot.TestURL}}); e != nil {
		return e
	}
	_, e := c.request(ctx, "panel/api/server/restartXrayService", url.Values{})
	return e
}
func Verify(before, after Snapshot, plan ingress.Plan) error {
	var old, new map[string]json.RawMessage
	if json.Unmarshal(before.Rendered, &old) != nil || json.Unmarshal(after.Rendered, &new) != nil {
		return ErrPanel
	}
	for name, original := range old {
		if name != "outbounds" && name != "routing" && !bytes.Equal(original, new[name]) {
			return ErrChanged
		}
	}
	if !bytes.Equal(old["inbounds"], new["inbounds"]) {
		return ErrChanged
	}
	var beforeOutbounds, afterOutbounds []json.RawMessage
	if json.Unmarshal(old["outbounds"], &beforeOutbounds) != nil || json.Unmarshal(new["outbounds"], &afterOutbounds) != nil {
		return ErrPanel
	}
	for _, original := range beforeOutbounds {
		preserved := false
		for _, current := range afterOutbounds {
			if bytes.Equal(original, current) {
				preserved = true
				break
			}
		}
		if !preserved {
			return ErrChanged
		}
	}
	var oldRouting, newRouting map[string]json.RawMessage
	if len(old["routing"]) > 0 {
		if json.Unmarshal(old["routing"], &oldRouting) != nil || json.Unmarshal(new["routing"], &newRouting) != nil {
			return ErrPanel
		}
		for key, value := range oldRouting {
			if key != "rules" && !bytes.Equal(value, newRouting[key]) {
				return ErrChanged
			}
		}
	}
	var oldRules, newRules []json.RawMessage
	if len(oldRouting["rules"]) > 0 {
		if json.Unmarshal(oldRouting["rules"], &oldRules) != nil || json.Unmarshal(newRouting["rules"], &newRules) != nil {
			return ErrPanel
		}
		offset := 0
		for _, original := range oldRules {
			found := false
			for offset < len(newRules) {
				current := newRules[offset]
				offset++
				if bytes.Equal(original, current) {
					found = true
					break
				}
			}
			if !found {
				return ErrChanged
			}
		}
	}
	var config struct {
		Outbounds []struct {
			Tag      string `json:"tag"`
			Protocol string `json:"protocol"`
			Settings struct {
				Servers []struct {
					Address string `json:"address"`
					Port    int    `json:"port"`
				} `json:"servers"`
			} `json:"settings"`
		} `json:"outbounds"`
		Routing struct {
			Rules []struct {
				Inbounds []string `json:"inboundTag"`
				Network  string   `json:"network"`
				Outbound string   `json:"outboundTag"`
			} `json:"rules"`
		} `json:"routing"`
	}
	if json.Unmarshal(after.Rendered, &config) != nil {
		return ErrPanel
	}
	socks, blocked := false, false
	for _, row := range config.Outbounds {
		if row.Tag == plan.OutboundTag && row.Protocol == "socks" && len(row.Settings.Servers) == 1 && row.Settings.Servers[0].Address == "127.0.0.1" && row.Settings.Servers[0].Port == plan.ProxyPort {
			socks = true
		}
		if row.Tag == plan.OutboundTag+"-udp-block" && row.Protocol == "blackhole" {
			blocked = true
		}
	}
	selected := []int{}
	for i, row := range config.Routing.Rules {
		for _, tag := range row.Inbounds {
			if tag == plan.InboundTag {
				selected = append(selected, i)
				break
			}
		}
	}
	if !socks || !blocked || len(selected) < 2 {
		return ErrPanel
	}
	first, second := config.Routing.Rules[selected[0]], config.Routing.Rules[selected[1]]
	if first.Network != "udp" || first.Outbound != plan.OutboundTag+"-udp-block" || second.Network != "tcp" || second.Outbound != plan.OutboundTag {
		return ErrPanel
	}
	return nil
}
