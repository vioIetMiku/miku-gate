// Package ipintel queries fixed HTTPS metadata providers after explicit consent.
// Metadata traffic uses the host network, NEVER the business egress as a fallback.
// Unknown/provider errors/pending DNSBL results are not evidence of a clean IP.
package ipintel

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"mikugate/internal/egress"
)

const PathDescription = "VPS host-network HTTPS metadata lookup; not business traffic or proof of a VPN connection"

var ErrAddress = errors.New("归属查询仅允许规范公共IPv4；不会请求私网、元数据地址或用户提供的URL")

type Geography struct {
	State        string `json:"state"`
	Source       string `json:"source"`
	Country      string `json:"country,omitempty"`
	CountryCode  string `json:"country_code,omitempty"`
	Region       string `json:"region,omitempty"`
	City         string `json:"city,omitempty"`
	ISP          string `json:"isp,omitempty"`
	Organization string `json:"organization,omitempty"`
}
type ASN struct {
	Number uint32 `json:"number"`
	Holder string `json:"holder,omitempty"`
	Source string `json:"source"`
}
type Blocklist struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Operator  string `json:"operator,omitempty"`
	Status    string `json:"status"`
	QueryTime string `json:"query_time,omitempty"`
}
type Reputation struct {
	State        string      `json:"state"`
	Source       string      `json:"source"`
	Pending      bool        `json:"pending"`
	Results      []Blocklist `json:"results"`
	ListedCount  int         `json:"listed_count"`
	CheckedCount int         `json:"checked_count"`
}
type Report struct {
	IP          string     `json:"ip"`
	CheckedAt   time.Time  `json:"checked_at"`
	Cached      bool       `json:"cached"`
	NetworkPath string     `json:"network_path"`
	Prefix      string     `json:"prefix,omitempty"`
	ASNs        []ASN      `json:"asns"`
	Geography   Geography  `json:"geography"`
	Reputation  Reputation `json:"reputation"`
	Residential string     `json:"residential"`
	Warnings    []string   `json:"warnings"`
}
type entry struct {
	report  Report
	expires time.Time
}
type Client struct {
	mu    sync.Mutex
	cache map[string]entry
	fetch func(context.Context, string, any) error
}

