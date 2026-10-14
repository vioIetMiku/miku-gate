package vpngate

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mikugate/internal/domain"
	"mikugate/internal/egress"
)

const Source = "https://www.vpngate.net/api/iphone/"
const MaxBytes = 8 << 20

// Configs are untrusted data. This validator is intentionally restrictive; no
// downloaded configuration is executed by this development release.
func ValidateConfig(config, expectedIP string) error {
	if len(config) == 0 || len(config) > 128*1024 {
		return errors.New("配置大小无效")
	}
	allowed := map[string]bool{"client": true, "dev": true, "proto": true, "remote": true, "resolv-retry": true, "nobind": true, "persist-key": true, "persist-tun": true, "remote-cert-tls": true, "cipher": true, "data-ciphers": true, "data-ciphers-fallback": true, "auth": true, "auth-nocache": true, "verb": true, "mute": true, "connect-retry": true, "connect-retry-max": true, "connect-timeout": true, "ping": true, "ping-restart": true, "reneg-sec": true, "auth-user-pass": true, "redirect-gateway": true, "route-delay": true, "comp-lzo": true, "setenv": true}
	block := ""
	seenRemote, seenClient, seenTun := false, false, false
	scanner := bufio.NewScanner(strings.NewReader(config))
	scanner.Buffer(make([]byte, 4096), 128*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if block != "" {
			if line == "</"+block+">" {
				block = ""
			} else if strings.ContainsAny(line, "<>") {
				return errors.New("无效内嵌证书块")
			}
			continue
		}
		if line == "<ca>" || line == "<cert>" || line == "<key>" {
			block = strings.Trim(line, "<>")
			continue
		}
		fields := strings.Fields(line)
		key := fields[0]
		if !allowed[key] {
			return fmt.Errorf("不允许的 OpenVPN 指令：%s", key)
		}
		if strings.ContainsAny(line, "\x00\r") {
			return errors.New("配置包含控制字符")
		}
		switch key {
		case "client":
			if len(fields) != 1 {
				return errors.New("client 参数无效")
			}
			seenClient = true
		case "dev":
			if len(fields) != 2 || fields[1] != "tun" {
				return errors.New("仅允许 tun 设备")
			}
			seenTun = true
		case "remote":
			if seenRemote || len(fields) < 3 || len(fields) > 4 || fields[1] != expectedIP {
				return errors.New("仅允许与节点 IP 一致的单个 remote")
			}
			if err := egress.PublicIPv4(fields[1]); err != nil {
				return err
			}
			p, err := strconv.Atoi(fields[2])
			if err != nil || p < 1 || p > 65535 {
				return errors.New("remote 端口无效")
			}
			if len(fields) == 4 && !protocol(fields[3]) {
				return errors.New("remote 协议无效")
			}
			seenRemote = true
		case "proto":
			if len(fields) != 2 || !protocol(fields[1]) {
				return errors.New("不支持的 VPN 协议")
			}
		case "auth-user-pass":
			if len(fields) != 1 {
				return errors.New("禁止外部认证文件")
			}
		case "remote-cert-tls":
			if len(fields) != 2 || fields[1] != "server" {
				return errors.New("证书角色必须是 server")
			}
		case "setenv":
			if len(fields) != 3 || fields[1] != "CLIENT_CERT" || fields[2] != "0" {
				return errors.New("不允许环境变量注入")
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if block != "" {
		return errors.New("内嵌证书块未闭合")
	}
	if !seenRemote || !seenClient || !seenTun {
		return errors.New("缺少 client/dev/remote 指令")
	}
	return nil
}
func protocol(v string) bool {
	return v == "udp" || v == "udp4" || v == "tcp" || v == "tcp4" || v == "tcp-client" || v == "tcp4-client"
}
func Parse(text string, now time.Time) ([]domain.Node, int, error) {
	if len(text) > MaxBytes {
		return nil, 0, errors.New("节点列表超过大小限制")
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	clean := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "*") || strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "#HostName,") {
			line = strings.TrimPrefix(line, "#")
		}
		clean = append(clean, line)
	}
	reader := csv.NewReader(strings.NewReader(strings.Join(clean, "\n")))
	header, err := reader.Read()
	if err != nil {
		return nil, 0, errors.New("节点列表不是有效 CSV")
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[h] = i
	}
	for _, key := range []string{"HostName", "IP", "CountryShort", "OpenVPN_ConfigData_Base64"} {
		if _, ok := idx[key]; !ok {
			return nil, 0, errors.New("节点列表缺少必要字段")
		}
	}
	nodes := []domain.Node{}
	skipped := 0
	seen := map[string]bool{}
	for count := 0; count < 5000; count++ {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, skipped, errors.New("CSV 记录损坏")
		}
		get := func(k string) string {
			i, ok := idx[k]
			if !ok || i >= len(row) {
				return ""
			}
			v := strings.TrimSpace(row[i])
			if k != "OpenVPN_ConfigData_Base64" && len(v) > 256 {
				return ""
			}
			return v
		}
		ip := get("IP")
		country := strings.ToUpper(get("CountryShort"))
		if len(country) != 2 || country[0] < 'A' || country[0] > 'Z' || country[1] < 'A' || country[1] > 'Z' {
			skipped++
			continue
		}
		config, err := base64.StdEncoding.DecodeString(get("OpenVPN_ConfigData_Base64"))
		if err != nil || ValidateConfig(string(config), ip) != nil {
			skipped++
			continue
		}
		hash := sha256.Sum256([]byte(get("HostName") + "|" + ip))
		id := hex.EncodeToString(hash[:12])
		if seen[id] {
			continue
		}
		seen[id] = true
		ping, _ := strconv.Atoi(get("Ping"))
		speed, _ := strconv.ParseInt(get("Speed"), 10, 64)
		sessions, _ := strconv.Atoi(get("NumVpnSessions"))
		nodes = append(nodes, domain.Node{ID: id, Hostname: get("HostName"), IP: ip, Country: country, CountryName: get("CountryLong"), AdvertisedPing: ping, AdvertisedSpeed: speed, Sessions: sessions, Source: Source, FetchedAt: now, Type: "unknown", Status: "untested", Config: string(config)})
		if len(nodes) >= 2000 {
			break
		}
	}
	if len(nodes) == 0 {
		return nil, skipped, errors.New("未找到通过格式与安全校验的节点；保留原缓存")
	}
	return nodes, skipped, nil
}
func Fetch(ctx context.Context) ([]domain.Node, int, error) {
	client := &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return errors.New("节点源重定向被拒绝") }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, Source, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "MikuGate/"+domain.Version)
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, errors.New("无法连接 VPN Gate 官方节点源；原缓存已保留")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("节点源返回 HTTP %d；原缓存已保留", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if err != nil {
		return nil, 0, err
	}
	return Parse(string(raw), time.Now().UTC())
}
