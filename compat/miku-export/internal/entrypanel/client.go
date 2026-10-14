// Package entrypanel reuses an existing panel listener through its persistent
// API, with encrypted local credentials and user-specific OpenVPN landing rules.
package entrypanel

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"mikugate/internal/ingress"
	"mikugate/internal/paneladapter"
)

var ErrPanel = errors.New("无法验证所选本机面板接口或持久配置；请核实端口、路径和凭据")

type Client struct {
	spec   ingress.ExternalEntrance
	xray   paneladapter.Runtime
	http   *http.Client
	origin string
}
type Snapshot struct {
	Rendered json.RawMessage
	Template json.RawMessage
	Original paneladapter.Snapshot
	Load     map[string]json.RawMessage
}
type InboundView struct {
	Tag        string `json:"tag"`
	Protocol   string `json:"protocol"`
	Port       int    `json:"port"`
	Core       string `json:"core"`
	Template   string `json:"template"`
	ServerName string `json:"server_name"`
	Supported  bool   `json:"supported"`
	Reason     string `json:"reason"`
}

func New(ctx context.Context, spec ingress.ExternalEntrance) (*Client, error) {
	endpoint := paneladapter.Endpoint{Port: spec.Port, Base: spec.Base, HTTPS: spec.HTTPS, TLSName: spec.TLSName, Mode: "current"}
	if endpoint.Validate() != nil || len(spec.Secret) < 1 || len(spec.Secret) > 255 || strings.ContainsAny(spec.Secret, "\x00\r\n") || (spec.Kind != "3x-ui" && spec.Kind != "s-ui") {
		return nil, ErrPanel
	}
	c := &Client{spec: spec}
	if spec.Kind == "3x-ui" {
		runtime, err := paneladapter.New(endpoint)
		if err != nil {
			return nil, err
		}
		if err = runtime.Login(ctx, spec.Username, spec.Secret, ""); err != nil {
			return nil, err
		}
		c.xray = runtime
		return c, nil
	}
	scheme := "http"
	if spec.HTTPS {
		scheme = "https"
	}
	c.origin = fmt.Sprintf("%s://127.0.0.1:%d", scheme, spec.Port)
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: spec.TLSName}, DisableKeepAlives: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != fmt.Sprintf("127.0.0.1:%d", spec.Port) {
			return nil, ErrPanel
		}
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp4", address)
	}}
	c.http = &http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrPanel }}
	return c, nil
}

func (c *Client) request(ctx context.Context, path string, form url.Values) (json.RawMessage, error) {
	if path != "load" && path != "save" && path != "status?r=sbd" {
		return nil, ErrPanel
	}
	method := "GET"
	var body io.Reader
	if form != nil {
		method = "POST"
		body = strings.NewReader(form.Encode())
	}
	r, err := http.NewRequestWithContext(ctx, method, c.origin+c.spec.Base+"apiv2/"+path, body)
	if err != nil {
		return nil, ErrPanel
	}
	r.Header.Set("Token", c.spec.Secret)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", c.origin)
	response, err := c.http.Do(r)
	if err != nil {
		return nil, ErrPanel
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 3<<20))
	if err != nil || len(raw) >= 3<<20 || response.StatusCode != 200 {
		return nil, ErrPanel
	}
	var result struct {
		Success bool            `json:"success"`
		Obj     json.RawMessage `json:"obj"`
	}
	if json.Unmarshal(raw, &result) != nil || !result.Success {
		return nil, ErrPanel
	}
	return result.Obj, nil
}

func object(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) > 0 && raw[0] == '"' {
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return nil, ErrPanel
		}
		raw = []byte(value)
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(raw, &result) != nil || result == nil {
		return nil, ErrPanel
	}
	return result, nil
}
func items(raw json.RawMessage) ([]map[string]any, error) {
	var values []map[string]any
	if json.Unmarshal(raw, &values) != nil {
		return nil, ErrPanel
	}
	return values, nil
}
func clone(value any) map[string]any {
	raw, _ := json.Marshal(value)
	var result map[string]any
	json.Unmarshal(raw, &result)
	return result
}
func n(value any) int {
	switch value := value.(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		number, _ := value.Int64()
		return int(number)
	}
	return 0
}
func text(value any) string { result, _ := value.(string); return result }

