package main

import (
	"encoding/base64"
	"encoding/csv"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const vpngateAPI = "https://www.vpngate.net/api/iphone/"

// vpngateMirror 是直连拿不到节点列表时的兜底（Cloudflare Worker 反代）。
// 用 FANOUT_VPNGATE_MIRROR 可以换成自己的地址，设成空字符串就只走直连。
const vpngateMirror = "https://p.xy.kg/vpngate"

// mirrorKey 只是让反代不被爬虫和端口扫描白嫖，不是安全边界。
const mirrorKey = "8rhIFzFKRJMFAe-xP5OQPclDEvSjKlHo"

func mirrorURL() string {
	if v, ok := os.LookupEnv("FANOUT_VPNGATE_MIRROR"); ok {
		return strings.TrimSpace(v)
	}
	return vpngateMirror
}

func mirrorAccessKey() string {
	if v, ok := os.LookupEnv("FANOUT_VPNGATE_MIRROR_KEY"); ok {
		return strings.TrimSpace(v)
	}
	return mirrorKey
}

// Node 是一个 VPN Gate 节点。
type Node struct {
	Pool           *PoolProfile `json:"-"`
	Source         string       `json:"source,omitempty"`
	Priority       int          `json:"-"`
	PrioritySource string       `json:"-"`
	HostName       string       `json:"hostname"`
	IP             string       `json:"ip"`
	Country        string       `json:"country"`
	CountryCode    string       `json:"country_code"`
	Ping           int          `json:"ping"`
	SpeedMbps      float64      `json:"speed_mbps"`
	Sessions       int          `json:"sessions"`
	Config         string       `json:"-"` // 解码后的 .ovpn 内容
	// Residential 表示这是志愿者的家庭宽带，不是 VPN Gate 自己的机房服务器。
	Residential bool `json:"residential"`
}

// vpngate 自营服务器所在的网段。这些是学术网络里的机房机器，
// 不是志愿者家宽：出口 IP 一眼能看出来是数据中心，而且更容易满员。
var vpngateOwnNets = []string{"219.100.37.0/24"}

// isResidential 判断一个节点是不是志愿者家宽。
//
// 两条判据都是实测出来的：
//   - hostname 以 public-vpn- 开头的是 vpngate 自营服务器
//   - 219.100.37.0/24 段同样是自营（学术网络机房）
//
// 剩下的是志愿者用自家宽带跑的，出口 IP 属于普通运营商。
func isResidential(hostName, ip string) bool {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(hostName)), "public-vpn-") {
		return false
	}
	addr := net.ParseIP(strings.TrimSpace(ip))
	if addr == nil {
		// IP 解析不出来时不敢断言，按家宽放过，让连通性测试去淘汰
		return true
	}
	for _, cidr := range vpngateOwnNets {
		_, block, err := net.ParseCIDR(cidr)
		if err == nil && block.Contains(addr) {
			return false
		}
	}
	return true
}

// fetchNodes 拉取并解析 VPN Gate 的节点列表。
// 先直连；连不上或者拿回来的内容不对（被拦截、返回门户页）就换反代再试一次。
// 返回的列表已按速度降序排列。
func fetchNodes(timeout time.Duration) ([]Node, error) {
	return fetchNodeSources(timeout)
}

// fetchNodesWith 把直连地址拆成参数，方便测试两条分支。
func fetchNodesWith(direct string, timeout time.Duration) ([]Node, error) {
	nodes, err := fetchNodesFrom(direct, "", timeout)
	if err == nil {
		return nodes, nil
	}
	mirror := mirrorURL()
	if mirror == "" {
		return nil, err
	}
	nodes, mirrorErr := fetchNodesFrom(mirror, mirrorAccessKey(), timeout)
	if mirrorErr != nil {
		return nil, fmt.Errorf("直连失败(%v)；反代也失败: %w", err, mirrorErr)
	}
	return nodes, nil
}

func fetchNodesFrom(url, key string, timeout time.Duration) ([]Node, error) {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("拉取节点列表失败: %w", err)
	}
	if key != "" {
		req.Header.Set("X-Fanout-Key", key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("拉取节点列表失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("拉取节点列表失败: HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20+1))
	if err != nil {
		return nil, fmt.Errorf("读取节点列表失败: %w", err)
	}
	if len(raw) > 32<<20 {
		return nil, fmt.Errorf("节点CSV超过32MiB")
	}
	nodes, err := parseNodeCSV(string(raw))
	for i := range nodes {
		nodes[i].Source = url
	}
	return nodes, err
}

// parseNodeCSV 解析 VPN Gate 的 CSV。首行是 "*vpn_servers"，
// 第二行是以 '#' 开头的表头，末行是 "*"。
func parseNodeCSV(body string) ([]Node, error) {
	var kept []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "*") {
			continue
		}
		kept = append(kept, strings.TrimPrefix(line, "#"))
	}
	if len(kept) < 2 {
		return nil, fmt.Errorf("节点列表格式异常: 有效行不足")
	}

	r := csv.NewReader(strings.NewReader(strings.Join(kept, "\n")))
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("解析节点 CSV 失败: %w", err)
	}

	header := records[0]
	idx := map[string]int{}
	for i, name := range header {
		idx[strings.TrimSpace(name)] = i
	}
	need := []string{"HostName", "IP", "CountryLong", "CountryShort", "Ping", "Speed", "OpenVPN_ConfigData_Base64"}
	for _, k := range need {
		if _, ok := idx[k]; !ok {
			return nil, fmt.Errorf("节点列表缺少字段 %s", k)
		}
	}

	var nodes []Node
	for _, rec := range records[1:] {
		get := func(k string) string {
			i := idx[k]
			if i >= len(rec) {
				return ""
			}
			return rec[i]
		}
		cfgB64 := get("OpenVPN_ConfigData_Base64")
		if cfgB64 == "" || get("HostName") == "" {
			continue
		}
		cfg, err := base64.StdEncoding.DecodeString(cfgB64)
		if err != nil {
			continue
		}
		if len(cfg) > 256<<10 || validateVPNProfile(string(cfg)) != nil {
			continue
		}
		ping, _ := strconv.Atoi(get("Ping"))
		speed, _ := strconv.ParseFloat(get("Speed"), 64)
		sessions, _ := strconv.Atoi(get("NumVpnSessions"))
		nodes = append(nodes, Node{
			HostName:    get("HostName"),
			IP:          get("IP"),
			Residential: isResidential(get("HostName"), get("IP")),
			Country:     get("CountryLong"),
			CountryCode: get("CountryShort"),
			Ping:        ping,
			SpeedMbps:   speed / 1e6,
			Sessions:    sessions,
			Config:      string(cfg),
		})
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("节点列表为空")
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].SpeedMbps > nodes[j].SpeedMbps })
	return nodes, nil
}
