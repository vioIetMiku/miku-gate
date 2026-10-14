package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type subscriptionNode struct {
	Name, Protocol, Server, UUID, Username, Password           string
	Security, ServerName, Fingerprint, PublicKey, ShortID, Pin string
	Network, Path, Host, Flow, Cipher, Encryption              string
	Port, AlterID                                              int
	ALPN                                                       []string
	Insecure                                                   bool
}

func decodeSubscriptionBase64(raw string) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if value, err := encoding.DecodeString(raw); err == nil {
			return value, nil
		}
	}
	return nil, fmt.Errorf("Base64格式无效")
}

func parseSubscriptionNode(raw string) (subscriptionNode, error) {
	n := subscriptionNode{Network: "tcp", Security: "none", Cipher: "auto"}
	raw = strings.TrimSpace(raw)
	if len(raw) > 32<<10 || strings.ContainsAny(raw, "\r\n\x00") {
		return n, fmt.Errorf("节点链接格式无效")
	}
	if strings.HasPrefix(raw, "vmess://") && !strings.Contains(strings.SplitN(strings.TrimPrefix(raw, "vmess://"), "?", 2)[0], "@") {
		data, err := decodeSubscriptionBase64(strings.TrimPrefix(raw, "vmess://"))
		if err != nil {
			return n, err
		}
		var fields map[string]any
		if json.Unmarshal(data, &fields) != nil {
			return n, fmt.Errorf("VMess配置格式无效")
		}
		value := func(key string) string {
			if v, ok := fields[key]; ok {
				return fmt.Sprint(v)
			}
			return ""
		}
		n.Protocol, n.Name, n.Server, n.UUID = "vmess", value("ps"), value("add"), value("id")
		n.Port, _ = strconv.Atoi(value("port"))
		n.AlterID, _ = strconv.Atoi(value("aid"))
		n.Network, n.Path, n.Host = value("net"), value("path"), value("host")
		n.Security, n.ServerName, n.Fingerprint = value("tls"), value("sni"), value("fp")
		if value("scy") != "" {
			n.Cipher = value("scy")
		}
		n.Pin, n.PublicKey, n.ShortID = value("pinSHA256"), value("pbk"), value("sid")
		if value("alpn") != "" {
			n.ALPN = strings.Split(value("alpn"), ",")
		}
	} else {
		u, err := url.Parse(raw)
		if err != nil {
			return n, fmt.Errorf("节点链接无法解析")
		}
		n.Protocol = strings.ToLower(u.Scheme)
		if n.Protocol == "socks" || n.Protocol == "sock5" {
			n.Protocol = "socks5"
		}
		n.Name, n.Server = u.Fragment, u.Hostname()
		if (n.Protocol == "http" || n.Protocol == "https" || n.Protocol == "socks5" || n.Protocol == "sock5" || n.Protocol == "socks") && u.Path != "" && u.Path != "/" {
			return n, fmt.Errorf("请填写代理分享链接；CSV来源请在来源入口添加")
		}
		n.Port, _ = strconv.Atoi(u.Port())
		if n.Port == 0 && n.Protocol == "http" {
			n.Port = 80
		}
		if n.Port == 0 && n.Protocol == "https" {
			n.Port = 443
		}
		q := u.Query()
		n.Network = q.Get("type")
		if n.Network == "" {
			n.Network = q.Get("network")
		}
		n.Security, n.ServerName = q.Get("security"), q.Get("sni")
		if n.ServerName == "" {
			n.ServerName = q.Get("peer")
		}
		n.Fingerprint, n.PublicKey, n.ShortID, n.Pin = q.Get("fp"), q.Get("pbk"), q.Get("sid"), q.Get("pinSHA256")
		n.Path, n.Host, n.Flow, n.Encryption = q.Get("path"), q.Get("host"), q.Get("flow"), q.Get("encryption")
		if n.Network == "grpc" {
			n.Path = q.Get("serviceName")
			if n.Path == "" {
				n.Path = q.Get("path")
			}
		}
		if alpn := q.Get("alpn"); alpn != "" {
			n.ALPN = strings.Split(alpn, ",")
		}
		n.Insecure = q.Get("allowInsecure") == "1" || q.Get("allowInsecure") == "true" || q.Get("insecure") == "1"
		if u.User != nil {
			n.Username = u.User.Username()
			n.Password, _ = u.User.Password()
		}
		switch n.Protocol {
		case "vless", "vmess":
			n.UUID = n.Username
		case "trojan":
			n.Password = n.Username
			if n.Security == "" {
				n.Security = "tls"
			}
		case "https":
			n.Protocol = "http"
			n.Security = "tls"
		case "ss":
			if n.Password == "" {
				decoded, err := decodeSubscriptionBase64(n.Username)
				if err != nil {
					return n, err
				}
				parts := strings.SplitN(string(decoded), ":", 2)
				if len(parts) != 2 {
					return n, fmt.Errorf("SS凭据格式无效")
				}
				n.Cipher, n.Password = parts[0], parts[1]
			} else {
				n.Cipher = n.Username
			}
			if q.Get("plugin") != "" {
				return n, fmt.Errorf("暂不支持带插件的SS链接")
			}
		}
	}
	if n.Network == "" || n.Network == "raw" {
		n.Network = "tcp"
	}
	if n.Security == "" {
		n.Security = "none"
	}
	if n.Name == "" {
		n.Name = n.Protocol + " · " + net.JoinHostPort(n.Server, strconv.Itoa(n.Port))
	}
	if n.Server == "" || n.Port < 1 || n.Port > 65535 || strings.ContainsAny(n.Server, " /\\\x00") {
		return n, fmt.Errorf("节点地址或端口无效")
	}
	switch n.Protocol {
	case "vless", "vmess":
		if n.UUID == "" {
			return n, fmt.Errorf("缺少节点UUID")
		}
	case "trojan", "ss":
		if n.Password == "" {
			return n, fmt.Errorf("缺少节点密码")
		}
	case "socks5", "http":
	default:
		return n, fmt.Errorf("支持VLESS、VMess、Trojan、SS、SOCKS5和HTTP链接")
	}
	if n.Security != "none" && n.Security != "tls" && n.Security != "reality" {
		return n, fmt.Errorf("不支持的安全层")
	}
	if n.Security == "reality" && n.PublicKey == "" {
		return n, fmt.Errorf("REALITY链接缺少公钥")
	}
	return n, nil
}