func (c *Client) Read(ctx context.Context) (Snapshot, error) {
	if c.xray != nil {
		snapshot, err := c.xray.Read(ctx)
		return Snapshot{Rendered: snapshot.Rendered, Template: snapshot.Template, Original: snapshot}, err
	}
	raw, err := c.request(ctx, "load", nil)
	if err != nil {
		return Snapshot{}, err
	}
	load, err := object(raw)
	if err != nil {
		return Snapshot{}, err
	}
	configRaw := load["config"]
	config, err := object(configRaw)
	if err != nil {
		return Snapshot{}, err
	}
	var root map[string]any
	canonical, _ := json.Marshal(config)
	if json.Unmarshal(canonical, &root) != nil {
		return Snapshot{}, ErrPanel
	}
	ins, err := items(load["inbounds"])
	if err != nil {
		return Snapshot{}, err
	}
	outs, err := items(load["outbounds"])
	if err != nil {
		return Snapshot{}, err
	}
	clients, err := items(load["clients"])
	if err != nil {
		return Snapshot{}, err
	}
	tlsObjects, err := items(load["tls"])
	if err != nil {
		return Snapshot{}, err
	}
	inbounds := []any{}
	for _, original := range ins {
		in := clone(original)
		id := n(in["id"])
		delete(in, "id")
		delete(in, "out_json")
		delete(in, "addrs")
		tlsID := n(in["tls_id"])
		delete(in, "tls_id")
		for _, entry := range tlsObjects {
			if n(entry["id"]) == tlsID {
				in["tls"] = entry["server"]
			}
		}
		users := []any{}
		for _, client := range clients {
			enabled, _ := client["enable"].(bool)
			if !enabled {
				continue
			}
			bound := false
			if list, ok := client["inbounds"].([]any); ok {
				for _, value := range list {
					if n(value) == id {
						bound = true
					}
				}
			}
			if !bound {
				continue
			}
			credentials, _ := client["config"].(map[string]any)
			profile, _ := credentials[text(in["type"])].(map[string]any)
			if profile == nil {
				continue
			}
			user := clone(profile)
			user["name"] = client["name"]
			users = append(users, user)
		}
		if text(in["type"]) == "vless" || text(in["type"]) == "trojan" {
			in["users"] = users
		}
		inbounds = append(inbounds, in)
	}
	outbounds := []any{}
	for _, entry := range outs {
		out := clone(entry)
		delete(out, "id")
		outbounds = append(outbounds, out)
	}
	root["inbounds"], root["outbounds"] = inbounds, outbounds
	for _, key := range []string{"endpoints", "services"} {
		if values, e := items(load[key]); e == nil {
			clean := []any{}
			for _, entry := range values {
				v := clone(entry)
				delete(v, "id")
				clean = append(clean, v)
			}
			root[key] = clean
		}
	}
	rendered, err := json.Marshal(root)
	if err != nil || len(rendered) > ingress.MaxConfig {
		return Snapshot{}, ErrPanel
	}
	return Snapshot{Rendered: rendered, Template: canonical, Load: load}, nil
}

