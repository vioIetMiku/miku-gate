package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const gatePoolIndex = "https://raw.githubusercontent.com/GeorgeXie2333/vpngate-list-mirror/main/pool/latest.json"

type PoolDescriptor struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}
type PoolProfile struct {
	Commit string
	File   PoolDescriptor
	Hash   string
	Bytes  int
}

func publicJSON(ctx context.Context, address string, limit int) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		return nil, err
	}
	response, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("公开目录暂不可达")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("目录HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, int64(limit)+1))
	if err != nil || len(raw) > limit {
		return nil, fmt.Errorf("目录大小无效")
	}
	return raw, nil
}

func verifiedPoolFile(ctx context.Context, commit string, descriptor PoolDescriptor) ([]byte, error) {
	if !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(commit) || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(descriptor.SHA256) || descriptor.Bytes < 1 || descriptor.Bytes > 16<<20 {
		return nil, fmt.Errorf("滚动池描述无效")
	}
	if !regexp.MustCompile(`^pool/(?:servers\.json|countries\.json|configs/[a-f0-9]{64}\.json)$`).MatchString(descriptor.Path) {
		return nil, fmt.Errorf("滚动池路径无效")
	}
	return verifiedPublicMirrors(ctx, []string{"https://cdn.jsdelivr.net/gh/GeorgeXie2333/vpngate-list-mirror@" + commit + "/" + descriptor.Path, "https://raw.githubusercontent.com/GeorgeXie2333/vpngate-list-mirror/" + commit + "/" + descriptor.Path}, descriptor.Bytes, descriptor.SHA256)
}