func (n subscriptionNode) vmessLink() string {
	v := map[string]any{"v": "2", "ps": n.Name, "add": n.Server, "port": strconv.Itoa(n.Port), "id": n.UUID, "aid": strconv.Itoa(n.AlterID), "scy": n.Cipher, "net": n.Network, "type": "none", "host": n.Host, "path": n.Path, "tls": "", "sni": n.ServerName, "fp": n.Fingerprint, "alpn": strings.Join(n.ALPN, ",")}
	if n.Pin != "" {
		v["pinSHA256"] = n.Pin
	}
	if n.PublicKey != "" {
		v["pbk"] = n.PublicKey
		v["sid"] = n.ShortID
	}
	if n.Security != "none" {
		v["tls"] = n.Security
	}
	raw, _ := json.Marshal(v)
	return "vmess://" + base64.StdEncoding.EncodeToString(raw)
}

func uniqueSubLinks(links []string) []string {
	seen, out := map[string]bool{}, []string{}
	for _, link := range links {
		link = strings.TrimSpace(link)
		key := link
		if node, err := parseSubscriptionNode(link); err == nil {
			node.Name = ""
			node.Server = strings.ToLower(node.Server)
			if node.Protocol == "vless" || node.Protocol == "vmess" {
				node.UUID = strings.ToLower(node.UUID)
				node.Username = ""
			}
			if raw, err := json.Marshal(node); err == nil {
				key = string(raw)
			}
		}
		if link != "" && !seen[key] {
			seen[key] = true
			out = append(out, link)
		}
	}
	return out
}

