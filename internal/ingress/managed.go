package ingress

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mikugate/internal/certassets"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"time"
)

type Template struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Cores              []string `json:"cores"`
	NeedsCertificate   bool     `json:"needs_certificate"`
	NeedsRealityTarget bool     `json:"needs_reality_target"`
}

func Templates() []Template {
	return []Template{
		{"vless-tls-vision", "VLESS + TCP + TLS + Vision", []string{"xray", "sing-box"}, true, false},
		{"vless-reality-vision", "VLESS + TCP + REALITY + Vision", []string{"xray", "sing-box"}, false, true},
		{"trojan-tls", "Trojan + TCP + TLS", []string{"xray", "sing-box"}, true, false},
	}
}

type ManagedRequest struct {
	Core            string `json:"core"`
	Template        string `json:"template"`
	Address         string `json:"address"`
	Listen          string `json:"listen"`
	Port            int    `json:"port"`
	ServerName      string `json:"server_name"`
	RealityHostname string `json:"reality_hostname,omitempty"`
}
type ManagedPlan struct {
	Request                    ManagedRequest `json:"request"`
	NeedsCertificate           bool           `json:"needs_certificate"`
	NeedsRealityTarget         bool           `json:"needs_reality_target"`
	UDPBlocked                 bool           `json:"udp_blocked"`
	PublicManagementConfigured bool           `json:"public_management_configured"`
	CoreValidated              bool           `json:"core_validated"`
	Applied                    bool           `json:"applied"`
	Warnings                   []string       `json:"warnings"`
}

func ManagedPlanFor(request ManagedRequest) (ManagedPlan, error) {
	r := request
	if r.Core != "xray" && r.Core != "sing-box" {
		return ManagedPlan{}, errors.New("请选择Xray或sing-box")
	}
	var selected *Template
	list := Templates()
	for i := range list {
		if list[i].ID == r.Template {
			selected = &list[i]
		}
	}
	if selected == nil {
		return ManagedPlan{}, errors.New("未知入口模板")
	}
	if r.Listen == "" {
		r.Listen = "0.0.0.0"
	}
	if net.ParseIP(r.Listen) == nil || r.Port < 1 || r.Port > 65535 {
		return ManagedPlan{}, errors.New("监听须为数字IP、端口1–65535")
	}
	if ip := net.ParseIP(r.Address); ip == nil {
		host, e := certassets.Hostname(r.Address)
		if e != nil {
			return ManagedPlan{}, errors.New("客户端连接地址需为域名或VPS数字IP，不接受URL")
		}
		r.Address = host
	}
	host, e := certassets.Hostname(r.ServerName)
	if e != nil {
		return ManagedPlan{}, errors.New("TLS/REALITY服务器名称需为规范域名")
	}
	r.ServerName = host
	if selected.NeedsRealityTarget {
		h, e := certassets.Hostname(r.RealityHostname)
		if e != nil || h != r.ServerName {
			return ManagedPlan{}, errors.New("REALITY目标域名与验证过的SNI名称必须一致")
		}
		r.RealityHostname = h
	} else if r.RealityHostname != "" {
		return ManagedPlan{}, errors.New("TLS模板不能混用REALITY目标")
	}
	return ManagedPlan{Request: r, NeedsCertificate: selected.NeedsCertificate, NeedsRealityTarget: selected.NeedsRealityTarget, UDPBlocked: true, Warnings: []string{
		"此入口是独立新配置，不覆盖已有面板/核心；端口冲突、核心版本校验、安装/运行账户和防火墙开放仍需确认。",
		"用户业务TCP只使用本机认证Miku出口；UDP显式阻断，出口停止不提供直连后备。管理HTTPS与客户端加密入口是不同配置。",
		"客户端导入信息包含入口凭据，须妥善保存；证书/REALITY私钥与本机代理密码只写私有服务端bundle，不返回浏览器。",
		"这是配置生成，不是已启动或已应用。低端口还需要专用服务权限；发行版/证书续期/核心升级与回滚继续开发。",
	}}, nil
}

