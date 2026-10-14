package main

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

func validPublishingHost(host string) bool {
	if host == "" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.To4() != nil
	}
	if len(host) > 253 || !strings.Contains(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`).MatchString(label) {
			return false
		}
	}
	return true
}

func publishingHost() string {
	if host := getWebSettings().PublicHost; host != "" {
		return host
	}
	return hostPublicIP()
}

func (s WebSettings) origin(host string) string {
	if s.PublicHost != "" {
		host = s.PublicHost
	}
	scheme := "http"
	if s.TLSCert != "" && s.TLSKey != "" {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(host, fmt.Sprint(s.Port))
}