func formatSubscription(links []string, target string) (string, string, error) {
	links = uniqueSubLinks(links)
	if len(links) == 0 {
		return "", "", fmt.Errorf("没有可用订阅节点")
	}
	if target == "links" || target == "base64" {
		return encodeSub(links, target), "text/plain; charset=utf-8", nil
	}
	if target == "v2ray" {
		for i, link := range links {
			if n, err := parseSubscriptionNode(link); err == nil && n.Protocol == "vmess" {
				links[i] = n.vmessLink()
			}
		}
		return encodeSub(uniqueSubLinks(links), "base64"), "text/plain; charset=utf-8", nil
	}
	nodes := []subscriptionNode{}
	names := []string{}
	taken := map[string]bool{"MIKU": true, "DIRECT": true}
	for _, link := range links {
		n, err := parseSubscriptionNode(link)
		if err != nil {
			return "", "", err
		}
		n.Name = uniqueRemark(n.Name, taken)
		taken[n.Name] = true
		nodes = append(nodes, n)
		names = append(names, n.Name)
	}
	if target == "clash" {
		var body strings.Builder
		body.WriteString("mixed-port: 7890\nallow-lan: false\nmode: rule\nlog-level: warning\nproxies:\n")
		for _, n := range nodes {
			value, err := n.clashProxy()
			if err != nil {
				return "", "", err
			}
			data, _ := json.Marshal(value)
			body.WriteString("  - " + string(data) + "\n")
		}
		group, _ := json.Marshal(map[string]any{"name": "MIKU", "type": "select", "proxies": names})
		body.WriteString("proxy-groups:\n  - " + string(group) + "\nrules:\n  - MATCH,MIKU\n")
		return body.String(), "application/yaml; charset=utf-8", nil
	}
	outbounds := []any{map[string]any{"type": "selector", "tag": "MIKU", "outbounds": names, "default": names[0]}}
	for _, n := range nodes {
		value, err := n.singBoxProxy()
		if err != nil {
			return "", "", err
		}
		outbounds = append(outbounds, value)
	}
	config := map[string]any{"log": map[string]any{"level": "warn"}, "inbounds": []any{map[string]any{"type": "mixed", "tag": "mixed-in", "listen": "127.0.0.1", "listen_port": 7890}}, "outbounds": outbounds, "route": map[string]any{"final": "MIKU"}}
	raw, err := json.MarshalIndent(config, "", "  ")
	return string(raw), "application/json; charset=utf-8", err
}

func (n subscriptionNode) clashProxy() (map[string]any, error) {
	typeName := n.Protocol
	if typeName == "socks5" {
		typeName = "socks5"
	}
	v := map[string]any{"name": n.Name, "type": typeName, "server": n.Server, "port": n.Port}
	switch n.Protocol {
	case "vless", "vmess":
		v["uuid"] = n.UUID
		v["udp"] = true
		v["network"] = n.Network
		if n.Protocol == "vmess" {
			v["alterId"] = n.AlterID
			v["cipher"] = n.Cipher
		} else {
			if n.Flow != "" {
				v["flow"] = n.Flow
			}
			v["packet-encoding"] = "xudp"
			if n.Encryption != "" && n.Encryption != "none" {
				return nil, fmt.Errorf("%s: 此VLESS加密方式请使用通用订阅", n.Name)
			}
		}
	case "trojan":
		v["password"] = n.Password
		v["udp"] = true
		v["network"] = n.Network
	case "ss":
		v["password"] = n.Password
		v["cipher"] = n.Cipher
		v["udp"] = true
	case "http", "socks5":
		if n.Username != "" {
			v["username"] = n.Username
			v["password"] = n.Password
		}
	}
	if n.Security != "none" {
		v["tls"] = true
		if n.Protocol == "trojan" || n.Protocol == "http" || n.Protocol == "socks5" {
			v["sni"] = n.ServerName
		} else {
			v["servername"] = n.ServerName
		}
		if n.Insecure {
			v["skip-cert-verify"] = true
		}
		if n.Fingerprint != "" {
			v["client-fingerprint"] = n.Fingerprint
		}
		if n.Pin != "" {
			v["fingerprint"] = strings.ReplaceAll(n.Pin, ":", "")
		}
		if len(n.ALPN) > 0 {
			v["alpn"] = n.ALPN
		}
		if n.Security == "reality" {
			v["reality-opts"] = map[string]any{"public-key": n.PublicKey, "short-id": n.ShortID}
		}
	}
	switch n.Network {
	case "tcp":
	case "ws", "httpupgrade":
		v["network"] = "ws"
		opts := map[string]any{"path": n.Path}
		if n.Host != "" {
			opts["headers"] = map[string]string{"Host": n.Host}
		}
		if n.Network == "httpupgrade" {
			opts["v2ray-http-upgrade"] = true
		}
		v["ws-opts"] = opts
	case "grpc":
		v["grpc-opts"] = map[string]any{"grpc-service-name": strings.TrimPrefix(n.Path, "/")}
	default:
		return nil, fmt.Errorf("%s: Clash格式不支持此传输，请使用通用/V2Ray订阅", n.Name)
	}
	return v, nil
}

