package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fixtureInspector(t *testing.T, handler http.HandlerFunc) *IPInspector {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &IPInspector{cache: map[string]IPInfo{}, pending: map[string]bool{}, queue: make(chan string, 8), blocked: map[string]time.Time{}, dir: t.TempDir(), client: server.Client(), ipapi: server.URL + "/primary/", isAPI: server.URL + "/secondary", whoAPI: server.URL + "/who/"}
}

func TestFreeIPDetectionClassifiesProxyResidenceWithoutDualTypes(t *testing.T) {
	useSettings(t)
	s := fixtureInspector(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/primary/121.82.126.229" {
			t.Errorf("unnecessary provider request: %s", r.URL.Path)
		}
		if !strings.Contains(r.URL.Query().Get("fields"), "hosting") {
			t.Error("network flags not requested")
		}
		fmt.Fprint(w, `{"status":"success","query":"121.82.126.229","as":"AS17511 OPTAGE Inc.","asname":"OPTAGE Inc.","isp":"OPTAGE Inc.","org":"OPTAGE Inc.","hosting":false,"mobile":false,"proxy":true}`)
	})
	s.lookup("121.82.126.229")
	info := s.get("121.82.126.229")
	if info.IPType != "residential" || info.TypeConfidence != "medium" || info.Proxy == nil || !*info.Proxy || info.Error != "" || info.ASNType != "" {
		t.Fatalf("proxy use prevented ordinary-provider classification: %+v", info)
	}
	// An old dev29 cache is reclassified immediately, then refreshed once.
	old := s.cache[info.IP]
	old.ClassificationVersion = 0
	s.cache[info.IP] = old
	s.enqueue(info.IP)
	s.enqueue(info.IP)
	if len(s.queue) != 1 {
		t.Fatal("legacy cache was not refreshed exactly once")
	}
}

func TestAnonymousSecondaryMissingFlagsDoesNotInventEvidence(t *testing.T) {
	useSettings(t)
	s := fixtureInspector(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/primary/8.8.8.8":
			fmt.Fprint(w, `{"status":"success","query":"8.8.8.8","as":"AS123 Example Cloud","asname":"Example Cloud","isp":"Example Cloud","hosting":false,"mobile":false,"proxy":true}`)
		case "/secondary":
			if r.Method != "GET" || r.URL.Query().Get("q") != "8.8.8.8" || r.URL.Query().Get("key") != "" {
				t.Error("anonymous request malformed")
			}
			fmt.Fprint(w, `{"ip":"8.8.8.8","is_vpn":true}`)
		default:
			t.Errorf("unexpected fallback %s", r.URL.Path)
		}
	})
	s.lookup("8.8.8.8")
	info := s.get("8.8.8.8")
	if info.IPType != "datacenter" || info.TypeConfidence != "low" || info.TypeSource != "ip-api.com" || info.Hosting == nil || *info.Hosting {
		t.Fatalf("short secondary response became false verification: %+v", info)
	}
}

func TestSecondaryFlagsAndFailedDetectionAreHandledHonestly(t *testing.T) {
	useSettings(t)
	for _, secondary := range []struct{ raw, want string }{
		{`{"ip":"8.8.8.8","is_datacenter":true,"is_mobile":false}`, "datacenter"},
		{`{"ip":"8.8.8.8","is_datacenter":false,"is_mobile":true}`, "mobile"},
		{`{"ip":"8.8.8.8","is_datacenter":false,"is_mobile":false,"asn":{"asn":123,"org":"Example ISP"}}`, "residential"},
	} {
		s := fixtureInspector(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/secondary" {
				fmt.Fprint(w, secondary.raw)
			} else {
				http.Error(w, "unavailable", 503)
			}
		})
		s.lookup("8.8.8.8")
		if info := s.get("8.8.8.8"); info.IPType != secondary.want || info.Error != "" {
			t.Fatalf("secondary fallback lost: %+v", info)
		}
	}
	s := fixtureInspector(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unavailable", 503) })
	s.lookup("8.8.8.8")
	if info := s.get("8.8.8.8"); info.IPType != "unknown" || info.Attribute != "检测失败" || info.Error == "" {
		t.Fatalf("failed providers fabricated ownership: %+v", info)
	}
	old := s.cache["8.8.8.8"]
	old.Checked = time.Now().Add(-2 * time.Minute)
	s.cache["8.8.8.8"] = old
	s.enqueue("8.8.8.8")
	if !s.pending["8.8.8.8"] {
		t.Fatal("failed classification remained cached for 24 hours")
	}
}