func New() *Client {
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, MaxIdleConns: 4, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: 5 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || port != "443" || host != "stat.ripe.net" && host != "ipwho.is" {
			return nil, ErrAddress
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
		if err != nil || len(ips) == 0 || len(ips) > 16 {
			return nil, ErrAddress
		}
		for _, ip := range ips {
			if egress.PublicIPv4(ip.String()) != nil {
				return nil, ErrAddress
			}
		}
		return (&net.Dialer{Timeout: 4 * time.Second}).DialContext(ctx, "tcp4", net.JoinHostPort(ips[0].String(), port))
	}
	httpClient := &http.Client{Transport: transport, Timeout: 6 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("metadata redirect refused") }}
	c := &Client{cache: map[string]entry{}}
	c.fetch = func(ctx context.Context, address string, result any) error {
		target, err := url.Parse(address)
		if err != nil || target.Scheme != "https" || target.User != nil || target.Fragment != "" || target.Host != "stat.ripe.net" && target.Host != "ipwho.is" {
			return ErrAddress
		}
		req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "MikuGate-consented-IP-metadata/1")
		response, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return errors.New("metadata source unavailable")
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, (128<<10)+1))
		if err != nil || len(raw) > 128<<10 {
			return errors.New("metadata response exceeds budget")
		}
		return json.Unmarshal(raw, result)
	}
	return c
}
func text(value string) string {
	if len(value) > 256 {
		return ""
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return strings.TrimSpace(value)
}
func publicIPv4(raw string) (string, error) {
	ip, err := netip.ParseAddr(raw)
	if err != nil || !ip.Is4() || ip.String() != raw || egress.PublicIPv4(raw) != nil {
		return "", ErrAddress
	}
	return raw, nil
}
func number(raw json.RawMessage) uint32 {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		n, _ := strconv.ParseUint(str, 10, 32)
		return uint32(n)
	}
	var n uint32
	_ = json.Unmarshal(raw, &n)
	return n
}
func (c *Client) Lookup(ctx context.Context, raw string) (Report, error) {
	ip, err := publicIPv4(raw)
	if err != nil {
		return Report{}, err
	}
	c.mu.Lock()
	cached, ok := c.cache[ip]
	c.mu.Unlock()
	if ok && time.Now().Before(cached.expires) {
		report := cached.report
		report.Cached = true
		return report, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	report := Report{IP: ip, CheckedAt: time.Now().UTC(), NetworkPath: PathDescription, ASNs: []ASN{}, Geography: Geography{State: "unknown", Source: "IPWHOIS.IO free HTTPS"}, Reputation: Reputation{State: "unknown", Source: "RIPEstat DNS blocklists", Results: []Blocklist{}}, Residential: "unknown", Warnings: []string{"IP地理库/运营商名称不是住宅、独享或网站经营者所在地的证明。", "黑名单只覆盖该服务查询的DNSBL；未列出不保证其他风控/解锁/信誉，pending和失败都是未知。", "目标IP会发送给固定第三方数据源；查询走VPS宿主网络，不代表业务直连或VPN出口测试。"}}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); report.Geography = c.geo(ctx, ip) }()
	go func() { defer wg.Done(); report.Prefix, report.ASNs = c.network(ctx, ip) }()
	go func() { defer wg.Done(); report.Reputation = c.reputation(ctx, ip) }()
	wg.Wait()
	ttl := 15 * time.Minute
	if report.Reputation.Pending || report.Geography.State == "unknown" && len(report.ASNs) == 0 {
		ttl = time.Minute
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cache == nil {
		c.cache = map[string]entry{}
	}
	if len(c.cache) >= 128 {
		var oldest string
		var before time.Time
		for key, value := range c.cache {
			if oldest == "" || value.expires.Before(before) {
				oldest = key
				before = value.expires
			}
		}
		delete(c.cache, oldest)
	}
	c.cache[ip] = entry{report: report, expires: time.Now().Add(ttl)}
	return report, nil
}
func (c *Client) geo(ctx context.Context, ip string) Geography {
	result := Geography{State: "unknown", Source: "IPWHOIS.IO free HTTPS"}
	var response struct {
		Success    bool   `json:"success"`
		IP         string `json:"ip"`
		Country    string `json:"country"`
		Code       string `json:"country_code"`
		Region     string `json:"region"`
		City       string `json:"city"`
		Connection struct {
			ISP string `json:"isp"`
			Org string `json:"org"`
		} `json:"connection"`
	}
	if c.fetch(ctx, "https://ipwho.is/"+ip, &response) != nil || !response.Success || response.IP != ip || len(response.Code) != 2 {
		return result
	}
	result.State = "available"
	result.Country = text(response.Country)
	result.CountryCode = text(response.Code)
	result.Region = text(response.Region)
	result.City = text(response.City)
	result.ISP = text(response.Connection.ISP)
	result.Organization = text(response.Connection.Org)
	return result
}
func (c *Client) network(ctx context.Context, ip string) (string, []ASN) {
	var response struct {
		Status string `json:"status"`
		Data   struct {
			Prefix string            `json:"prefix"`
			ASNs   []json.RawMessage `json:"asns"`
		} `json:"data"`
	}
	result := []ASN{}
	if c.fetch(ctx, "https://stat.ripe.net/data/network-info/data.json?resource="+ip, &response) != nil || response.Status != "ok" {
		return "", result
	}
	prefix, err := netip.ParsePrefix(response.Data.Prefix)
	parsed, _ := netip.ParseAddr(ip)
	if err != nil || !prefix.Addr().Is4() || !prefix.Contains(parsed) || len(response.Data.ASNs) > 16 {
		return "", result
	}
	for _, raw := range response.Data.ASNs {
		n := number(raw)
		if n == 0 {
			continue
		}
		record := ASN{Number: n, Source: "RIPEstat RIS network-info"}
		if len(result) < 2 {
			var overview struct {
				Status string `json:"status"`
				Data   struct {
					Resource string `json:"resource"`
					Holder   string `json:"holder"`
				} `json:"data"`
			}
			if c.fetch(ctx, "https://stat.ripe.net/data/as-overview/data.json?resource=AS"+strconv.FormatUint(uint64(n), 10), &overview) == nil && overview.Status == "ok" && overview.Data.Resource == strconv.FormatUint(uint64(n), 10) {
				record.Holder = text(overview.Data.Holder)
			}
		}
		result = append(result, record)
	}
	return prefix.String(), result
}
func (c *Client) reputation(ctx context.Context, ip string) Reputation {
	result := Reputation{State: "unknown", Source: "RIPEstat DNS blocklists", Results: []Blocklist{}}
	var response struct {
		Status string `json:"status"`
		Data   struct {
			Pending    bool `json:"pending_results"`
			Parameters struct {
				Resource string `json:"resource"`
			} `json:"parameters"`
			Blocklists map[string]struct {
				Name     string `json:"name"`
				Operator string `json:"operator"`
			} `json:"blocklists"`
			Results map[string]struct {
				Status    string `json:"status"`
				QueryTime string `json:"query_time"`
			} `json:"data"`
		} `json:"data"`
	}
	if c.fetch(ctx, "https://stat.ripe.net/data/dns-blocklists/data.json?resource="+ip, &response) != nil || response.Status != "ok" || response.Data.Parameters.Resource != ip || len(response.Data.Results) > 32 || len(response.Data.Blocklists) > 32 {
		return result
	}
	result.Pending = response.Data.Pending
	failed := false
	for id, record := range response.Data.Results {
		info, known := response.Data.Blocklists[id]
		if !known || text(id) == "" {
			failed = true
			continue
		}
		state := record.Status
		switch state {
		case "listed":
			result.ListedCount++
			result.CheckedCount++
		case "notlisted":
			result.CheckedCount++
		default:
			state = "unknown"
			failed = true
		}
		result.Results = append(result.Results, Blocklist{ID: text(id), Name: text(info.Name), Operator: text(info.Operator), Status: state, QueryTime: text(record.QueryTime)})
	}
	sort.Slice(result.Results, func(i, j int) bool { return result.Results[i].ID < result.Results[j].ID })
	if result.ListedCount > 0 {
		result.State = "listed"
	} else if result.Pending {
		result.State = "pending"
	} else if !failed && result.CheckedCount > 0 && len(response.Data.Results) == len(response.Data.Blocklists) {
		result.State = "no_listings_in_checked_sources"
	}
	return result
}
