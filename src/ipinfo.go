package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type IPInfo struct {
	IP                    string    `json:"ip"`
	ASN                   uint32    `json:"asn"`
	Organization          string    `json:"organization"`
	ISP                   string    `json:"isp"`
	Company               string    `json:"company"`
	Network               string    `json:"network,omitempty"`
	Country               string    `json:"country"`
	CountryCode           string    `json:"country_code,omitempty"`
	RegionName            string    `json:"region_name,omitempty"`
	City                  string    `json:"city"`
	Location              string    `json:"location,omitempty"`
	GeoSource             string    `json:"geo_source,omitempty"`
	GeoChecked            time.Time `json:"geo_checked,omitempty"`
	ASNSource             string    `json:"asn_source,omitempty"`
	Family                string    `json:"family"`
	Attribute             string    `json:"attribute"`
	ASNType               string    `json:"asn_type,omitempty"`
	CompanyType           string    `json:"company_type,omitempty"`
	IPType                string    `json:"ip_type,omitempty"`
	TypeReason            string    `json:"type_reason,omitempty"`
	TypeConfidence        string    `json:"type_confidence,omitempty"`
	TypeSource            string    `json:"type_source,omitempty"`
	ClassificationVersion int       `json:"classification_version,omitempty"`
	Hosting               *bool     `json:"hosting,omitempty"`
	Mobile                *bool     `json:"mobile,omitempty"`
	Proxy                 *bool     `json:"proxy,omitempty"`
	VPN                   *bool     `json:"vpn,omitempty"`
	Tor                   *bool     `json:"tor,omitempty"`
	Source                string    `json:"source"`
	Checked               time.Time `json:"checked"`
	Error                 string    `json:"error,omitempty"`
	Pending               bool      `json:"pending,omitempty"`
}

const ipClassificationVersion = 2

var datacenterProvider = regexp.MustCompile(`(?i)\b(cloud|colo|colocation|data[ -]?center|hosting|servers?|vps|softether|google llc|amazon|microsoft corporation|cloudflare|digitalocean|hetzner|ovh|linode|akamai|oracle)\b`)

type IPInspector struct {
	mu                   sync.Mutex
	cache                map[string]IPInfo
	pending              map[string]bool
	queue                chan string
	dir                  string
	client               *http.Client
	ipapi, isAPI, whoAPI string
	blocked              map[string]time.Time
	typeKey              string // private; changing the key refreshes legacy type data
	batchAPI             string
	nextIdentityBatch    time.Time
}

func newIPInspector(dir string) *IPInspector {
	s := &IPInspector{cache: map[string]IPInfo{}, pending: map[string]bool{}, queue: make(chan string, 1024), dir: dir, blocked: map[string]time.Time{}, ipapi: "http://ip-api.com/json/", batchAPI: "http://ip-api.com/batch", isAPI: "https://api.ipapi.is/", whoAPI: "https://ipwho.is/", client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("IP服务不接受跳转") }}}
	if raw, err := os.ReadFile(filepath.Join(dir, "ip-cache.json")); err == nil && len(raw) < 8<<20 {
		_ = json.Unmarshal(raw, &s.cache)
		if s.cache == nil {
			s.cache = map[string]IPInfo{}
		}
	}
	go func() {
		for ip := range s.queue {
			s.lookup(ip)
			time.Sleep(2100 * time.Millisecond)
		}
	}()
	return s
}

func publicLookupIP(raw string) (string, error) {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return "", fmt.Errorf("请填写公网IPv4或IPv6地址")
	}
	return ip.String(), nil
}
func (s *IPInspector) get(ip string) *IPInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.cache[ip]
	if !ok {
		return nil
	}
	copy := value
	classifyIPInfo(&copy)
	copy.Pending = s.pending[ip] && copy.IPType == "unknown"
	return &copy
}
func (s *IPInspector) enqueue(raw string) {
	ip, err := publicLookupIP(raw)
	if err != nil {
		return
	}
	key := getWebSettings().IPInfoKey
	s.mu.Lock()
	defer s.mu.Unlock()
	if key != s.typeKey {
		s.typeKey = key
		for address, value := range s.cache {
			value.Checked = time.Time{}
			s.cache[address] = value
		}
	}
	if value, ok := s.cache[ip]; ok {
		ttl := 24 * time.Hour
		if value.Error != "" || value.IPType == "unknown" {
			ttl = time.Minute
		}
		if value.ClassificationVersion == ipClassificationVersion && time.Since(value.Checked) < ttl {
			return
		}
	}
	if s.pending[ip] {
		return
	}
	select {
	case s.queue <- ip:
		s.pending[ip] = true
	default:
	}
}