func TestIPAPIQuotaHeadersBackOffAndKeepCurrentResult(t *testing.T) {
	useSettings(t)
	s := fixtureInspector(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Rl", "0")
		w.Header().Set("X-Ttl", "120")
		fmt.Fprint(w, `{"status":"success","query":"8.8.8.8"}`)
	})
	data, err := s.request("ip-api.com", "GET", s.ipapi+"8.8.8.8", nil)
	if err != nil || infoString(data["status"]) != "success" || time.Until(s.blocked["ip-api.com"]) < time.Minute {
		t.Fatal("last valid result lost or quota ignored")
	}
	if _, err := s.request("ip-api.com", "GET", s.ipapi+"8.8.8.8", nil); err == nil {
		t.Fatal("request sent after quota exhausted")
	}
}

func TestPingAverageSupportsLinuxAndBusyBoxWithoutRounding(t *testing.T) {
	for _, raw := range []string{
		"rtt min/avg/max/mdev = 0.055/0.125/0.225/0.085 ms",
		"round-trip min/avg/max = 0.055/0.125/0.225 ms",
	} {
		ms, err := parsePingAverage(raw)
		if err != nil || ms != 0.125 {
			t.Fatal("submillisecond ping lost", ms, err)
		}
	}
	for _, raw := range []string{"100% packet loss", "rtt min/avg/max = 1/NaN/3 ms", "rtt min/avg/max = 1/-2/3 ms"} {
		if _, err := parsePingAverage(raw); err == nil {
			t.Fatal("invalid ping parsed as usable latency", raw)
		}
	}
}

func TestGateLatencyCacheSeparatesChangedServersAndClearsFailures(t *testing.T) {
	s := &LatencyInspector{cache: map[string]GateLatency{}, pending: map[string]bool{}, queue: make(chan string, 4), probe: func(string) (float64, error) { return 2.25, nil }}
	a := Node{IP: "198.51.100.10", Ping: 1}
	if initial := s.get(a); !initial.Pending || initial.MS != nil {
		t.Fatal("reference ping leaked into measurement", initial)
	}
	s.get(a)
	if len(s.queue) != 1 {
		t.Fatal("duplicate measurement queued")
	}
	s.measure(a.IP)
	if measured := s.get(a); measured.MS == nil || *measured.MS != 2.25 || measured.Method != "icmp" {
		t.Fatal("measurement not used", measured)
	}
	b := Node{IP: "198.51.100.11", Ping: 1}
	if changed := s.get(b); changed.MS != nil || !changed.Pending {
		t.Fatal("previous server measurement reused after switch", changed)
	}
	s.probe = func(string) (float64, error) { return 0, fmt.Errorf("no ICMP response") }
	s.measure(a.IP)
	if failed := s.get(a); failed.MS != nil || failed.Error == "" {
		t.Fatal("old successful ping survived new failure", failed)
	}
	if gatePingTarget(Node{IP: "127.0.0.1"}) != "" || gatePingTarget(Node{Config: "remote 203.0.113.5 443\n"}) != "203.0.113.5" {
		t.Fatal("wrong ping endpoint")
	}
	if gatePingTarget(Node{IP: "198.51.100.10", Config: "remote 203.0.113.5 443\n"}) != "203.0.113.5" {
		t.Fatal("list metadata displaced the active profile endpoint")
	}
}

func TestBrowserPingDisablesCacheAndAcceptsOnlyReadMethods(t *testing.T) {
	for _, method := range []string{"GET", "HEAD", "POST"} {
		rec := httptest.NewRecorder()
		apiBrowserPing(rec, httptest.NewRequest(method, "/api/ping", nil))
		want := 204
		if method == "POST" {
			want = 405
		}
		if rec.Code != want || rec.Header().Get("Cache-Control") != "no-store, max-age=0" || rec.Body.Len() != 0 {
			t.Fatal("browser ping can use cached or invalid response", rec)
		}
	}
}
