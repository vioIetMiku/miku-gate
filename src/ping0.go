package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Ping0's documented free endpoint describes the caller, so it must run
// through the selected tunnel, bound to tun0, never through the host route.
func (t *Tunnel) probeExitDetails() (string, *IPInfo, error) {
	request := func(address string, seconds string) ([]byte, error) {
		out, err := cmdOutput(exec.Command("ip", "netns", "exec", t.nsName(), "curl",
			"-4", "--interface", "tun0", "--fail", "--silent", "--show-error",
			"--noproxy", "*", "--proto", "=https",
			"--max-time", seconds, "--max-filesize", "16384", "--write-out", "\nMIKU_CONNECT:%{time_connect}:%{time_namelookup}\n", address))
		if err != nil {
			return nil, err
		}
		body, ms, err := parseTunnelTiming(out)
		if err != nil {
			return nil, err
		}
		t.mu.Lock()
		t.lastProbeSample = &GateLatency{MS: &ms, Target: address, Method: "tunnel_tcp", Checked: time.Now()}
		t.mu.Unlock()
		return []byte(body), nil
	}
	if raw, err := request("https://ping0.cc/geo", "8"); err == nil {
		if info, err := parsePing0Geo(string(raw)); err == nil {
			return info.IP, &info, nil
		}
	}
	if raw, err := request("https://api.ipify.org", "10"); err == nil {
		if ip, err := publicLookupIP(strings.TrimSpace(string(raw))); err == nil {
			return ip, nil, nil
		}
	}
	return "", nil, fmt.Errorf("通过VPN查询出口IP失败，请检查隧道或稍后重连")
}

func parsePing0Geo(raw string) (IPInfo, error) {
	if len(raw) > 16<<10 {
		return IPInfo{}, fmt.Errorf("Ping0响应过大")
	}
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n")), "\n")
	if len(lines) != 4 {
		return IPInfo{}, fmt.Errorf("Ping0格式异常")
	}
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	ip, err := publicLookupIP(lines[0])
	asn, asErr := strconv.ParseUint(strings.TrimPrefix(lines[2], "AS"), 10, 32)
	if err != nil || asErr != nil || asn == 0 || !strings.HasPrefix(lines[2], "AS") || lines[1] == "" || lines[3] == "" || len(lines[1]) > 512 || len(lines[3]) > 512 || strings.ContainsAny(lines[1]+lines[3], "<>\x00") {
		return IPInfo{}, fmt.Errorf("Ping0内容无效")
	}
	info := IPInfo{IP: ip, ASN: uint32(asn), Organization: lines[3], Location: lines[1], CountryCode: countryCodeFromLocation(lines[1]), GeoSource: "ping0.cc", ASNSource: "ping0.cc", GeoChecked: time.Now()}
	parts := strings.Fields(lines[1])
	if len(parts) > 1 {
		info.RegionName = parts[1]
	}
	if len(parts) > 2 {
		info.City = strings.Join(parts[2:], " ")
	}
	return info, nil
}

func mergePing0Geo(value *IPInfo, geo IPInfo) {
	if value.IP != geo.IP || geo.GeoSource != "ping0.cc" || geo.ASN == 0 || time.Since(geo.GeoChecked) > 24*time.Hour {
		return
	}
	value.Location, value.GeoSource, value.GeoChecked = geo.Location, geo.GeoSource, geo.GeoChecked
	if code := infoCountryCode(geo); code != "" {
		value.CountryCode = code
	}
	if geo.RegionName != "" {
		value.RegionName = geo.RegionName
	}
	if geo.City != "" {
		value.City = geo.City
	}
	value.ASN, value.Organization, value.ASNSource = geo.ASN, geo.Organization, geo.ASNSource
}

func countryCodeFromLocation(location string) string {
	first := strings.Fields(location)
	if len(first) == 0 {
		return ""
	}
	for code, name := range countryCN {
		if first[0] == name {
			return code
		}
	}
	return ""
}

func (s *IPInspector) seedGeo(geo IPInfo) {
	if ip, err := publicLookupIP(geo.IP); err != nil || ip != geo.IP {
		return
	}
	s.mu.Lock()
	value, ok := s.cache[geo.IP]
	if !ok {
		value = IPInfo{IP: geo.IP, Family: "IPv4", Attribute: "未知（属性查询中）"}
	}
	mergePing0Geo(&value, geo)
	s.cache[geo.IP] = value
	s.saveCacheLocked()
	s.mu.Unlock()
	s.enqueue(geo.IP)
}