func (c *Client) Inbounds(snapshot Snapshot) ([]InboundView, error) {
	var root struct {
		Inbounds []map[string]any `json:"inbounds"`
	}
	if json.Unmarshal(snapshot.Rendered, &root) != nil {
		return nil, ErrPanel
	}
	views := []InboundView{}
	for _, in := range root.Inbounds {
		view := InboundView{Tag: text(in["tag"]), Core: "sing-box", Protocol: text(in["type"]), Port: n(in["listen_port"]), Reason: "请选择支持的VLESS/Trojan TCP入口，或在空闲端口重建"}
		security := "tls"
		network := "tcp"
		if c.xray != nil {
			view.Core = "xray"
			view.Protocol = text(in["protocol"])
			view.Port = n(in["port"])
			stream, _ := in["streamSettings"].(map[string]any)
			security = text(stream["security"])
			network = text(stream["network"])
			if network == "" || network == "raw" {
				network = "tcp"
			}
			if security == "reality" {
				settings, _ := stream["realitySettings"].(map[string]any)
				if names, ok := settings["serverNames"].([]any); ok && len(names) > 0 {
					view.ServerName = text(names[0])
				}
			} else {
				tls, _ := stream["tlsSettings"].(map[string]any)
				view.ServerName = text(tls["serverName"])
			}
		} else {
			tls, _ := in["tls"].(map[string]any)
			enabled, _ := tls["enabled"].(bool)
			if !enabled {
				security = "none"
			}
			view.ServerName = text(tls["server_name"])
			if reality, ok := tls["reality"].(map[string]any); ok {
				if enabled, _ := reality["enabled"].(bool); enabled {
					security = "reality"
				}
			}
			if in["transport"] != nil {
				network = "transport"
			}
		}
		view.Template = "vless-tls-vision"
		if security == "reality" {
			view.Template = "vless-reality-vision"
		}
		if view.Protocol == "trojan" {
			view.Template = "trojan-tls"
		}
		view.Supported = view.Tag != "" && (view.Protocol == "vless" || view.Protocol == "trojan") && network == "tcp" && (security == "tls" || security == "reality")
		if view.Protocol == "trojan" && security != "tls" {
			view.Supported = false
		}
		if view.Supported {
			view.Reason = "可复用此入口端口，为各VPN落地新增独立客户端"
		}
		views = append(views, view)
	}
	return views, nil
}

func (c *Client) Material(snapshot Snapshot, tag, address, sni string) (ingress.ManagedMaterial, error) {
	views, err := c.Inbounds(snapshot)
	if err != nil {
		return ingress.ManagedMaterial{}, err
	}
	var selected InboundView
	for _, view := range views {
		if view.Tag == tag && view.Supported {
			selected = view
		}
	}
	if selected.Tag == "" {
		return ingress.ManagedMaterial{}, ErrPanel
	}
	if sni == "" {
		sni = selected.ServerName
	}
	if sni == "" && net.ParseIP(address) == nil {
		sni = address
	}
	if sni == "" {
		return ingress.ManagedMaterial{}, errors.New("请填写原TLS节点的SNI域名")
	}
	var root struct {
		Inbounds []map[string]any `json:"inbounds"`
	}
	json.Unmarshal(snapshot.Rendered, &root)
	device, err := ingress.NewDevice("existing")
	if err != nil {
		return ingress.ManagedMaterial{}, err
	}
	m := ingress.ManagedMaterial{Request: ingress.ManagedRequest{Core: selected.Core, Template: selected.Template, Address: address, Port: selected.Port, Listen: "0.0.0.0", ServerName: sni}, Proxy: ingress.Proxy{Port: 20000, Username: "unbound", Password: device.Password}, Identity: ingress.ManagedIdentity{UUID: device.UUID, Password: device.Password}}
	if m.Proxy.Port == m.Request.Port {
		m.Proxy.Port++
	}
	for _, in := range root.Inbounds {
		if text(in["tag"]) != tag {
			continue
		}
		if listen := text(in["listen"]); net.ParseIP(listen) != nil {
			m.Request.Listen = listen
		}
		raw, _ := json.Marshal(in)
		spec := c.spec
		spec.InboundTag, spec.Inbound = tag, raw
		m.External = &spec
		if selected.Template == "vless-reality-vision" {
			m.Request.RealityHostname = sni
			if c.xray != nil {
				stream, _ := in["streamSettings"].(map[string]any)
				reality, _ := stream["realitySettings"].(map[string]any)
				m.Identity.RealityPrivateKey = text(reality["privateKey"])
				if ids, ok := reality["shortIds"].([]any); ok && len(ids) > 0 {
					m.Identity.ShortID = text(ids[0])
				}
			} else {
				tls, _ := in["tls"].(map[string]any)
				reality, _ := tls["reality"].(map[string]any)
				m.Identity.RealityPrivateKey = text(reality["private_key"])
				if ids, ok := reality["short_id"].([]any); ok && len(ids) > 0 {
					m.Identity.ShortID = text(ids[0])
				}
			}
		}
	}
	if _, err = ingress.ManagedPlanFor(m.Request); err != nil {
		return m, err
	}
	if m.Identity.Validate(m.Request.Template == "vless-reality-vision") != nil {
		return m, errors.New("原入口REALITY格式不兼容，请选择在空闲端口重新搭建")
	}
	return m, nil
}