type ManagedFiles struct {
	Certificate string
	PrivateKey  string
}
type ManagedBundle struct {
	Material     *ManagedMaterial    `json:"-"`
	Plan         ManagedPlan         `json:"plan"`
	ServerJSON   []byte              `json:"-"`
	ClientJSON   []byte              `json:"-"`
	URI          string              `json:"-"`
	Certificate  *certassets.Summary `json:"certificate,omitempty"`
	Reality      *RealityTarget      `json:"reality,omitempty"`
	ServerSHA256 string              `json:"server_sha256"`
	ClientSHA256 string              `json:"client_sha256"`
}

func GenerateManaged(request ManagedRequest, proxy Proxy, certificate *certassets.Asset, files ManagedFiles, target *RealityTarget) (ManagedBundle, error) {
	return generateManaged(request, proxy, certificate, files, target, nil)
}
func generateManaged(request ManagedRequest, proxy Proxy, certificate *certassets.Asset, files ManagedFiles, target *RealityTarget, identity *ManagedIdentity) (ManagedBundle, error) {
	plan, e := ManagedPlanFor(request)
	if e != nil {
		return ManagedBundle{}, e
	}
	r := plan.Request
	if e = proxy.Validate(); e != nil || r.Port == proxy.Port {
		return ManagedBundle{}, errors.New("Miku出口认证无效或与入口监听端口冲突")
	}
	if plan.NeedsCertificate {
		if certificate == nil || !certificate.Summary().ChainVerified || !certificate.Summary().KeyMatched || certificate.Summary().Hostname != r.ServerName || time.Until(certificate.Summary().NotAfter) < 24*time.Hour || !filepath.IsAbs(files.Certificate) || !filepath.IsAbs(files.PrivateKey) || files.Certificate == files.PrivateKey {
			return ManagedBundle{}, certassets.ErrCertificate
		}
	} else if target == nil || target.validate(r.RealityHostname) != nil {
		return ManagedBundle{}, ErrReality
	}
	var uuidRaw [16]byte
	if _, e = rand.Read(uuidRaw[:]); e != nil {
		return ManagedBundle{}, errors.New("入口凭据生成失败")
	}
	uuidRaw[6] = uuidRaw[6]&15 | 64
	uuidRaw[8] = uuidRaw[8]&63 | 128
	uuid := fmt.Sprintf("%x-%x-%x-%x-%x", uuidRaw[:4], uuidRaw[4:6], uuidRaw[6:8], uuidRaw[8:10], uuidRaw[10:])
	passwordRaw := make([]byte, 24)
	if _, e = rand.Read(passwordRaw); e != nil {
		return ManagedBundle{}, errors.New("入口凭据生成失败")
	}
	password := base64.RawURLEncoding.EncodeToString(passwordRaw)
	var publicKey, privateKey, shortID string
	if plan.NeedsRealityTarget {
		key, e := ecdh.X25519().GenerateKey(rand.Reader)
		if e != nil {
			return ManagedBundle{}, ErrReality
		}
		privateKey = base64.RawURLEncoding.EncodeToString(key.Bytes())
		publicKey = base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
		sid := make([]byte, 8)
		if _, e = rand.Read(sid); e != nil {
			return ManagedBundle{}, ErrReality
		}
		shortID = hex.EncodeToString(sid)
	}
	if identity != nil {
		if err := identity.Validate(plan.NeedsRealityTarget); err != nil {
			return ManagedBundle{}, err
		}
		uuid, password, privateKey, shortID = identity.UUID, identity.Password, identity.RealityPrivateKey, identity.ShortID
		if plan.NeedsRealityTarget {
			raw, err := base64.RawURLEncoding.DecodeString(privateKey)
			if err != nil {
				return ManagedBundle{}, ErrConfig
			}
			key, err := ecdh.X25519().NewPrivateKey(raw)
			if err != nil {
				return ManagedBundle{}, ErrConfig
			}
			publicKey = base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
		}
	}
	protocol := "vless"
	if r.Template == "trojan-tls" {
		protocol = "trojan"
	}
	var server, client map[string]any
	if r.Core == "xray" {
		settings := map[string]any{}
		user := map[string]any{}
		if protocol == "vless" {
			settings["decryption"] = "none"
			settings["clients"] = []any{map[string]any{"id": uuid, "flow": "xtls-rprx-vision"}}
			user = map[string]any{"id": uuid, "encryption": "none", "flow": "xtls-rprx-vision"}
		} else {
			settings["clients"] = []any{map[string]any{"password": password}}
			user = map[string]any{"password": password}
		}
		stream := map[string]any{"network": "raw", "security": "tls", "tlsSettings": map[string]any{"minVersion": "1.2", "alpn": []string{"h2", "http/1.1"}, "certificates": []any{map[string]any{"certificateFile": files.Certificate, "keyFile": files.PrivateKey}}}}
		clientStream := map[string]any{"network": "raw", "security": "tls", "tlsSettings": map[string]any{"serverName": r.ServerName, "allowInsecure": false, "alpn": []string{"h2", "http/1.1"}}}
		if plan.NeedsRealityTarget {
			stream = map[string]any{"network": "raw", "security": "reality", "realitySettings": map[string]any{"show": false, "target": net.JoinHostPort(target.Address, strconv.Itoa(target.Port)), "serverNames": []string{r.ServerName}, "privateKey": privateKey, "shortIds": []string{shortID}, "xver": 0}}
			clientStream = map[string]any{"network": "raw", "security": "reality", "realitySettings": map[string]any{"serverName": r.ServerName, "fingerprint": "chrome", "password": publicKey, "shortId": shortID, "spiderX": "/"}}
		}
		tag := "miku-managed-in"
		server = map[string]any{"log": map[string]any{"loglevel": "warning"}, "inbounds": []any{map[string]any{"tag": tag, "protocol": protocol, "listen": r.Listen, "port": r.Port, "settings": settings, "streamSettings": stream}}, "outbounds": []any{map[string]any{"tag": "miku-exit", "protocol": "socks", "settings": map[string]any{"servers": []any{map[string]any{"address": "127.0.0.1", "port": proxy.Port, "users": []any{map[string]any{"user": proxy.Username, "pass": proxy.Password}}}}}}, map[string]any{"tag": "block", "protocol": "blackhole", "settings": map[string]any{}}}, "routing": map[string]any{"domainStrategy": "AsIs", "rules": []any{map[string]any{"type": "field", "inboundTag": []string{tag}, "network": "udp", "outboundTag": "block"}, map[string]any{"type": "field", "inboundTag": []string{tag}, "network": "tcp", "outboundTag": "miku-exit"}, map[string]any{"type": "field", "network": "tcp,udp", "outboundTag": "block"}}}}
		outSettings := map[string]any{"vnext": []any{map[string]any{"address": r.Address, "port": r.Port, "users": []any{user}}}}
		if protocol == "trojan" {
			outSettings = map[string]any{"servers": []any{map[string]any{"address": r.Address, "port": r.Port, "password": password}}}
		}
		client = map[string]any{"log": map[string]any{"loglevel": "warning"}, "inbounds": []any{map[string]any{"tag": "local-client", "listen": "127.0.0.1", "port": 1080, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": true}}}, "outbounds": []any{map[string]any{"tag": "miku-server", "protocol": protocol, "settings": outSettings, "streamSettings": clientStream}}}
	} else {
		in := map[string]any{"tag": "miku-managed-in", "type": protocol, "listen": r.Listen, "listen_port": r.Port}
		out := map[string]any{"tag": "miku-server", "type": protocol, "server": r.Address, "server_port": r.Port}
		if protocol == "vless" {
			in["users"] = []any{map[string]any{"uuid": uuid, "flow": "xtls-rprx-vision"}}
			out["uuid"] = uuid
			out["flow"] = "xtls-rprx-vision"
		} else {
			in["users"] = []any{map[string]any{"password": password}}
			out["password"] = password
		}
		in["tls"] = map[string]any{"enabled": true, "min_version": "1.2", "alpn": []string{"h2", "http/1.1"}, "certificate_path": files.Certificate, "key_path": files.PrivateKey}
		out["tls"] = map[string]any{"enabled": true, "server_name": r.ServerName, "insecure": false, "alpn": []string{"h2", "http/1.1"}}
		if plan.NeedsRealityTarget {
			in["tls"] = map[string]any{"enabled": true, "server_name": r.ServerName, "reality": map[string]any{"enabled": true, "handshake": map[string]any{"server": target.Address, "server_port": target.Port}, "private_key": privateKey, "short_id": []string{shortID}}}
			out["tls"] = map[string]any{"enabled": true, "server_name": r.ServerName, "utls": map[string]any{"enabled": true, "fingerprint": "chrome"}, "reality": map[string]any{"enabled": true, "public_key": publicKey, "short_id": shortID}}
		}
		server = map[string]any{"log": map[string]any{"level": "warn"}, "inbounds": []any{in}, "outbounds": []any{map[string]any{"tag": "miku-exit", "type": "socks", "server": "127.0.0.1", "server_port": proxy.Port, "version": "5", "username": proxy.Username, "password": proxy.Password, "network": "tcp"}}, "route": map[string]any{"final": "miku-exit", "rules": []any{map[string]any{"network": "udp", "action": "reject", "method": "drop"}, map[string]any{"inbound": []string{"miku-managed-in"}, "network": "tcp", "action": "route", "outbound": "miku-exit"}, map[string]any{"action": "reject", "method": "drop"}}}}
		client = map[string]any{"log": map[string]any{"level": "warn"}, "inbounds": []any{map[string]any{"type": "socks", "listen": "127.0.0.1", "listen_port": 1080}}, "outbounds": []any{out}, "route": map[string]any{"final": "miku-server"}}
	}
	serverRaw, e := json.MarshalIndent(server, "", "  ")
	if e != nil {
		return ManagedBundle{}, ErrConfig
	}
	clientRaw, e := json.MarshalIndent(client, "", "  ")
	if e != nil {
		return ManagedBundle{}, ErrConfig
	}
	serverRaw = append(serverRaw, '\n')
	clientRaw = append(clientRaw, '\n')
	query := url.Values{"type": {"tcp"}, "security": {"tls"}, "sni": {r.ServerName}, "alpn": {"h2,http/1.1"}}
	credential := uuid
	if protocol == "vless" {
		query.Set("encryption", "none")
		query.Set("flow", "xtls-rprx-vision")
	} else {
		credential = password
	}
	if plan.NeedsRealityTarget {
		query.Set("security", "reality")
		query.Set("fp", "chrome")
		query.Set("pbk", publicKey)
		query.Set("sid", shortID)
		query.Del("alpn")
	}
	uri := (&url.URL{Scheme: protocol, User: url.User(credential), Host: net.JoinHostPort(r.Address, strconv.Itoa(r.Port)), RawQuery: query.Encode(), Fragment: "Miku Gate " + r.Core}).String()
	material := &ManagedMaterial{Request: r, Proxy: proxy, Identity: ManagedIdentity{uuid, password, privateKey, shortID}}
	if certificate != nil {
		material.CertificatePEM, material.PrivateKeyPEM = certificate.PEM()
	}
	if target != nil {
		copy := *target
		material.Target = &copy
	}
	bundle := ManagedBundle{Material: material, Plan: plan, ServerJSON: serverRaw, ClientJSON: clientRaw, URI: uri, ServerSHA256: digest(serverRaw), ClientSHA256: digest(clientRaw)}
	if certificate != nil {
		summary := certificate.Summary()
		bundle.Certificate = &summary
	}
	if target != nil {
		copy := *target
		bundle.Reality = &copy
	}
	return bundle, nil
}

// Useful for fixed metadata listings; never use it to print a secret bundle.
func (b ManagedBundle) MarshalJSON() ([]byte, error) {
	type safe ManagedBundle
	return json.Marshal(safe(b))
}
