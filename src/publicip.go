package main

import (
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// publicIPSources 是几个只回一行纯 IPv4 的接口，任意一个先返回就用它。
var publicIPSources = []string{
	"https://api.ipify.org",
	"https://ipv4.icanhazip.com",
	"https://ifconfig.me/ip",
}

var (
	publicIPMu       sync.Mutex
	publicIPOverride string    // 由 -ip / FANOUT_PUBLIC_IP 显式指定，优先级最高
	publicIPCache    string    // 上一次探测成功的结果
	publicIPAt       time.Time // 上次探测时间，用于 TTL
)

const publicIPTTL = 30 * time.Minute

// setPublicIPOverride 记录用户显式指定的母机公网地址，空值表示不覆盖。
func setPublicIPOverride(ip string) {
	publicIPMu.Lock()
	publicIPOverride = strings.TrimSpace(ip)
	publicIPMu.Unlock()
}

// hostPublicIP 返回跑 fanout 这台母机的公网 IPv4。
// 优先用显式覆盖值；否则用缓存（未过期）；再否则对外探测一次。
// 探测不到就返回空串，由调用方决定兜底。
func hostPublicIP() string {
	publicIPMu.Lock()
	if publicIPOverride != "" {
		ip := publicIPOverride
		publicIPMu.Unlock()
		return ip
	}
	if publicIPCache != "" && time.Since(publicIPAt) < publicIPTTL {
		ip := publicIPCache
		publicIPMu.Unlock()
		return ip
	}
	publicIPMu.Unlock()

	ip := probePublicIP()
	if ip == "" {
		// 探测失败时退回上一次的结果，比直接空着强
		publicIPMu.Lock()
		ip = publicIPCache
		publicIPMu.Unlock()
		return ip
	}

	publicIPMu.Lock()
	publicIPCache = ip
	publicIPAt = time.Now()
	publicIPMu.Unlock()
	return ip
}

// probePublicIP 逐个问外部接口，拿到第一个合法的 IPv4 就返回。
func probePublicIP() string {
	for _, url := range publicIPSources {
		// 必须从母机探：落在隧道里会拿到那条出口的 IP，分享链接就全写错了
		out, err := cmdOutput(exec.Command("curl", "-4", "-s", "--max-time", "5", url))
		if err != nil {
			continue
		}
		ip := strings.TrimSpace(string(out))
		if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() != nil {
			return ip
		}
	}
	return ""
}