func (c *Client) Apply(ctx context.Context, group string, base ingress.ManagedMaterial, routes []ingress.LandingRoute, pause bool) error {
	if len(routes) == 0 {
		return errors.New("先为入口添加落地")
	}
	before, err := c.Read(ctx)
	if err != nil {
		return err
	}
	if !pause {
		for _, route := range routes {
			user := "miku-" + group + "-" + route.ExitID
			if c.xray != nil {
				client, ok := c.xray.(*paneladapter.Client)
				if !ok {
					return ErrPanel
				}
				if err = client.EnsureInboundClient(ctx, c.spec.InboundTag, base.Request.Port, route.Identity.UUID, route.Identity.Password, user, base.Request.Template == "trojan-tls"); err != nil {
					return err
				}
			} else {
				if err = c.ensureSUIClient(ctx, before, base, route, user); err != nil {
					return err
				}
			}
		}
		before, err = c.Read(ctx)
		if err != nil {
			return err
		}
	}
	prefix := "miku-eg-" + group + "-"
	tag := c.spec.InboundTag
	var config map[string]any
	if json.Unmarshal(before.Template, &config) != nil {
		return ErrPanel
	}
	rootKey, rulesKey := "route", "outbound"
	if c.xray != nil {
		rootKey, rulesKey = "routing", "outboundTag"
	}
	routing, _ := config[rootKey].(map[string]any)
	if routing == nil {
		routing = map[string]any{}
	}
	oldRules, _ := routing["rules"].([]any)
	kept := []any{}
	for _, rule := range oldRules {
		v, _ := rule.(map[string]any)
		owned := strings.HasPrefix(text(v[rulesKey]), prefix)
		if c.xray == nil {
			inbounds, _ := v["inbound"].([]any)
			if len(inbounds) == 1 && text(inbounds[0]) == tag && text(v["action"]) == "reject" && text(v["method"]) == "drop" {
				owned = true
			}
		}
		if !owned {
			kept = append(kept, rule)
		}
	}
	outbounds, _ := config["outbounds"].([]any)
	clean := []any{}
	for _, out := range outbounds {
		v, _ := out.(map[string]any)
		if !strings.HasPrefix(text(v["tag"]), prefix) {
			clean = append(clean, out)
		}
	}
	rules := []any{}
	if c.xray != nil {
		block := prefix + "block"
		clean = append(clean, map[string]any{"tag": block, "protocol": "blackhole", "settings": map[string]any{}})
		rules = append(rules, map[string]any{"type": "field", "inboundTag": []string{tag}, "network": "udp", "outboundTag": block})
		if !pause {
			for _, route := range routes {
				out := prefix + route.ExitID
				clean = append(clean, map[string]any{"tag": out, "protocol": "socks", "settings": map[string]any{"servers": []any{map[string]any{"address": "127.0.0.1", "port": route.Proxy.Port, "users": []any{map[string]any{"user": route.Proxy.Username, "pass": route.Proxy.Password}}}}}})
				rules = append(rules, map[string]any{"type": "field", "inboundTag": []string{tag}, "user": []string{"miku-" + group + "-" + route.ExitID}, "network": "tcp", "outboundTag": out})
			}
		}
		fallback := block
		if !pause {
			fallback = prefix + routes[0].ExitID
		}
		rules = append(rules, map[string]any{"type": "field", "inboundTag": []string{tag}, "network": "tcp", "outboundTag": fallback})
		config["outbounds"] = clean
	} else {
		rules = append(rules, map[string]any{"inbound": []string{tag}, "network": "udp", "action": "reject", "method": "drop"})
		if !pause {
			for _, route := range routes {
				out := prefix + route.ExitID
				if err = c.ensureSUIOutbound(ctx, before, route, out); err != nil {
					return err
				}
				rules = append(rules, map[string]any{"inbound": []string{tag}, "auth_user": []string{"miku-" + group + "-" + route.ExitID}, "network": "tcp", "action": "route", "outbound": out})
			}
			rules = append(rules, map[string]any{"inbound": []string{tag}, "network": "tcp", "action": "route", "outbound": prefix + routes[0].ExitID})
		} else {
			rules = append(rules, map[string]any{"inbound": []string{tag}, "action": "reject", "method": "drop"})
		}
		delete(config, "outbounds")
	}
	routing["rules"] = append(rules, kept...)
	config[rootKey] = routing
	candidate, err := json.Marshal(config)
	if err != nil {
		return ErrPanel
	}
	if c.xray != nil {
		next := before.Original
		next.Template = candidate
		if err = c.xray.Save(ctx, next); err != nil {
			return err
		}
	} else {
		if _, err = c.request(ctx, "save", url.Values{"object": {"config"}, "action": {"edit"}, "data": {string(candidate)}}); err != nil {
			return err
		}
	}
	after, err := c.Read(ctx)
	if err != nil {
		return err
	}
	var readback map[string]any
	if json.Unmarshal(after.Template, &readback) != nil {
		return ErrPanel
	}
	actual, _ := json.Marshal(readback[rootKey])
	expected, _ := json.Marshal(config[rootKey])
	if !bytes.Equal(actual, expected) {
		return errors.New("面板未回读到预期落地路由，请重新检查")
	}
	return nil
}