func (s *IPInspector) request(provider, method, address string, body []byte) (map[string]json.RawMessage, error) {
	s.mu.Lock()
	blocked := time.Now().Before(s.blocked[provider])
	s.mu.Unlock()
	if blocked {
		return nil, fmt.Errorf("IP信息来源额度暂不可用")
	}
	req, err := http.NewRequest(method, address, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("IP信息来源地址无效")
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("IP信息来源暂不可达")
	}
	defer response.Body.Close()
	// IP-API advertises the remaining quota even on successful responses.
	if provider == "ip-api.com" && response.Header.Get("X-Rl") == "0" {
		seconds, _ := strconv.Atoi(response.Header.Get("X-Ttl"))
		if seconds < 1 {
			seconds = 60
		}
		s.mu.Lock()
		s.blocked[provider] = time.Now().Add(time.Duration(seconds) * time.Second)
		s.mu.Unlock()
	}
	if response.StatusCode == 429 {
		delay := 24 * time.Hour
		if provider == "ip-api.com" {
			delay = time.Minute
		}
		if seconds, e := strconv.Atoi(response.Header.Get("Retry-After")); e == nil && seconds > 0 {
			delay = time.Duration(seconds) * time.Second
		} else if seconds, e := strconv.Atoi(response.Header.Get("X-Ttl")); e == nil && seconds > 0 {
			delay = time.Duration(seconds) * time.Second
		}
		s.mu.Lock()
		s.blocked[provider] = time.Now().Add(delay)
		s.mu.Unlock()
		return nil, fmt.Errorf("IP信息来源额度已用尽")
	}
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("IP信息来源HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 128<<10))
	if err != nil || len(raw) >= 128<<10 {
		return nil, fmt.Errorf("IP信息响应异常")
	}
	value := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &value) != nil {
		return nil, fmt.Errorf("IP信息格式异常")
	}
	return value, nil
}
func infoString(raw json.RawMessage) string {
	var value string
	_ = json.Unmarshal(raw, &value)
	return value
}
func infoBool(raw json.RawMessage) *bool { var v *bool; _ = json.Unmarshal(raw, &v); return v }
func infoObject(raw json.RawMessage) map[string]json.RawMessage {
	v := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &v)
	return v
}
func infoASN(raw json.RawMessage) uint32 {
	var n uint32
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	fields := strings.Fields(infoString(raw))
	if len(fields) == 0 {
		return 0
	}
	text := strings.TrimPrefix(strings.ToUpper(fields[0]), "AS")
	value, _ := strconv.ParseUint(text, 10, 32)
	return uint32(value)
}

func attributeOf(v IPInfo, reportedType string) string {
	if v.Hosting != nil && *v.Hosting {
		return "数据中心 / 机房"
	}
	if v.Mobile != nil && *v.Mobile {
		return "移动网络"
	}
	switch strings.ToLower(reportedType) {
	case "hosting":
		return "数据中心 / 机房"
	case "isp":
		return "运营商网络（住宅未确认）"
	case "education":
		return "教育网络"
	case "government":
		return "政府网络"
	case "business":
		return "企业网络"
	}
	if v.Hosting != nil && v.Mobile != nil && !*v.Hosting && !*v.Mobile {
		return "非机房 / 非移动（住宅未确认）"
	}
	return "未知（来源未提供分类）"
}

func normalizeIPType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "isp":
		return "isp"
	case "bus", "business":
		return "business"
	case "hosting", "datacenter", "data center":
		return "hosting"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

