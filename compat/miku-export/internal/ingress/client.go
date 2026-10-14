package ingress

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"strconv"
)

// ClientBundle contains only intended client credentials, never server private
// material, upstream credentials or an assertion about current server health.
type ClientBundle struct {
	ClientJSON []byte
	URI        string
}

func (m ManagedMaterial) ClientBundle() (ClientBundle, error) {
	if m.LegacyDisabled {
		return ClientBundle{}, ErrDeviceUnavailable
	}
	plan, err := ManagedPlanFor(m.Request)
	if err != nil {
		return ClientBundle{}, err
	}
	raw, uri, err := renderClient(plan, m.Identity)
	return ClientBundle{ClientJSON: raw, URI: uri}, err
}
func renderClient(plan ManagedPlan, identity ManagedIdentity) ([]byte, string, error) {
	if err := identity.Validate(plan.NeedsRealityTarget); err != nil {
		return nil, "", err
	}
	r := plan.Request
	protocol := "vless"
	if r.Template == "trojan-tls" {
		protocol = "trojan"
	}
	publicKey := ""
	if plan.NeedsRealityTarget {
		raw, err := base64.RawURLEncoding.DecodeString(identity.RealityPrivateKey)
		if err != nil {
			return nil, "", ErrConfig
		}
		key, err := ecdh.X25519().NewPrivateKey(raw)
		if err != nil {
			return nil, "", ErrConfig
		}
		publicKey = base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
	}
	var client map[string]any
	if r.Core == "xray" {
		user := map[string]any{"id": identity.UUID, "encryption": "none", "flow": "xtls-rprx-vision"}
		settings := map[string]any{"vnext": []any{map[string]any{"address": r.Address, "port": r.Port, "users": []any{user}}}}
		if protocol == "trojan" {
			settings = map[string]any{"servers": []any{map[string]any{"address": r.Address, "port": r.Port, "password": identity.Password}}}
		}
		stream := map[string]any{"network": "raw", "security": "tls", "tlsSettings": map[string]any{"serverName": r.ServerName, "allowInsecure": false, "alpn": []string{"h2", "http/1.1"}}}
		if plan.NeedsRealityTarget {
			stream = map[string]any{"network": "raw", "security": "reality", "realitySettings": map[string]any{"serverName": r.ServerName, "fingerprint": "chrome", "password": publicKey, "shortId": identity.ShortID, "spiderX": "/"}}
		}
		client = map[string]any{"log": map[string]any{"loglevel": "warning"}, "inbounds": []any{map[string]any{"tag": "local-client", "listen": "127.0.0.1", "port": 1080, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": true}}}, "outbounds": []any{map[string]any{"tag": "miku-server", "protocol": protocol, "settings": settings, "streamSettings": stream}}}
	} else {
		out := map[string]any{"tag": "miku-server", "type": protocol, "server": r.Address, "server_port": r.Port}
		if protocol == "vless" {
			out["uuid"] = identity.UUID
			out["flow"] = "xtls-rprx-vision"
		} else {
			out["password"] = identity.Password
		}
		out["tls"] = map[string]any{"enabled": true, "server_name": r.ServerName, "insecure": false, "alpn": []string{"h2", "http/1.1"}}
		if plan.NeedsRealityTarget {
			out["tls"] = map[string]any{"enabled": true, "server_name": r.ServerName, "utls": map[string]any{"enabled": true, "fingerprint": "chrome"}, "reality": map[string]any{"enabled": true, "public_key": publicKey, "short_id": identity.ShortID}}
		}
		client = map[string]any{"log": map[string]any{"level": "warn"}, "inbounds": []any{map[string]any{"type": "socks", "listen": "127.0.0.1", "listen_port": 1080}}, "outbounds": []any{out}, "route": map[string]any{"final": "miku-server"}}
	}
	raw, err := json.MarshalIndent(client, "", "  ")
	if err != nil {
		return nil, "", ErrConfig
	}
	raw = append(raw, '\n')
	query := url.Values{"type": {"tcp"}, "security": {"tls"}, "sni": {r.ServerName}, "alpn": {"h2,http/1.1"}}
	credential := identity.UUID
	if protocol == "vless" {
		query.Set("encryption", "none")
		query.Set("flow", "xtls-rprx-vision")
	} else {
		credential = identity.Password
	}
	if plan.NeedsRealityTarget {
		query.Set("security", "reality")
		query.Set("fp", "chrome")
		query.Set("pbk", publicKey)
		query.Set("sid", identity.ShortID)
		query.Del("alpn")
	}
	uri := (&url.URL{Scheme: protocol, User: url.User(credential), Host: net.JoinHostPort(r.Address, strconv.Itoa(r.Port)), RawQuery: query.Encode(), Fragment: "Miku Gate " + r.Core}).String()
	return raw, uri, nil
}
