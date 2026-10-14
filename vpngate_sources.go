package main

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type SourceReport struct {
	URL      string    `json:"url"`
	Count    int       `json:"count"`
	Error    string    `json:"error,omitempty"`
	Fetched  time.Time `json:"fetched"`
	Name     string    `json:"name,omitempty"`
	Priority int       `json:"priority,omitempty"`
}

var sourceReports struct {
	sync.RWMutex
	Values []SourceReport
}

func getSourceReports() []SourceReport {
	sourceReports.RLock()
	defer sourceReports.RUnlock()
	return append([]SourceReport{}, sourceReports.Values...)
}

func validSourceURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return false
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil {
		return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
	}
	host := strings.ToLower(u.Hostname())
	return host != "localhost" && !strings.HasSuffix(host, ".local") && !strings.HasSuffix(host, ".localhost")
}

func configuredSources() []string {
	sources := []string{"https://fastly.jsdelivr.net/gh/GeorgeXie2333/vpngate-list-mirror@latest/data/vpngate.csv", "https://gcore.jsdelivr.net/gh/GeorgeXie2333/vpngate-list-mirror@latest/data/vpngate.csv"}
	if mirror := mirrorURL(); mirror != "" {
		sources = append(sources, mirror)
	}
	sources = append(sources, getWebSettings().VPNGateSources...)
	for _, raw := range strings.FieldsFunc(os.Getenv("MIKU_VPNGATE_SOURCES"), func(c rune) bool { return c == '\n' || c == ',' }) {
		sources = append(sources, strings.TrimSpace(raw))
	}
	seen := map[string]bool{}
	out := []string{}
	for _, raw := range sources {
		if raw = strings.TrimSpace(raw); validSourceURL(raw) && !seen[raw] {
			seen[raw] = true
			out = append(out, raw)
		}
	}
	// The official source is requested only after the community phase and is
	// always the last entry, including when the user supplied it explicitly.
	filtered := out[:0]
	for _, source := range out {
		if source != vpngateAPI {
			filtered = append(filtered, source)
		}
	}
	out = append(filtered, vpngateAPI)
	return out
}

func fetchNodeSources(timeout time.Duration) ([]Node, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	sources := configuredSources()
	communityCtx, stopCommunity := context.WithTimeout(ctx, 32*time.Second)
	defer stopCommunity()
	type result struct {
		nodes  []Node
		report SourceReport
	}
	results := make(chan result, len(sources)+2)
	gate := make(chan struct{}, 4)
	pending := 0
	launch := func(source string) {
		pending++
		go func() {
			select {
			case gate <- struct{}{}:
			case <-communityCtx.Done():
				results <- result{report: SourceReport{URL: source, Error: "拉取超时", Fetched: time.Now()}}
				return
			}
			defer func() { <-gate }()
			key := ""
			if source == mirrorURL() {
				key = mirrorAccessKey()
			}
			nodes, err := fetchNodesFrom(source, key, 12*time.Second)
			for i := range nodes {
				nodes[i].Priority = 3
			}
			report := SourceReport{URL: source, Count: len(nodes), Fetched: time.Now(), Name: "社区CSV镜像", Priority: 3}
			if err != nil {
				report.Error = "来源不可用或CSV无效"
			}
			results <- result{nodes, report}
		}()
	}
	for _, source := range sources {
		if source != vpngateAPI {
			launch(source)
		}
	}
	pending++
	go func() {
		nodes, err := fetchGatePool(communityCtx)
		report := SourceReport{URL: gatePoolIndex, Count: len(nodes), Fetched: time.Now(), Name: "开源滚动池（近期TCP检测）", Priority: 1}
		if err != nil {
			report.Error = "滚动池不可用或校验失败"
		}
		results <- result{nodes, report}
	}()
	pending++
	go func() {
		nodes, err := fetchGateSnapshot(communityCtx)
		report := SourceReport{URL: gateSnapshotIndex, Count: len(nodes), Fetched: time.Now(), Name: "开源定时快照（提交与哈希校验）", Priority: 2}
		if err != nil {
			report.Error = "快照不可用、过期或校验失败"
		}
		results <- result{nodes, report}
	}()
	all := []Node{}
	reports := []SourceReport{}
	merge := func(nodes []Node) { all = mergeSourceNodes(all, nodes) }
	accept := func(value result) {
		rank := configuredSourceRank(value.report.URL)
		for i := range value.nodes {
			penalty := 0
			if value.nodes[i].Pool != nil && value.nodes[i].Priority >= 5 {
				penalty = 1
			}
			value.nodes[i].Priority = rank + penalty
			value.nodes[i].Source = value.report.URL
			value.nodes[i].PrioritySource = value.report.URL
		}
		value.report.Priority = rank
		reports = append(reports, value.report)
		merge(value.nodes)
	}
	for pending > 0 {
		select {
		case value := <-results:
			pending--
			accept(value)
		case <-communityCtx.Done():
			pending = 0
		}
	}
	stopCommunity()
	if ctx.Err() == nil {
		nodes, err := fetchNodesFrom(vpngateAPI, "", 12*time.Second)
		for i := range nodes {
			nodes[i].Priority = 9
		}
		report := SourceReport{URL: vpngateAPI, Count: len(nodes), Fetched: time.Now(), Name: "官方API（兜底）", Priority: 9}
		if err != nil {
			report.Error = "官方来源暂不可用"
		}
		accept(result{nodes, report})
	}
	sort.SliceStable(reports, func(i, j int) bool { return reports[i].Priority < reports[j].Priority })
	sourceReports.Lock()
	sourceReports.Values = reports
	sourceReports.Unlock()
	if len(all) == 0 {
		return nil, fmt.Errorf("全部VPN Gate来源拉取失败；可在设置增加CSV地址后重试")
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, b := nodePriority(all[i]), nodePriority(all[j])
		if a != b {
			return a < b
		}
		return all[i].SpeedMbps > all[j].SpeedMbps
	})
	return all, nil
}

