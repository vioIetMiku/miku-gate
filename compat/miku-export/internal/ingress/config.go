// Package ingress inspects and prepares existing core configurations without
// returning client identities, passwords, certificate keys or full raw JSON.
package ingress

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
)

const MaxConfig = 1 << 20

var ErrConfig = errors.New("入口配置不是受支持的有界 JSON，未展示原始内容")
var ErrSelection = errors.New("所选入口缺少唯一标识，或协议/传输/版本语法尚不支持受控复用")
var ErrChanged = errors.New("配置摘要不匹配；请重新检测并确认")

type Source struct {
	ID           string `json:"id"`
	Core         string `json:"core"`
	Path         string `json:"path"`
	PanelManaged bool   `json:"panel_managed"`
}

var sources = []Source{
	{"s-ui", "sing-box", "/usr/local/s-ui/bin/config.json", true},
	{"x-ui", "xray", "/usr/local/x-ui/bin/config.json", true},
	{"xray-local", "xray", "/usr/local/etc/xray/config.json", false},
	{"xray-etc", "xray", "/etc/xray/config.json", false},
	{"sing-box", "sing-box", "/etc/sing-box/config.json", false},
}

func Sources() []Source { return append([]Source(nil), sources...) }
func FindSource(id string) (Source, error) {
	for _, s := range sources {
		if id == s.ID {
			return s, nil
		}
	}
	return Source{}, errors.New("未知固定配置来源")
}

type Inbound struct {
	Listener  string `json:"listener"`
	ID        string `json:"id"`
	Tag       string `json:"tag"`
	Protocol  string `json:"protocol"`
	Listen    string `json:"listen"`
	Port      int    `json:"port"`
	Transport string `json:"transport"`
	Security  string `json:"security"`
	Clients   int    `json:"clients"`
	Vision    bool   `json:"vision"`
	Candidate bool   `json:"candidate"`
	Reason    string `json:"reason"`
}
type Report struct {
	Source        Source    `json:"source"`
	SHA256        string    `json:"sha256"`
	Inbounds      []Inbound `json:"inbounds"`
	ReadOnly      bool      `json:"read_only"`
	CoreValidated bool      `json:"core_validated"`
	Applied       bool      `json:"applied"`
	Warnings      []string  `json:"warnings"`
}

