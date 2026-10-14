package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func sampleNodeCSV(host string) string {
	cfg := base64.StdEncoding.EncodeToString([]byte("client\nremote 1.2.3.4 443\n"))
	return "*vpn_servers\n" +
		"#HostName,IP,Score,Ping,Speed,CountryLong,CountryShort,NumVpnSessions,Uptime,TotalUsers,TotalTraffic,LogType,Operator,Message,OpenVPN_ConfigData_Base64\n" +
		fmt.Sprintf("%s,1.2.3.4,100,20,10000000,Japan,JP,3,1,1,1,2,op,,%s\n", host, cfg) +
		"*\n"
}

// 直连能用时不该碰反代。
func TestFetchNodesUsesDirectWhenHealthy(t *testing.T) {
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, sampleNodeCSV("direct-node"))
	}))
	defer direct.Close()
	mirrorHit := false
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorHit = true
		fmt.Fprint(w, sampleNodeCSV("mirror-node"))
	}))
	defer mirror.Close()

	t.Setenv("FANOUT_VPNGATE_MIRROR", mirror.URL)
	nodes, err := fetchNodesWith(direct.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("直连应该成功: %v", err)
	}
	if nodes[0].HostName != "direct-node" {
		t.Fatalf("拿到的不是直连结果: %s", nodes[0].HostName)
	}
	if mirrorHit {
		t.Fatal("直连成功时不应该请求反代")
	}
}

// 直连被拦截时回落到反代，并带上访问密钥。
func TestFetchNodesFallsBackToMirror(t *testing.T) {
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", http.StatusForbidden)
	}))
	defer direct.Close()
	gotKey := ""
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Fanout-Key")
		fmt.Fprint(w, sampleNodeCSV("mirror-node"))
	}))
	defer mirror.Close()

	t.Setenv("FANOUT_VPNGATE_MIRROR", mirror.URL)
	t.Setenv("FANOUT_VPNGATE_MIRROR_KEY", "test-key")
	nodes, err := fetchNodesWith(direct.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("反代应该兜住: %v", err)
	}
	if nodes[0].HostName != "mirror-node" {
		t.Fatalf("没走反代: %s", nodes[0].HostName)
	}
	if gotKey != "test-key" {
		t.Fatalf("反代请求没带密钥: %q", gotKey)
	}
}

// 直连返回 200 但内容是被劫持的门户页，也要回落。
func TestFetchNodesFallsBackOnGarbageBody(t *testing.T) {
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html><body>请先登录网络</body></html>")
	}))
	defer direct.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, sampleNodeCSV("mirror-node"))
	}))
	defer mirror.Close()

	t.Setenv("FANOUT_VPNGATE_MIRROR", mirror.URL)
	nodes, err := fetchNodesWith(direct.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("内容异常时应该回落: %v", err)
	}
	if nodes[0].HostName != "mirror-node" {
		t.Fatalf("没走反代: %s", nodes[0].HostName)
	}
}

// 反代被显式关掉时，直连失败就直接报错。
func TestFetchNodesMirrorDisabled(t *testing.T) {
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", http.StatusForbidden)
	}))
	defer direct.Close()

	t.Setenv("FANOUT_VPNGATE_MIRROR", "")
	if _, err := fetchNodesWith(direct.URL, 5*time.Second); err == nil {
		t.Fatal("关掉反代后应该直接失败")
	}
}
