package ingress

import (
	"encoding/json"
	"errors"
	"strings"
)

type Proxy struct {
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}
type Plan struct {
	Source                 Source   `json:"source"`
	ConfigSHA256           string   `json:"config_sha256"`
	CandidateSHA256        string   `json:"candidate_sha256"`
	InboundID              string   `json:"inbound_id"`
	InboundTag             string   `json:"inbound_tag"`
	ProxyAddress           string   `json:"proxy_address"`
	ProxyPort              int      `json:"proxy_port"`
	OutboundTag            string   `json:"outbound_tag"`
	UDPBlocked             bool     `json:"udp_blocked"`
	OtherInboundsPreserved bool     `json:"other_inbounds_preserved"`
	CoreValidated          bool     `json:"core_validated"`
	Applied                bool     `json:"applied"`
	Warnings               []string `json:"warnings"`
}

func (p Proxy) Validate() error {
	if p.Port < 1024 || p.Port > 65535 || len(p.Username) < 1 || len(p.Username) > 255 || len(p.Password) < 1 || len(p.Password) > 255 || strings.ContainsAny(p.Username+p.Password, "\x00\r\n") {
		return errors.New("本机 SOCKS 认证材料无效")
	}
	return nil
}

// Prepare returns secret-bearing JSON only to local orchestration, never an API
// response. It preserves untouched fields and prepends exact inbound rules.
// No file writes, daemon restart or validation claims occur here.
func Prepare(source Source, raw []byte, expected, inboundID string, proxy Proxy) ([]byte, Plan, error) {
	if !ValidSHA(expected) || digest(raw) != expected {
		return nil, Plan{}, ErrChanged
	}
	if err := proxy.Validate(); err != nil {
		return nil, Plan{}, err
	}
	m, e := parseJSON(raw)
	if e != nil {
		return nil, Plan{}, e
	}
	report, e := inspect(source, raw, m)
	if e != nil {
		return nil, Plan{}, e
	}
	var selected *Inbound
	for i := range report.Inbounds {
		if report.Inbounds[i].ID == inboundID {
			selected = &report.Inbounds[i]
		}
	}
	if selected == nil || !selected.Candidate {
		return nil, Plan{}, ErrSelection
	}
	for _, key := range []string{"outbounds"} {
		if _, ok := m[key].([]any); !ok {
			return nil, Plan{}, ErrConfig
		}
	}
	// Reapplying against a config with our tag prefix needs explicit reconciliation,
	// not an extra duplicate route layer or silently changing an existing recipient.
	prefix := "miku-gate-"
	for _, row := range array(m["outbounds"]) {
		o := object(row)
		if o == nil {
			return nil, Plan{}, ErrConfig
		}
		if strings.HasPrefix(text(o["tag"]), prefix) {
			return nil, Plan{}, errors.New("配置已有 Miku 路由，需要受控更新而非重复添加")
		}
	}
	tag := prefix + inboundID
	plan := Plan{Source: source, ConfigSHA256: expected, InboundID: inboundID, InboundTag: selected.Tag, ProxyAddress: "127.0.0.1", ProxyPort: proxy.Port,
		OutboundTag: tag, UDPBlocked: true, OtherInboundsPreserved: true, Warnings: report.Warnings}
	if source.Core == "xray" {
		routing := object(m["routing"])
		if routing == nil {
			if m["routing"] != nil {
				return nil, Plan{}, ErrConfig
			}
			routing = map[string]any{}
		}
		rules := array(routing["rules"])
		if v, exists := routing["rules"]; exists && v != nil && rules == nil {
			return nil, Plan{}, ErrConfig
		}
		if _, exists := routing["rules"]; exists && routing["rules"] == nil {
			return nil, Plan{}, ErrConfig
		}
		// Xray SOCKS outbound remote domain routing is controlled by this tag rule;
		// leave other inbounds' domainStrategy and routing fields untouched.
		blocked := tag + "-udp-block"
		rules = append([]any{map[string]any{"type": "field", "inboundTag": []string{selected.Tag}, "network": "udp", "outboundTag": blocked},
			map[string]any{"type": "field", "inboundTag": []string{selected.Tag}, "network": "tcp", "outboundTag": tag}}, rules...)
		routing["rules"] = rules
		m["routing"] = routing
		m["outbounds"] = append(array(m["outbounds"]), map[string]any{"tag": tag, "protocol": "socks", "settings": map[string]any{"servers": []any{map[string]any{"address": "127.0.0.1", "port": proxy.Port, "users": []any{map[string]any{"user": proxy.Username, "pass": proxy.Password}}}}}}, map[string]any{"tag": blocked, "protocol": "blackhole", "settings": map[string]any{}})
	} else {
		route := object(m["route"])
		if route == nil {
			if m["route"] != nil {
				return nil, Plan{}, ErrConfig
			}
			route = map[string]any{}
		}
		rules := array(route["rules"])
		if v, exists := route["rules"]; exists && (v == nil || rules == nil) {
			return nil, Plan{}, ErrConfig
		}
		rules = append([]any{map[string]any{"inbound": []string{selected.Tag}, "network": "udp", "action": "reject", "method": "drop"},
			map[string]any{"inbound": []string{selected.Tag}, "network": "tcp", "action": "route", "outbound": tag}}, rules...)
		route["rules"] = rules
		m["route"] = route
		m["outbounds"] = append(array(m["outbounds"]), map[string]any{"tag": tag, "type": "socks", "server": "127.0.0.1", "server_port": proxy.Port, "version": "5", "username": proxy.Username, "password": proxy.Password, "network": "tcp"})
		plan.Warnings = append(plan.Warnings, "候选使用现代 sing-box route action 语法；必须通过所选真实核心 check，不能推定旧版兼容。")
	}
	candidate, e := json.MarshalIndent(m, "", "  ")
	if e != nil || len(candidate) > MaxConfig+16384 {
		return nil, Plan{}, ErrConfig
	}
	candidate = append(candidate, '\n')
	plan.CandidateSHA256 = digest(candidate)
	return candidate, plan, nil
}
