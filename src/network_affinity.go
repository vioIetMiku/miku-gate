package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type NetworkAffinity struct {
	ASN         uint32 `json:"asn,omitempty"`
	CountryCode string `json:"country_code,omitempty"`
	Region      string `json:"region,omitempty"`
	City        string `json:"city,omitempty"`
	Location    string `json:"location,omitempty"`
}

func infoCountryCode(info IPInfo) string {
	// Ping0 describes the live tunnel and takes precedence over an older cache.
	if info.GeoSource == "ping0.cc" {
		if code := countryCodeFromLocation(info.Location); code != "" {
			return code
		}
	}
	if code := normalizeRegion(info.CountryCode); code != "" {
		return code
	}
	if code := countryCodeFromLocation(info.Location); code != "" {
		return code
	}
	return countryCodeFromLocation(info.Country)
}

func freshNetworkInfo(info IPInfo) bool {
	if infoCountryCode(info) == "" {
		return false
	}
	return !info.Checked.IsZero() && time.Since(info.Checked) < 24*time.Hour && info.Error == "" || info.GeoSource == "ping0.cc" && !info.GeoChecked.IsZero() && time.Since(info.GeoChecked) < 24*time.Hour
}

func (s *IPInspector) networkInfo(ip string) *IPInfo {
	info := s.get(ip)
	if info == nil || !freshNetworkInfo(*info) {
		return nil
	}
	return info
}

func (t *Tunnel) networkAffinity() NetworkAffinity {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.PreferredNetwork
}

func (t *Tunnel) rememberAffinity(info IPInfo) bool {
	code := infoCountryCode(info)
	if info.ASN == 0 || code == "" || t.preferredRegion() != "" && code != t.preferredRegion() {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.PreferredNetwork.ASN != 0 {
		return false
	}
	t.PreferredNetwork = NetworkAffinity{ASN: info.ASN, CountryCode: code, Region: info.RegionName, City: info.City, Location: info.Location}
	return true
}

func (m *Manager) rememberNetworkAffinity(t *Tunnel) {
	if m.inspector == nil {
		return
	}
	m.mu.RLock()
	ip := t.ExitIP
	m.mu.RUnlock()
	if info := m.inspector.networkInfo(ip); info != nil && t.rememberAffinity(*info) {
		_ = m.saveState()
	}
}

func affinityRank(preferred NetworkAffinity, info *IPInfo) int {
	if info == nil {
		return 5
	}
	samePlace := preferred.City != "" && strings.EqualFold(preferred.City, info.City) || preferred.Region != "" && strings.EqualFold(preferred.Region, info.RegionName) || preferred.Location != "" && preferred.Location == info.Location
	if preferred.ASN != 0 && preferred.ASN == info.ASN {
		if samePlace {
			return 0
		}
		return 1
	}
	if samePlace {
		return 2
	}
	if code := infoCountryCode(*info); code != "" && code == preferred.CountryCode {
		return 3
	}
	return 4
}

// At most one batch per five seconds (below the provider's 15/min limit).
// It reuses the existing IP cache, never publishes credentials or node links.
func (s *IPInspector) fillCandidateIdentities(ctx context.Context, addresses []string) {
	if s == nil || s.batchAPI == "" {
		return
	}
	wanted := map[string]bool{}
	ips := []string{}
	s.mu.Lock()
	for _, raw := range addresses {
		ip, err := publicLookupIP(raw)
		if err != nil || wanted[ip] {
			continue
		}
		if v, ok := s.cache[ip]; ok && v.ASN != 0 && freshNetworkInfo(v) {
			continue
		}
		wanted[ip] = true
		ips = append(ips, ip)
		if len(ips) == 6 {
			break
		}
	}
	if len(ips) == 0 || time.Now().Before(s.nextIdentityBatch) || time.Now().Before(s.blocked["ip-api.batch"]) {
		s.mu.Unlock()
		return
	}
	s.nextIdentityBatch = time.Now().Add(5 * time.Second)
	s.mu.Unlock()
	body, _ := json.Marshal(ips)
	req, err := http.NewRequestWithContext(ctx, "POST", s.batchAPI+"?fields=status,query,as,asname,isp,org,country,countryCode,regionName,city,proxy,hosting,mobile", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(req)
	if err != nil {
		return
	}
	defer response.Body.Close()
	if response.StatusCode == 429 || response.Header.Get("X-Rl") == "0" {
		seconds, _ := strconv.Atoi(response.Header.Get("X-Ttl"))
		if retry, e := strconv.Atoi(response.Header.Get("Retry-After")); e == nil && retry > seconds {
			seconds = retry
		}
		if seconds < 1 {
			seconds = 60
		}
		s.mu.Lock()
		if s.blocked == nil {
			s.blocked = map[string]time.Time{}
		}
		s.blocked["ip-api.batch"] = time.Now().Add(time.Duration(seconds) * time.Second)
		s.mu.Unlock()
	}
	if response.StatusCode != 200 {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 128<<10))
	if err != nil || len(raw) >= 128<<10 {
		return
	}
	var data []map[string]json.RawMessage
	if json.Unmarshal(raw, &data) != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range data {
		ip := infoString(item["query"])
		if !wanted[ip] || infoString(item["status"]) != "success" {
			continue
		}
		info := s.cache[ip]
		info.IP, info.Family, info.Checked = ip, "IPv4", time.Now()
		if strings.Contains(ip, ":") {
			info.Family = "IPv6"
		}
		info.ASN, info.Organization = infoASN(item["as"]), infoString(item["asname"])
		info.Country, info.CountryCode = infoString(item["country"]), normalizeRegion(infoString(item["countryCode"]))
		info.RegionName, info.City = infoString(item["regionName"]), infoString(item["city"])
		info.ISP, info.Company = infoString(item["isp"]), infoString(item["org"])
		info.Hosting, info.Mobile, info.Proxy = infoBool(item["hosting"]), infoBool(item["mobile"]), infoBool(item["proxy"])
		info.Source, info.ASNSource, info.TypeSource = "ip-api.com", "ip-api.com", "ip-api.com"
		info.ClassificationVersion = ipClassificationVersion
		info.Error = ""
		if old, ok := s.cache[ip]; ok {
			mergePing0Geo(&info, old)
		}
		classifyIPInfo(&info)
		s.cache[ip] = info
	}
	s.saveCacheLocked()
}
