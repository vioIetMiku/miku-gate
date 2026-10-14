package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func findSingBox(dir string) (string, error) {
	for _, path := range []string{filepath.Join(dir, "bin", "sing-box"), "/usr/local/bin/sing-box", "/usr/bin/sing-box", "/usr/local/s-ui/bin/sing-box"} {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
			return path, nil
		}
	}
	return "", fmt.Errorf("未找到 sing-box，请在VPS运行 miku repair 补齐核心")
}

func base64KeyPublic(private string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(private)
	if err != nil {
		return "", err
	}
	key, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}

func generatedReality(spec NewInboundSpec) (*realityConfig, error) {
	dest := strings.TrimSpace(spec.Dest)
	if dest == "" {
		dest = "www.microsoft.com:443"
	}
	host, _, err := net.SplitHostPort(dest)
	if err != nil {
		return nil, fmt.Errorf("REALITY目标需填写 域名:端口")
	}
	names := []string{}
	for _, name := range strings.Split(spec.ServerNames, ",") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		names = append(names, host)
	}
	if err := checkRealityDest(dest, names[0]); err != nil {
		return nil, err
	}
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	sid := strings.TrimSpace(spec.ShortID)
	if sid == "" {
		sid = randomShortID()
	}
	if raw, err := hex.DecodeString(sid); err != nil || len(raw) > 8 {
		return nil, fmt.Errorf("REALITY short ID应为最多16位、偶数长度的十六进制")
	}
	fp := strings.TrimSpace(spec.Fingerprint)
	if fp == "" {
		fp = "chrome"
	}
	return &realityConfig{Dest: dest, ServerNames: names, PrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()), PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), ShortIDs: []string{sid}, Fingerprint: fp}, nil
}

func buildSingBoxConfig(inbounds []*nativeInbound, tunnels []*Tunnel) (map[string]any, error) {
	live := map[string]bool{}
	outs := []any{map[string]any{"type": "direct", "tag": "direct"}}
	for _, tunnel := range tunnels {
		if tunnel.Status != "up" {
			continue
		}
		live[sanitizeTag(tunnel.Node.HostName)] = true
		cred := tunnel.credential()
		outs = append(outs, map[string]any{"type": "socks", "tag": tunnelTag(tunnel), "server": "127.0.0.1", "server_port": tunnel.Port, "version": "5", "username": cred.User, "password": cred.Pass})
	}
	ins, rules := []any{}, []any{}
	for _, ib := range inbounds {
		if !ib.Enable {
			continue
		}
		if ib.netOrTCP() == "xhttp" {
			return nil, fmt.Errorf("节点 %s 使用XHTTP，请选择Xray核心；sing-box不支持此传输", ib.Remark)
		}
		in := map[string]any{"type": ib.Protocol, "tag": ib.tag(), "listen": "0.0.0.0", "listen_port": ib.Port}
		users := []any{}
		for _, client := range ib.Clients {
			if !client.Enable {
				continue
			}
			user := map[string]any{"name": client.Email}
			if ib.Protocol == "trojan" {
				user["password"] = client.Password
			} else {
				user["uuid"] = client.ID
				if ib.Protocol == "vless" && client.Flow != "" {
					user["flow"] = client.Flow
				}
			}
			users = append(users, user)
		}
		in["users"] = users
		if ib.Security == "tls" && ib.TLS != nil {
			in["tls"] = map[string]any{"enabled": true, "server_name": ib.TLS.ServerName, "certificate_path": ib.TLS.CertFile, "key_path": ib.TLS.KeyFile}
		}
		if ib.Security == "reality" && ib.Reality != nil {
			host, port, err := net.SplitHostPort(ib.Reality.Dest)
			if err != nil || len(ib.Reality.ServerNames) == 0 {
				return nil, fmt.Errorf("节点REALITY目标无效")
			}
			var number int
			fmt.Sscanf(port, "%d", &number)
			in["tls"] = map[string]any{"enabled": true, "server_name": ib.Reality.ServerNames[0], "reality": map[string]any{"enabled": true, "handshake": map[string]any{"server": host, "server_port": number}, "private_key": ib.Reality.PrivateKey, "short_id": ib.Reality.ShortIDs}}
		}
		switch ib.netOrTCP() {
		case "ws":
			transport := map[string]any{"type": "ws", "path": ib.Path}
			if ib.Host != "" {
				transport["headers"] = map[string][]string{"Host": {ib.Host}}
			}
			in["transport"] = transport
		case "grpc":
			in["transport"] = map[string]any{"type": "grpc", "service_name": strings.TrimPrefix(ib.Path, "/")}
		case "httpupgrade":
			in["transport"] = map[string]any{"type": "httpupgrade", "path": ib.Path, "host": ib.Host}
		}
		ins = append(ins, in)
		for _, client := range ib.Clients {
			if !client.Enable || client.BoundTo == "" {
				continue
			}
			rule := map[string]any{"inbound": []string{ib.tag()}, "auth_user": []string{client.Email}, "action": "reject", "method": "drop"}
			if live[client.BoundTo] {
				rule = map[string]any{"inbound": []string{ib.tag()}, "auth_user": []string{client.Email}, "action": "route", "outbound": xuiTagPrefix + client.BoundTo}
			}
			rules = append(rules, rule)
		}
		if ib.BoundTo != "" {
			rule := map[string]any{"inbound": []string{ib.tag()}, "action": "reject", "method": "drop"}
			if live[ib.BoundTo] {
				rule = map[string]any{"inbound": []string{ib.tag()}, "action": "route", "outbound": xuiTagPrefix + ib.BoundTo}
			}
			rules = append(rules, rule)
		}
	}
	return map[string]any{"log": map[string]any{"level": "warn"}, "inbounds": ins, "outbounds": outs, "route": map[string]any{"rules": rules, "final": "direct"}}, nil
}

func (p *xrayProc) kind() string {
	if p.core == "sing-box" {
		return "sing-box"
	}
	return "xray"
}

func (p *xrayProc) verify(path string) error {
	if p.kind() != "sing-box" {
		return verifyXrayConfig(p.bin, path)
	}
	output, err := cmdCombined(exec.Command(p.bin, "check", "-c", path))
	if err != nil {
		return fmt.Errorf("sing-box配置校验失败: %s", trimOutput(output))
	}
	return nil
}