// parseJSON preserves all fields but rejects ambiguous duplicate keys, enormous
// token/depth budgets, comments/JSONC and trailing values. Parse errors do not
// include surrounding source bytes, which can contain passwords/private keys.
func parseJSON(raw []byte) (map[string]any, error) {
	if len(raw) == 0 || len(raw) > MaxConfig {
		return nil, ErrConfig
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	count := 0
	var walk func(int) (any, error)
	walk = func(depth int) (any, error) {
		count++
		if depth > 32 || count > 100000 {
			return nil, ErrConfig
		}
		t, e := d.Token()
		if e != nil {
			return nil, ErrConfig
		}
		delimiter, ok := t.(json.Delim)
		if !ok {
			return t, nil
		}
		switch delimiter {
		case '{':
			m := map[string]any{}
			for d.More() {
				key, e := d.Token()
				if e != nil {
					return nil, ErrConfig
				}
				k, ok := key.(string)
				if !ok || len(k) > 256 {
					return nil, ErrConfig
				}
				if _, exists := m[k]; exists {
					return nil, ErrConfig
				}
				v, e := walk(depth + 1)
				if e != nil {
					return nil, e
				}
				m[k] = v
			}
			end, e := d.Token()
			if e != nil || end != json.Delim('}') {
				return nil, ErrConfig
			}
			return m, nil
		case '[':
			a := []any{}
			for d.More() {
				v, e := walk(depth + 1)
				if e != nil {
					return nil, e
				}
				a = append(a, v)
			}
			end, e := d.Token()
			if e != nil || end != json.Delim(']') {
				return nil, ErrConfig
			}
			return a, nil
		}
		return nil, ErrConfig
	}
	value, e := walk(0)
	if e != nil {
		return nil, e
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ErrConfig
	}
	m, ok := value.(map[string]any)
	if !ok {
		return nil, ErrConfig
	}
	return m, nil
}
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func array(v any) []any           { a, _ := v.([]any); return a }
func text(v any) string           { s, _ := v.(string); return s }
func number(v any) int {
	n, ok := v.(json.Number)
	if !ok {
		return 0
	}
	i, e := strconv.Atoi(n.String())
	if e != nil {
		return 0
	}
	return i
}
func bounded(s string) bool {
	if s == "" || len(s) > 80 {
		return false
	}
	for _, c := range s {
		if c < 32 || c == 127 {
			return false
		}
	}
	return true
}
func digest(raw []byte) string { s := sha256.Sum256(raw); return hex.EncodeToString(s[:]) }
func entryID(sha string, index int) string {
	s := sha256.Sum256([]byte(sha + ":" + strconv.Itoa(index)))
	return hex.EncodeToString(s[:12])
}
func inspect(source Source, raw []byte, m map[string]any) (Report, error) {
	if source.Core != "xray" && source.Core != "sing-box" {
		return Report{}, ErrConfig
	}
	rows, ok := m["inbounds"].([]any)
	if !ok || len(rows) > 256 {
		return Report{}, ErrConfig
	}
	r := Report{Source: source, SHA256: digest(raw), Inbounds: []Inbound{}, ReadOnly: true, Warnings: []string{
		"仅解析严格 JSON；客户端 UUID/密码、私钥、原始配置不返回。检测到的地址/端口来自配置，不证明进程实际监听或拥有该端口。",
		"候选仅表示可生成定向路由，不代表核心版本兼容、节点可连通或已应用。核心校验、备份及用户应用确认仍必须执行。",
		"复用时客户端入口和密钥不变；仅选定入口 TCP 送入 Miku 本机认证代理，UDP 明确阻断，不绕回旧默认直连。",
	}}
	if source.PanelManaged {
		r.Warnings = append(r.Warnings, "此配置由 x-ui/3x-ui 生成；面板会重写它。不能仅修改生成文件视为持久复用，需面板持久化适配或迁移/并行新建。")
	}
	tags := map[string]int{}
	for _, row := range rows {
		tag := text(object(row)["tag"])
		if tag != "" {
			tags[tag]++
		}
	}
	for i, row := range rows {
		item := object(row)
		if item == nil {
			return Report{}, ErrConfig
		}
		b := Inbound{Listener: "unknown", ID: entryID(r.SHA256, i), Tag: text(item["tag"]), Listen: text(item["listen"]), Transport: "tcp", Security: "none", Reason: "尚未支持该入口或其标签/传输配置"}
		if source.Core == "xray" {
			b.Protocol = text(item["protocol"])
			b.Port = number(item["port"])
			if s, ok := item["port"].(string); ok {
				b.Port, _ = strconv.Atoi(s)
			}
			if b.Listen == "" {
				b.Listen = "0.0.0.0"
			}
			stream := object(item["streamSettings"])
			if t := text(stream["network"]); t != "" {
				b.Transport = t
			}
			if t := text(stream["security"]); t != "" {
				b.Security = t
			}
			clients := array(object(item["settings"])["clients"])
			b.Clients = len(clients)
			if b.Protocol == "shadowsocks" && text(object(item["settings"])["password"]) != "" {
				b.Clients = 1
			}
			for _, client := range clients {
				if text(object(client)["flow"]) == "xtls-rprx-vision" {
					b.Vision = true
				}
			}
			switch b.Protocol {
			case "vless", "vmess", "trojan", "shadowsocks":
				b.Candidate = true
			}
			switch b.Transport {
			case "tcp", "raw", "ws", "grpc", "httpupgrade", "xhttp":
			default:
				b.Candidate = false
			}
		} else {
			b.Protocol = text(item["type"])
			b.Port = number(item["listen_port"])
			if b.Listen == "" {
				b.Listen = "::"
			}
			if t := text(object(item["transport"])["type"]); t != "" {
				b.Transport = t
			}
			tls := object(item["tls"])
			if enabled, _ := tls["enabled"].(bool); enabled {
				b.Security = "tls"
			}
			if enabled, _ := object(tls["reality"])["enabled"].(bool); enabled {
				b.Security = "reality"
			}
			users := array(item["users"])
			b.Clients = len(users)
			if b.Protocol == "shadowsocks" && text(item["password"]) != "" {
				b.Clients = 1
			}
			for _, u := range users {
				if text(object(u)["flow"]) == "xtls-rprx-vision" {
					b.Vision = true
				}
			}
			switch b.Protocol {
			case "vless", "vmess", "trojan", "shadowsocks":
				b.Candidate = true
			}
			switch b.Transport {
			case "tcp", "ws", "grpc", "httpupgrade", "http":
			default:
				b.Candidate = false
			}
		}
		if text(item["detour"]) != "" {
			b.Candidate = false
		}
		if b.Clients == 0 {
			b.Candidate = false
		}
		if !bounded(b.Tag) || tags[b.Tag] != 1 || b.Port < 1 || b.Port > 65535 || net.ParseIP(b.Listen) == nil {
			b.Candidate = false
		}
		if !bounded(b.Tag) {
			b.Tag = ""
		}
		if !bounded(b.Listen) || net.ParseIP(b.Listen) == nil {
			b.Listen = "unknown"
		}
		switch b.Protocol {
		case "vless", "vmess", "trojan", "shadowsocks", "socks", "http", "mixed", "tun", "hysteria2", "tuic", "dokodemo-door":
		default:
			b.Protocol = "unsupported"
			b.Candidate = false
		}
		switch b.Security {
		case "none", "tls", "reality":
		default:
			b.Security = "unsupported"
			b.Candidate = false
		}
		switch b.Transport {
		case "tcp", "raw", "ws", "grpc", "httpupgrade", "xhttp", "http":
		default:
			b.Transport = "unsupported"
			b.Candidate = false
		}
		if b.Candidate {
			b.Reason = "可生成限定入口 TCP 候选；尚未进行核心校验或应用"
		}
		r.Inbounds = append(r.Inbounds, b)
	}
	return r, nil
}
func Inspect(source Source, raw []byte) (Report, error) {
	m, e := parseJSON(raw)
	if e != nil {
		return Report{}, e
	}
	return inspect(source, raw, m)
}

// Strings that are not part of the small allowed metadata set are never echoed.
func ValidSHA(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, e := hex.DecodeString(value)
	return e == nil
}