func (n subscriptionNode) singBoxProxy() (map[string]any, error) {
	kind := n.Protocol
	if kind == "socks5" {
		kind = "socks"
	}
	if kind == "ss" {
		kind = "shadowsocks"
	}
	v := map[string]any{"type": kind, "tag": n.Name, "server": n.Server, "server_port": n.Port}
	switch n.Protocol {
	case "vless":
		v["uuid"] = n.UUID
		if n.Flow != "" {
			v["flow"] = n.Flow
		}
		if n.Encryption != "" && n.Encryption != "none" {
			return nil, fmt.Errorf("%s: 此VLESS加密方式请使用通用订阅", n.Name)
		}
	case "vmess":
		v["uuid"] = n.UUID
		v["security"] = n.Cipher
		v["alter_id"] = n.AlterID
	case "trojan":
		v["password"] = n.Password
	case "ss":
		v["password"] = n.Password
		v["method"] = n.Cipher
	case "socks5", "http":
		if n.Username != "" {
			v["username"] = n.Username
			v["password"] = n.Password
		}
		if n.Protocol == "socks5" {
			v["version"] = "5"
		}
	}
	if n.Security != "none" {
		tls := map[string]any{"enabled": true, "server_name": n.ServerName}
		if n.Insecure {
			tls["insecure"] = true
		}
		if len(n.ALPN) > 0 {
			tls["alpn"] = n.ALPN
		}
		if n.Fingerprint != "" {
			tls["utls"] = map[string]any{"enabled": true, "fingerprint": n.Fingerprint}
		}
		if n.Pin != "" {
			pin, err := hex.DecodeString(strings.ReplaceAll(n.Pin, ":", ""))
			if err != nil || len(pin) != 32 {
				return nil, fmt.Errorf("证书指纹格式无效")
			}
			tls["certificate_sha256"] = []string{base64.StdEncoding.EncodeToString(pin)}
		}
		if n.Security == "reality" {
			tls["reality"] = map[string]any{"enabled": true, "public_key": n.PublicKey, "short_id": n.ShortID}
		}
		v["tls"] = tls
	}
	switch n.Network {
	case "tcp":
	case "ws":
		tr := map[string]any{"type": "ws", "path": n.Path}
		if n.Host != "" {
			tr["headers"] = map[string]string{"Host": n.Host}
		}
		v["transport"] = tr
	case "grpc":
		v["transport"] = map[string]any{"type": "grpc", "service_name": strings.TrimPrefix(n.Path, "/")}
	case "httpupgrade":
		v["transport"] = map[string]any{"type": "httpupgrade", "host": n.Host, "path": n.Path}
	default:
		return nil, fmt.Errorf("%s: sing-box不支持此传输，请使用通用/V2Ray订阅", n.Name)
	}
	return v, nil
}
