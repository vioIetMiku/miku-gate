package domain

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"unicode/utf8"

	"mikugate/internal/dnswire"
	"mikugate/internal/egress"
)

// UpstreamConfig is an internal decrypted value. Password never marshals to API JSON.
type UpstreamConfig struct {
	Kind           string `json:"kind"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	Password       string `json:"-"`
	TLSName        string `json:"tls_name"`
	DNS            string `json:"dns"`
	AllowLoopback  bool   `json:"allow_loopback"`
	AllowPlaintext bool   `json:"allow_plaintext"`
}

type UpstreamInput struct {
	Kind           string `json:"kind"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	ReusePassword  bool   `json:"reuse_password"`
	TLSName        string `json:"tls_name"`
	DNS            string `json:"dns"`
	AllowLoopback  bool   `json:"allow_loopback"`
	AllowPlaintext bool   `json:"allow_plaintext"`
}

type UpstreamView struct {
	UpstreamConfig
	HasPassword bool   `json:"has_password"`
	Configured  bool   `json:"configured"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

func (u UpstreamConfig) String() string {
	return fmt.Sprintf("%s upstream %s:%d (credentials redacted)", u.Kind, u.Host, u.Port)
}
func (u UpstreamInput) String() string { return "upstream input (credentials redacted)" }
func (u UpstreamConfig) View(updated string) UpstreamView {
	has := u.Password != ""
	u.Password = ""
	return UpstreamView{UpstreamConfig: u, HasPassword: has, Configured: true, UpdatedAt: updated}
}
func (u UpstreamInput) Config() UpstreamConfig {
	return UpstreamConfig{Kind: u.Kind, Host: u.Host, Port: u.Port, Username: u.Username, Password: u.Password, TLSName: u.TLSName, DNS: u.DNS, AllowLoopback: u.AllowLoopback, AllowPlaintext: u.AllowPlaintext}
}
func (u *UpstreamInput) Validate() error {
	if u.ReusePassword && u.Password != "" {
		return errors.New("不能同时提供新密码和复用旧密码")
	}
	config := u.Config()
	if err := config.validate(u.ReusePassword); err != nil {
		return err
	}
	u.Kind, u.Host, u.TLSName, u.DNS = config.Kind, config.Host, config.TLSName, config.DNS
	return nil
}
func (u *UpstreamConfig) Validate() error { return u.validate(false) }
func (u UpstreamConfig) SameCredentialTarget(other UpstreamConfig) bool {
	return u.Kind == other.Kind && u.Host == other.Host && u.Port == other.Port && u.Username == other.Username && u.TLSName == other.TLSName
}
func (u *UpstreamConfig) validate(reuse bool) error {
	u.Kind = strings.ToLower(strings.TrimSpace(u.Kind))
	if u.Kind != "socks5" && u.Kind != "http" && u.Kind != "https" {
		return errors.New("上游类型需为 SOCKS5、HTTP CONNECT 或 HTTPS CONNECT")
	}
	u.Host = strings.ToLower(strings.TrimSpace(u.Host))
	u.Host = strings.TrimSuffix(u.Host, ".")
	if u.Port < 1 || u.Port > 65535 {
		return errors.New("上游端口需在1–65535之间")
	}
	if ip, err := netip.ParseAddr(u.Host); err == nil {
		if !ip.Is4() || ip.String() != u.Host {
			return errors.New("上游地址需为规范IPv4或域名")
		}
		if ip.String() == "127.0.0.1" && u.AllowLoopback {
			if u.Port < 1024 {
				return errors.New("本机代理上游仅允许非特权端口")
			}
		} else if egress.PublicIPv4(ip.String()) != nil {
			return errors.New("上游不能指向私网、保留地址或元数据；本机127.0.0.1需显式确认")
		}
	} else if !PublicHostname(u.Host) {
		return errors.New("上游域名无效；请使用ASCII或punycode完整域名")
	}
	u.TLSName = strings.ToLower(strings.TrimSpace(u.TLSName))
	if u.Kind != "https" && u.TLSName != "" {
		return errors.New("仅HTTPS CONNECT支持TLS服务器名")
	}
	if u.TLSName != "" && !PublicHostname(u.TLSName) {
		return errors.New("TLS服务器名需为完整域名")
	}
	if u.Kind != "https" && u.Host != "127.0.0.1" && !u.AllowPlaintext {
		return errors.New("远程HTTP/SOCKS上游不加密，请明确确认明文传输风险或改用HTTPS")
	}
	if len(u.Username) > 255 || len(u.Password) > 255 || !utf8.ValidString(u.Username) || !utf8.ValidString(u.Password) {
		return errors.New("上游凭据最多255字节")
	}
	for _, s := range []string{u.Username, u.Password} {
		for _, c := range s {
			if c < 32 || c == 127 {
				return errors.New("上游凭据不能含控制字符")
			}
		}
	}
	if strings.Contains(u.Username, ":") {
		return errors.New("上游用户名不能包含冒号")
	}
	if reuse {
		if u.Username == "" {
			return errors.New("复用密码时用户名不能为空")
		}
	} else if (u.Username == "") != (u.Password == "") {
		return errors.New("上游用户名和密码必须同时填写或同时留空")
	}
	if u.DNS == "" {
		u.DNS = "cloudflare"
	}
	if u.DNS != "cloudflare" && u.DNS != "google" {
		return errors.New("业务DNS服务需为cloudflare或google；始终经指定上游访问")
	}
	return nil
}

// PublicHostname rejects local names and alternate numeric-IP spellings. Resolution
// and validation of every resulting address are still mandatory at the dial boundary.
func PublicHostname(host string) bool {
	if !dnswire.ValidHostname(host) || !strings.Contains(host, ".") {
		return false
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, suffix := range []string{".localhost", ".local", ".internal", ".lan", ".home", ".onion"} {
		if strings.HasSuffix(host, suffix) {
			return false
		}
	}
	last := host[strings.LastIndex(host, ".")+1:]
	if _, err := strconv.ParseUint(last, 10, 64); err == nil {
		return false
	}
	return !strings.HasPrefix(last, "0x")
}