func nodePriority(node Node) int {
	if node.Priority == 0 {
		return 3
	}
	return node.Priority
}

func mergeSourceNodes(all, nodes []Node) []Node {
	seen := map[string]int{}
	for i, node := range all {
		seen[strings.ToLower(node.HostName)] = i
	}
	for _, node := range nodes {
		key := strings.ToLower(node.HostName)
		index, exists := seen[key]
		if !exists {
			seen[key] = len(all)
			all = append(all, node)
			continue
		}
		old := all[index]
		if node.Config != "" && old.Pool != nil || nodePriority(node) < nodePriority(old) && (old.Config == "" || node.Config != "") {
			if nodePriority(old) < nodePriority(node) && old.IP == node.IP {
				node.Priority = old.Priority
				node.PrioritySource = old.PrioritySource
			}
			all[index] = node
		} else if nodePriority(node) < nodePriority(old) && old.IP == node.IP {
			all[index].Priority = node.Priority
			all[index].PrioritySource = node.PrioritySource
		}
	}
	return all
}

// Profiles from additional sources cannot run programs or write arbitrary
// root files. Only ordinary OpenVPN client directives and inline PKI are used.
func validateVPNProfile(raw string) error {
	if len(raw) > 256<<10 {
		return fmt.Errorf("OpenVPN配置过大")
	}
	block := ""
	allowed := map[string]bool{}
	for _, name := range strings.Fields("client dev dev-type proto remote remote-random remote-random-hostname port rport lport resolv-retry connect-retry connect-retry-max connect-timeout nobind persist-key persist-tun cipher data-ciphers data-ciphers-fallback auth auth-nocache auth-user-pass auth-retry remote-cert-tls remote-cert-ku remote-cert-eku verify-x509-name tls-client tls-version-min tls-version-max tls-cipher tls-ciphersuites verb mute mute-replay-warnings reneg-sec ping ping-restart ping-exit inactive keepalive comp-lzo compress allow-compression sndbuf rcvbuf fast-io explicit-exit-notify route route-ipv6 route-nopull redirect-gateway pull pull-filter route-delay route-metric route-noexec topology tun-mtu tun-mtu-extra link-mtu mssfix fragment disable-dco ignore-unknown-option tls-timeout tls-auth tls-crypt tls-crypt-v2 key-direction ca cert key ifconfig ifconfig-ipv6 dhcp-option replay-window hand-window tls-cert-profile tls-remote tls-exit register-dns block-outside-dns") {
		allowed[name] = true
	}
	denied := map[string]bool{"plugin": true, "config": true, "up": true, "down": true, "route-up": true, "route-pre-down": true, "tls-verify": true, "ipchange": true, "iproute": true, "script-security": true, "auth-user-pass-verify": true, "client-connect": true, "client-disconnect": true, "learn-address": true, "log": true, "log-append": true, "status": true, "writepid": true, "management": true, "management-client": true, "cd": true, "chroot": true, "daemon": true, "tls-export-cert": true, "askpass": true}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if block != "" {
			if line == "</"+block+">" {
				block = ""
			}
			continue
		}
		if strings.HasPrefix(line, "<") {
			name := strings.Trim(line, "<>")
			switch name {
			case "ca", "cert", "key", "tls-auth", "tls-crypt", "tls-crypt-v2":
				block = name
				continue
			default:
				return fmt.Errorf("OpenVPN配置包含不支持的内联块")
			}
		}
		fields := strings.Fields(line)
		name := strings.ToLower(strings.TrimLeft(strings.Trim(strings.ReplaceAll(fields[0], "\\", ""), "\"'"), "-"))
		if denied[name] || strings.HasPrefix(name, "management-") || strings.HasPrefix(name, "pkcs11-") {
			return fmt.Errorf("OpenVPN配置包含不允许的脚本/文件选项")
		}
		if !allowed[name] {
			return fmt.Errorf("OpenVPN配置包含非标准客户端选项")
		}
		if (name == "ca" || name == "cert" || name == "key" || name == "tls-auth" || name == "tls-crypt" || name == "tls-crypt-v2") && len(fields) > 1 {
			return fmt.Errorf("OpenVPN证书必须内联，不能引用本机文件")
		}
		if name == "remote" && len(fields) > 1 {
			if ip := net.ParseIP(fields[1]); ip != nil && (!ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback()) {
				return fmt.Errorf("VPN远端需要公网地址")
			}
		}
	}
	if block != "" {
		return fmt.Errorf("OpenVPN内联块未闭合")
	}
	return nil
}