// Race immutable mirrors, accepting only the exact pinned bytes. A fast
// corrupt response must never beat a slower verified copy.
func verifiedPublicMirrors(parent context.Context, addresses []string, size int, hash string) ([]byte, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	results := make(chan []byte, len(addresses))
	for _, address := range addresses {
		go func() {
			raw, err := publicJSON(ctx, address, size)
			if err != nil || len(raw) != size {
				results <- nil
				return
			}
			sum := sha256.Sum256(raw)
			if hex.EncodeToString(sum[:]) != hash {
				results <- nil
				return
			}
			results <- raw
		}()
	}
	for range addresses {
		select {
		case raw := <-results:
			if raw != nil {
				return raw, nil
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("镜像文件SHA256校验失败或不可达")
}

func fetchGatePool(ctx context.Context) ([]Node, error) {
	raw, err := publicJSON(ctx, gatePoolIndex, 64<<10)
	if err != nil {
		return nil, err
	}
	var index struct {
		Kind    string                    `json:"kind"`
		Version int                       `json:"schema_version"`
		Commit  string                    `json:"data_commit"`
		Fetched time.Time                 `json:"source_fetched_at"`
		Files   map[string]PoolDescriptor `json:"files"`
	}
	if json.Unmarshal(raw, &index) != nil || index.Kind != "vpngate-pool" || index.Version != 1 || index.Fetched.IsZero() || time.Since(index.Fetched) > 24*time.Hour || index.Fetched.After(time.Now().Add(5*time.Minute)) {
		return nil, fmt.Errorf("滚动池版本或源时间无效")
	}
	descriptor, ok := index.Files["pool/servers.json"]
	if !ok {
		return nil, fmt.Errorf("滚动池缺少节点目录")
	}
	descriptor.Path = "pool/servers.json"
	raw, err = verifiedPoolFile(ctx, index.Commit, descriptor)
	if err != nil {
		return nil, err
	}
	var catalog struct {
		Version int `json:"schema_version"`
		Servers []struct {
			Host     string    `json:"hostname"`
			IP       string    `json:"ip"`
			Country  string    `json:"country_name"`
			Code     string    `json:"country_code"`
			Ping     int       `json:"ping_ms"`
			Speed    float64   `json:"speed_bps"`
			Sessions int       `json:"num_vpn_sessions"`
			LastSeen time.Time `json:"last_seen_at"`
			Probe    struct {
				Status  string    `json:"status"`
				Checked time.Time `json:"checked_at"`
			} `json:"tcp_probe"`
			Hash   string         `json:"openvpn_config_sha256"`
			Bytes  int            `json:"openvpn_config_bytes"`
			Config PoolDescriptor `json:"config"`
		} `json:"servers"`
	}
	if json.Unmarshal(raw, &catalog) != nil || catalog.Version != 1 {
		return nil, fmt.Errorf("滚动池目录格式无效")
	}
	out := []Node{}
	for _, v := range catalog.Servers {
		if v.Host == "" || strings.ContainsAny(v.Host, "/\\\r\n\x00") || v.LastSeen.IsZero() || time.Since(v.LastSeen) > 7*24*time.Hour || v.Bytes < 1 || v.Bytes > 128<<10 || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(v.Hash) || v.Config.Path != "pool/configs/"+v.Hash+".json" {
			continue
		}
		priority := 5
		if !v.Probe.Checked.IsZero() && time.Since(v.Probe.Checked) <= 12*time.Hour && !v.Probe.Checked.After(time.Now().Add(5*time.Minute)) {
			if v.Probe.Status == "unreachable" {
				continue
			}
			if v.Probe.Status == "reachable" {
				priority = 1
			}
		}
		out = append(out, Node{HostName: v.Host, IP: v.IP, Country: v.Country, CountryCode: v.Code, Ping: v.Ping, SpeedMbps: v.Speed / 1e6, Sessions: v.Sessions, Residential: isResidential(v.Host, v.IP), Source: "VPN Gate滚动池", Priority: priority, Pool: &PoolProfile{Commit: index.Commit, File: v.Config, Hash: v.Hash, Bytes: v.Bytes}})
	}
	return out, nil
}

func resolveNodeProfile(node Node, dir string) (Node, error) {
	return resolveNodeProfileContext(context.Background(), node, dir)
}

func resolveNodeProfileContext(parent context.Context, node Node, dir string) (Node, error) {
	if node.Config != "" {
		return node, validateVPNProfile(node.Config)
	}
	if node.Pool == nil {
		return node, fmt.Errorf("节点缺少OpenVPN配置")
	}
	ref := node.Pool
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(ref.Hash) || ref.Bytes < 1 || ref.Bytes > 128<<10 {
		return node, fmt.Errorf("滚动池配置索引无效")
	}
	root := filepath.Join(dir, "pool-configs")
	if err := os.MkdirAll(root, 0700); err != nil {
		return node, err
	}
	path := filepath.Join(root, ref.Hash+".ovpn")
	valid := func(raw []byte) bool {
		sum := sha256.Sum256(raw)
		return len(raw) == ref.Bytes && hex.EncodeToString(sum[:]) == ref.Hash && validateVPNProfile(string(raw)) == nil
	}
	if raw, err := os.ReadFile(path); err == nil && valid(raw) {
		node.Config = string(raw)
		return node, nil
	}
	ctx, cancel := context.WithTimeout(parent, 25*time.Second)
	defer cancel()
	raw, err := verifiedPoolFile(ctx, ref.Commit, ref.File)
	if err != nil {
		return node, err
	}
	var config struct {
		Kind    string `json:"kind"`
		Version int    `json:"schema_version"`
		Base64  string `json:"openvpn_config_base64"`
		Hash    string `json:"openvpn_config_sha256"`
		Bytes   int    `json:"openvpn_config_bytes"`
	}
	if json.Unmarshal(raw, &config) != nil || config.Kind != "vpngate-pool-config" || config.Version != 1 || config.Hash != ref.Hash || config.Bytes != ref.Bytes {
		return node, fmt.Errorf("滚动池配置描述不一致")
	}
	decoded, err := base64.StdEncoding.DecodeString(config.Base64)
	if err != nil || !valid(decoded) {
		return node, fmt.Errorf("OpenVPN配置内容校验失败")
	}
	if err = os.WriteFile(path, decoded, 0600); err != nil {
		return node, err
	}
	node.Config = string(decoded)
	return node, nil
}