func (c *Client) ensureSUIClient(ctx context.Context, snapshot Snapshot, base ingress.ManagedMaterial, route ingress.LandingRoute, user string) error {
	clients, err := items(snapshot.Load["clients"])
	if err != nil {
		return err
	}
	for _, client := range clients {
		if text(client["name"]) == user {
			enabled, _ := client["enable"].(bool)
			bound := false
			inbounds, _ := client["inbounds"].([]any)
			for _, binding := range inbounds {
				entries, _ := items(snapshot.Load["inbounds"])
				for _, entry := range entries {
					if n(binding) == n(entry["id"]) && text(entry["tag"]) == c.spec.InboundTag && n(entry["listen_port"]) == base.Request.Port {
						bound = true
					}
				}
			}
			if !enabled || !bound {
				return ErrPanel
			}
			config, _ := client["config"].(map[string]any)
			profile, _ := config["vless"].(map[string]any)
			if base.Request.Template == "trojan-tls" {
				profile, _ = config["trojan"].(map[string]any)
				if text(profile["password"]) != route.Identity.Password {
					return ErrPanel
				}
			} else if text(profile["uuid"]) != route.Identity.UUID {
				return ErrPanel
			}
			return nil
		}
	}
	inbounds, err := items(snapshot.Load["inbounds"])
	if err != nil {
		return err
	}
	id := 0
	for _, in := range inbounds {
		if text(in["tag"]) == c.spec.InboundTag {
			id = n(in["id"])
		}
	}
	if id == 0 {
		return ErrPanel
	}
	protocol, profile := "vless", map[string]any{"name": user, "uuid": route.Identity.UUID, "flow": "xtls-rprx-vision"}
	if base.Request.Template == "trojan-tls" {
		protocol = "trojan"
		profile = map[string]any{"name": user, "password": route.Identity.Password}
	}
	value := map[string]any{"enable": true, "name": user, "remark": route.Name, "inbounds": []int{id}, "volume": 0, "expiry": 0, "config": map[string]any{protocol: profile}}
	raw, _ := json.Marshal(value)
	_, err = c.request(ctx, "save", url.Values{"object": {"clients"}, "action": {"new"}, "data": {string(raw)}})
	return err
}
func (c *Client) ensureSUIOutbound(ctx context.Context, snapshot Snapshot, route ingress.LandingRoute, tag string) error {
	outbounds, err := items(snapshot.Load["outbounds"])
	if err != nil {
		return err
	}
	value := map[string]any{"type": "socks", "tag": tag, "server": "127.0.0.1", "server_port": route.Proxy.Port, "version": "5", "username": route.Proxy.Username, "password": route.Proxy.Password, "network": "tcp"}
	action := "new"
	for _, out := range outbounds {
		if text(out["tag"]) == tag {
			value["id"] = out["id"]
			action = "edit"
		}
	}
	raw, _ := json.Marshal(value)
	_, err = c.request(ctx, "save", url.Values{"object": {"outbounds"}, "action": {action}, "data": {string(raw)}})
	return err
}
