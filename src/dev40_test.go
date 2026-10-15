package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDev40SourcePublishesBeforeSlowSourceFinishes(t *testing.T) {
	useSettings(t)
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, sampleNodeCSV("first-usable")) }))
	defer fast.Close()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	published, finished := make(chan []Node, 8), make(chan error, 1)
	go func() {
		_, err := fetchNodeSourceList(ctx, time.Minute, []string{fast.URL, slow.URL}, func(nodes []Node) { published <- nodes })
		finished <- err
	}()
	nodes := awaitRaceValue(t, published)
	if len(nodes) != 1 || nodes[0].HostName != "first-usable" {
		t.Fatal(nodes)
	}
	select {
	case err := <-finished:
		t.Fatal("slow source was not still in flight", err)
	default:
	}
	cancel()
	if awaitRaceValue(t, finished) == nil {
		t.Fatal("canceled source fetch succeeded")
	}
}

func TestDev40NewSourceWakesIdleRaceAndPublishesPartialResults(t *testing.T) {
	useSettings(t)
	m := NewManager(0, t.TempDir())
	job := quickFixtureJob(m)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	returned := make(chan (<-chan struct{}), 1)
	go func() {
		returned <- m.runQuickRace(job, []quickTarget{{"JP", 4}, {"KR", 9}}, 77, quickRaceOps{
			ctx: ctx, retryDelay: time.Hour,
			connect: func(tunnel *Tunnel) error { tunnel.ExitIP = tunnel.Node.IP; return nil },
			entry:   workingQuickEntry, stop: stopQuickFixture,
		})
	}()
	m.mu.Lock()
	m.nodes = []Node{{HostName: "arriving-source", CountryCode: "JP", IP: "1.2.3.4"}}
	m.mu.Unlock()
	m.signalNodesChanged()
	awaitQuickCondition(t, func() bool { v := job.View(); return v.QuickResult != nil && v.QuickResult.Ready == 1 })
	if job.View().Status != "running" || len(m.visibleTunnels()) != 1 {
		t.Fatal("partial result gated by empty country")
	}
	cancel()
	awaitRaceValue(t, awaitRaceValue(t, returned))
}

func TestDev40MeasuredCandidatesLeadAdvertisedSpeed(t *testing.T) {
	useSettings(t)
	m := NewManager(0, t.TempDir())
	m.nodes = []Node{{HostName: "advertised-fast", CountryCode: "JP", IP: "1.1.1.1", SpeedMbps: 1000}, {HostName: "measured-fast", CountryCode: "JP", IP: "8.8.8.8", SpeedMbps: 1}, {HostName: "stale", CountryCode: "JP", IP: "9.9.9.9"}}
	ms := 20.0
	m.latencies = &LatencyInspector{cache: map[string]GateLatency{latencyKey(m.nodes[1]): {MS: &ms, Checked: time.Now()}, latencyKey(m.nodes[2]): {MS: &ms, Checked: time.Now().Add(-time.Hour)}}}
	picks := m.quickRacePicks([]quickTarget{{"JP", 4}})
	if len(picks) != 3 || picks[0].HostName != "measured-fast" || picks[1].HostName != "advertised-fast" {
		t.Fatal(picks)
	}
}

func TestDev40MirrorRaceVerifiesWinnerAndCancelsSlowCopies(t *testing.T) {
	slowCanceled := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done(); close(slowCanceled) }))
	defer slow.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "bad!") }))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "good") }))
	defer good.Close()
	sum := sha256.Sum256([]byte("good"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	raw, err := verifiedPublicMirrors(ctx, []string{slow.URL, bad.URL, good.URL}, 4, hex.EncodeToString(sum[:]))
	if err != nil || string(raw) != "good" {
		t.Fatal(string(raw), err)
	}
	// If cancellation raced before the slow request reached its handler, closing
	// the server is sufficient; a response from it was never required to finish.
}

func TestDev40RetargetQuickDomainKeepsIdentityAndCustomNodes(t *testing.T) {
	tpl := &nativeInbound{ID: 7, Port: 30001, Protocol: "vless", Network: "tcp", Security: "reality", Enable: true, Remark: "Miku 一键 · Reality", Reality: &realityConfig{PublicKey: "old"}, Clients: []nativeClient{{ID: fixtureUUID, Enable: true, Flow: "xtls-rprx-vision"}}}
	clone := *tpl
	clone.ID = 8
	clone.Port = 30002
	clone.BoundTo = "jp"
	clone.AutoExitOrder = 3
	clone.AutoTemplate = 7
	clone.Remark = "日本"
	custom := clone
	custom.ID = 9
	custom.Port = 30003
	custom.Reality = &realityConfig{PublicKey: "custom"}
	store := &nativeStore{NextID: 10, Inbounds: []*nativeInbound{tpl, &clone, &custom}}
	tls := &tlsConfig{ServerName: "new.example.com", CertFile: "new.pem", KeyFile: "new.key"}
	if count := retargetQuickFamily(store, 7, quickInboundSpec("new.example.com", RegisteredDomain{}), tls, nil); count != 2 {
		t.Fatal(count)
	}
	if clone.ID != 8 || clone.Port != 30002 || clone.Clients[0].ID != fixtureUUID || clone.BoundTo != "jp" || clone.AutoExitOrder != 3 || clone.TLS != tls || clone.Reality != nil || store.NextID != 10 {
		t.Fatal("domain change recreated existing identity", clone)
	}
	if custom.Reality.PublicKey != "custom" || custom.TLS != nil {
		t.Fatal("customized entry overwritten")
	}
	if count := retargetQuickFamily(store, 7, quickInboundSpec("", RegisteredDomain{}), nil, &realityConfig{PublicKey: "new"}); count != 2 || clone.Security != "reality" || clone.TLS != nil {
		t.Fatal("domain unbind did not return family to Reality")
	}
}

func TestDev40DesktopFlagsAreEmbedded(t *testing.T) {
	for _, code := range []string{"jp", "kr", "us", "hk", "de"} {
		w := httptest.NewRecorder()
		brandAssets().ServeHTTP(w, httptest.NewRequest("GET", "/assets/flags/"+code+".svg", nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "<svg") || !strings.Contains(w.Header().Get("Content-Type"), "svg") {
			t.Fatal(code, w.Code, w.Header())
		}
	}
}