// Ownership is separate from proxy/VPN use. Ordinary provider networks are
// classified using the same consumer-network heuristic as AimiliVPN; reasons
// and confidence remain visible rather than claiming certified residential IPs.
func classifyIPInfo(v *IPInfo) {
	a, c := normalizeIPType(v.ASNType), normalizeIPType(v.CompanyType)
	v.IPType, v.Attribute, v.TypeReason, v.TypeConfidence = "unknown", "检测中", "等待网络属性和运营商信息", "low"
	if v.Mobile != nil && *v.Mobile {
		v.IPType, v.Attribute, v.TypeReason, v.TypeConfidence = "mobile", "移动网络", "来源标记为移动网络", "high"
	} else if v.Hosting != nil && *v.Hosting || a == "hosting" || c == "hosting" {
		v.IPType, v.Attribute, v.TypeReason = "datacenter", "机房 IP", "来源标记为机房或 Hosting"
		v.TypeConfidence = "high"
	} else if a == "isp" && c == "isp" || a == "business" && c == "isp" || a == "isp" && c == "business" {
		v.IPType, v.Attribute = "residential", "住宅 IP"
		v.TypeReason = "按 ASN / IP 归属类型 " + strings.ToUpper(a) + " + " + strings.ToUpper(c) + " 规则展示"
		v.TypeConfidence = "medium"
	} else if provider := strings.TrimSpace(v.ISP + " " + v.Organization + " " + v.Company); provider != "" {
		v.TypeConfidence = "medium"
		if datacenterProvider.MatchString(provider) {
			if v.Hosting != nil && v.Mobile != nil && !*v.Hosting && !*v.Mobile && v.TypeSource == "ipapi.is" {
				v.IPType, v.Attribute, v.TypeReason = "residential", "住宅 IP", "第二来源排除机房和移动标记，按普通网络规则推断"
			} else {
				v.IPType, v.Attribute, v.TypeReason, v.TypeConfidence = "datacenter", "机房 IP", "归属商包含机房关键词，按网络名称推断；可用第二来源复核", "low"
			}
		} else {
			v.IPType, v.Attribute, v.TypeReason = "residential", "住宅 IP", "有运营商信息且未标记机房或移动，按普通网络规则推断"
			if v.Hosting == nil || v.Mobile == nil {
				v.TypeConfidence = "low"
			}
		}
	} else if v.Error != "" {
		v.Attribute, v.TypeReason = "检测失败", "检测来源暂不可用，稍后自动重试"
	}
}

func (s *IPInspector) queryIPAPIIs(ip, key string) (map[string]json.RawMessage, error) {
	if key != "" {
		body, _ := json.Marshal(map[string]string{"q": ip, "key": key})
		return s.request("ipapi.is", "POST", s.isAPI, body)
	}
	separator := "?"
	if strings.Contains(s.isAPI, "?") {
		separator = "&"
	}
	return s.request("ipapi.is", "GET", s.isAPI+separator+"q="+url.QueryEscape(ip), nil)
}

func validIPAPIIs(data map[string]json.RawMessage, ip string) bool {
	return data != nil && (len(data["error"]) == 0 || string(data["error"]) == "null") && infoString(data["ip"]) == ip
}

func applyIPAPIIs(value *IPInfo, data map[string]json.RawMessage) {
	asn, company, location := infoObject(data["asn"]), infoObject(data["company"]), infoObject(data["location"])
	if n := infoASN(asn["asn"]); n != 0 {
		value.ASN = n
	}
	for _, field := range []struct {
		dst *string
		raw json.RawMessage
	}{
		{&value.Organization, asn["org"]}, {&value.Company, company["name"]},
		{&value.ISP, company["name"]}, {&value.Network, asn["route"]},
		{&value.Country, location["country"]}, {&value.City, location["city"]},
		{&value.CountryCode, location["country_code"]}, {&value.RegionName, location["state"]},
	} {
		if text := infoString(field.raw); text != "" {
			*field.dst = text
		}
	}
	if kind := infoString(asn["type"]); kind != "" {
		value.ASNType = normalizeIPType(kind)
	}
	if kind := infoString(company["type"]); kind != "" {
		value.CompanyType = normalizeIPType(kind)
	}
	for _, field := range []struct {
		dst **bool
		raw json.RawMessage
	}{
		{&value.Hosting, data["is_datacenter"]}, {&value.Mobile, data["is_mobile"]},
		{&value.Proxy, data["is_proxy"]}, {&value.VPN, data["is_vpn"]}, {&value.Tor, data["is_tor"]},
	} {
		if flag := infoBool(field.raw); flag != nil {
			*field.dst = flag
		}
	}
	if infoBool(data["is_datacenter"]) != nil && infoBool(data["is_mobile"]) != nil {
		value.TypeSource = "ipapi.is"
	}
	if value.Source == "" {
		value.Source = "ipapi.is"
	} else if !strings.Contains(value.Source, "ipapi.is") {
		value.Source += " + ipapi.is"
	}
}

func (s *IPInspector) lookup(ip string) {
	value := IPInfo{IP: ip, Checked: time.Now(), Family: "IPv6", ClassificationVersion: ipClassificationVersion}
	if net.ParseIP(ip).To4() != nil {
		value.Family = "IPv4"
	}
	var data map[string]json.RawMessage
	var err error
	key := getWebSettings().IPInfoKey
	if key != "" {
		data, err = s.queryIPAPIIs(ip, key)
		if err == nil && validIPAPIIs(data, ip) {
			applyIPAPIIs(&value, data)
		}
	}
	if value.Source == "" || value.Hosting == nil || value.Mobile == nil {
		data, err = s.request("ip-api.com", "GET", s.ipapi+url.PathEscape(ip)+"?fields=status,message,query,as,asname,isp,org,country,countryCode,regionName,city,proxy,hosting,mobile", nil)
		if err == nil && infoString(data["status"]) == "success" && infoString(data["query"]) == ip {
			primary := IPInfo{ASN: infoASN(data["as"]), Organization: infoString(data["asname"]), ISP: infoString(data["isp"]), Company: infoString(data["org"]), Country: infoString(data["country"]), City: infoString(data["city"]), Hosting: infoBool(data["hosting"]), Mobile: infoBool(data["mobile"]), Proxy: infoBool(data["proxy"])}
			primary.CountryCode, primary.RegionName = normalizeRegion(infoString(data["countryCode"])), infoString(data["regionName"])
			if value.Source == "" {
				primary.IP, primary.Family, primary.Checked, primary.ClassificationVersion = value.IP, value.Family, value.Checked, value.ClassificationVersion
				value = primary
				value.Source = "ip-api.com"
				value.TypeSource = "ip-api.com"
			} else {
				if value.ASN == 0 {
					value.ASN = primary.ASN
				}
				if value.ISP == "" {
					value.ISP = primary.ISP
				}
				if value.Organization == "" {
					value.Organization = primary.Organization
				}
				if value.CountryCode == "" {
					value.CountryCode = primary.CountryCode
				}
				if value.RegionName == "" {
					value.RegionName = primary.RegionName
				}
				if value.Hosting == nil {
					value.Hosting = primary.Hosting
				}
				if value.Mobile == nil {
					value.Mobile = primary.Mobile
				}
				if value.Proxy == nil {
					value.Proxy = primary.Proxy
				}
				value.Source += " + ip-api.com"
			}
		}
	}
	// Anonymous ipapi.is is useful for ambiguous ownership even without a key.
	// Missing fields in its short response must not overwrite primary flags.
	if key == "" && (value.Source == "" || value.Hosting == nil || value.Mobile == nil || datacenterProvider.MatchString(value.ISP+" "+value.Organization+" "+value.Company)) {
		data, err = s.queryIPAPIIs(ip, "")
		if err == nil && validIPAPIIs(data, ip) {
			applyIPAPIIs(&value, data)
		}
	}
	if value.ASN == 0 {
		data, err = s.request("ipwho.is", "GET", s.whoAPI+url.PathEscape(ip), nil)
		if err == nil && infoBool(data["success"]) != nil && *infoBool(data["success"]) && infoString(data["ip"]) == ip {
			connection := infoObject(data["connection"])
			value.ASN = infoASN(connection["asn"])
			value.Organization = infoString(connection["org"])
			value.ISP = infoString(connection["isp"])
			value.Company = value.ISP
			value.Country = infoString(data["country"])
			value.CountryCode = normalizeRegion(infoString(data["country_code"]))
			value.RegionName = infoString(data["region"])
			value.City = infoString(data["city"])
			if value.Source == "" {
				value.Source = "ipwho.is"
			} else {
				value.Source += " + ipwho.is"
			}
		}
	}
	if value.Source == "" {
		value.Error = "IP检测来源暂不可达；稍后自动重试"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if geo, ok := s.cache[ip]; ok && geo.GeoSource == "ping0.cc" && time.Since(geo.GeoChecked) <= 24*time.Hour {
		mergePing0Geo(&value, geo)
		if value.ASN != 0 && value.Source != "" {
			value.Error = ""
		}
		if value.Source == "" {
			value.Error = "IP属性暂未获取；Ping0位置和ASN已就绪"
		}
	}
	if value.ASNSource == "" {
		value.ASNSource = value.Source
	}
	classifyIPInfo(&value)
	if value.IPType == "unknown" {
		value.Error = "未获取到可用的网络类型信息；稍后自动重试"
		classifyIPInfo(&value)
	}
	s.cache[ip] = value
	delete(s.pending, ip)
	if len(s.cache) > 5000 {
		oldest := ip
		for k, v := range s.cache {
			if v.Checked.Before(s.cache[oldest].Checked) {
				oldest = k
			}
		}
		delete(s.cache, oldest)
	}
	s.saveCacheLocked()
}

func (s *IPInspector) saveCacheLocked() {
	if raw, err := json.Marshal(s.cache); err == nil {
		path := filepath.Join(s.dir, "ip-cache.json")
		if os.WriteFile(path+".tmp", raw, 0600) == nil {
			_ = os.Rename(path+".tmp", path)
		}
	}
}

func apiIPInfo(s *IPInspector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip, err := publicLookupIP(r.URL.Query().Get("ip"))
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		s.enqueue(ip)
		if value := s.get(ip); value != nil {
			writeJSON(w, 200, value)
			return
		}
		writeJSON(w, 202, map[string]any{"ip": ip, "pending": true})
	}
}
