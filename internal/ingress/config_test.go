package ingress

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const xrayFixture = `{"log":{"loglevel":"warning"},"inbounds":[{"tag":"existing-vless","protocol":"vless","port":443,"settings":{"clients":[{"id":"secret-uuid","flow":"xtls-rprx-vision","email":"secret-email"}],"decryption":"none"},"streamSettings":{"network":"raw","security":"reality","realitySettings":{"privateKey":"secret-private-key","target":"example.com:443"}},"sniffing":{"enabled":true}},{"tag":"other-node","protocol":"trojan","port":8443,"settings":{"clients":[{"password":"secret-trojan"}]},"streamSettings":{"network":"tcp","security":"tls"}}],"outbounds":[{"tag":"direct","protocol":"freedom"}],"routing":{"domainStrategy":"IPIfNonMatch","rules":[{"type":"field","inboundTag":["other-node"],"outboundTag":"direct"}]},"dns":{"servers":["1.1.1.1"]}}`
const singFixture = `{"inbounds":[{"type":"vless","tag":"existing-vless","listen":"::","listen_port":443,"users":[{"uuid":"secret-uuid","flow":"xtls-rprx-vision"}],"tls":{"enabled":true,"reality":{"enabled":true,"private_key":"secret-private-key","handshake":{"server":"example.com","server_port":443}}}}],"outbounds":[{"type":"direct","tag":"direct"}],"route":{"final":"direct","rules":[{"domain":"other.example","action":"route","outbound":"direct"}]},"dns":{"servers":[{"type":"local","tag":"local"}]}}`

func TestInspectionDoesNotReturnCredentialsOrPrivateMaterial(t *testing.T) {
	for _, tc := range []struct{ id, raw string }{{"x-ui", xrayFixture}, {"sing-box", singFixture}} {
		s, _ := FindSource(tc.id)
		r, e := Inspect(s, []byte(tc.raw))
		if e != nil {
			t.Fatal(e)
		}
		if r.Applied || r.CoreValidated || !r.ReadOnly || len(r.Inbounds) < 1 || !r.Inbounds[0].Candidate || !r.Inbounds[0].Vision || r.Inbounds[0].Security != "reality" {
			t.Fatalf("unexpected summary %+v", r)
		}
		b, _ := json.Marshal(r)
		if strings.Contains(string(b), "secret-") {
			t.Fatal("sensitive JSON leaked")
		}
		if s.PanelManaged && !strings.Contains(strings.Join(r.Warnings, ""), "面板会重写") {
			t.Fatal("panel persistence limitation hidden")
		}
	}
}
func TestScopedRoutesPreserveUnselectedAndExistingData(t *testing.T) {
	for _, tc := range []struct{ id, raw string }{{"x-ui", xrayFixture}, {"sing-box", singFixture}} {
		s, _ := FindSource(tc.id)
		raw := []byte(tc.raw)
		report, _ := Inspect(s, raw)
		proxy := Proxy{Port: 20000, Username: "new-user", Password: "new-secret"}
		candidate, plan, e := Prepare(s, raw, report.SHA256, report.Inbounds[0].ID, proxy)
		if e != nil {
			t.Fatal(e)
		}
		if plan.Applied || plan.CoreValidated || !plan.UDPBlocked || !plan.OtherInboundsPreserved || plan.CandidateSHA256 != digest(candidate) {
			t.Fatal("false claims")
		}
		encoded, _ := json.Marshal(plan)
		if strings.Contains(string(encoded), "new-secret") || strings.Contains(string(encoded), "secret-uuid") {
			t.Fatal("plan leaks secrets")
		}
		before, _ := parseJSON(raw)
		after, _ := parseJSON(candidate)
		for _, key := range []string{"inbounds", "log", "dns"} {
			if !reflect.DeepEqual(before[key], after[key]) {
				t.Fatalf("modified unrelated %s", key)
			}
		}
		if !reflect.DeepEqual(array(before["outbounds"])[0], array(after["outbounds"])[0]) {
			t.Fatal("modified original outbound")
		}
		key := "routing"
		if s.Core == "sing-box" {
			key = "route"
		}
		rules := array(object(after[key])["rules"])
		old := array(object(before[key])["rules"])
		if len(rules) != len(old)+2 || !reflect.DeepEqual(rules[2:], old) {
			t.Fatal("existing rules changed or broad rule")
		}
		if text(object(rules[0])["network"]) != "udp" || text(object(rules[1])["network"]) != "tcp" {
			t.Fatal("first-match protection broken")
		}
		if s.Core == "xray" {
			if object(after[key])["domainStrategy"] != "IPIfNonMatch" {
				t.Fatal("global DNS strategy changed")
			}
			if !reflect.DeepEqual(object(rules[0])["inboundTag"], []any{"existing-vless"}) {
				t.Fatal("unscoped block")
			}
		} else {
			if object(after[key])["final"] != "direct" || object(rules[0])["action"] != "reject" || object(rules[1])["action"] != "route" {
				t.Fatal("wrong modern route syntax")
			}
		}
		if !strings.Contains(string(candidate), "new-secret") {
			t.Fatal("local candidate missing authentication")
		}
		nextReport, _ := Inspect(s, candidate)
		if _, _, e = Prepare(s, candidate, nextReport.SHA256, nextReport.Inbounds[0].ID, proxy); e == nil {
			t.Fatal("duplicate managed routes accepted")
		}
	}
}
func TestConfigurationAmbiguityAndSelectionFailClosed(t *testing.T) {
	s, _ := FindSource("x-ui")
	for _, raw := range []string{`{"inbounds":[],"inbounds":[]}`, `{"inbounds":[{"tag":"x","tag":"secret"}]}`, `{"inbounds":[]} {}`, `{"inbounds":[],//secret\n}`, `[]`, strings.Repeat(" ", MaxConfig+1)} {
		if _, e := Inspect(s, []byte(raw)); e == nil || strings.Contains(e.Error(), "secret") {
			t.Fatal("ambiguous/oversized source accepted or leaked")
		}
	}
	raw := []byte(xrayFixture)
	r, _ := Inspect(s, raw)
	p := Proxy{20000, "user", "secret"}
	for _, change := range []struct {
		sha, id string
		p       Proxy
	}{{strings.Repeat("0", 64), r.Inbounds[0].ID, p}, {r.SHA256, "arbitrary", p}, {r.SHA256, r.Inbounds[0].ID, Proxy{80, "user", "secret"}}} {
		if _, _, e := Prepare(s, raw, change.sha, change.id, change.p); e == nil {
			t.Fatal("invalid snapshot/id/proxy accepted")
		}
	}
	duplicate := strings.Replace(xrayFixture, `"other-node"`, `"existing-vless"`, 1)
	summary, e := Inspect(s, []byte(duplicate))
	if e != nil || summary.Inbounds[0].Candidate || summary.Inbounds[1].Candidate {
		t.Fatal("duplicate tag accepted")
	}
	noClients := strings.Replace(xrayFixture, `[{"id":"secret-uuid","flow":"xtls-rprx-vision","email":"secret-email"}]`, `[]`, 1)
	summary, _ = Inspect(s, []byte(noClients))
	if summary.Inbounds[0].Candidate {
		t.Fatal("no clients treated as usable")
	}
	for _, value := range []string{"secret-value", strings.Repeat("x", 100)} {
		altered := strings.Replace(xrayFixture, `"network":"raw"`, `"network":"`+value+`"`, 1)
		r, e := Inspect(s, []byte(altered))
		if e != nil || r.Inbounds[0].Candidate || strings.Contains(r.Inbounds[0].Transport, "secret") {
			t.Fatal("unsupported transport echoed")
		}
	}
}
func TestFixedSourcesAndIsolation(t *testing.T) {
	original := Sources()
	original[0].Path = "/etc/shadow"
	s, _ := FindSource("x-ui")
	if s.Path == "/etc/shadow" {
		t.Fatal("sources mutated")
	}
	for _, id := range []string{"../../etc/shadow", "/etc/shadow", "xray.service; bad"} {
		if _, e := FindSource(id); e == nil {
			t.Fatal("arbitrary source accepted")
		}
	}
	if _, e := ReadSource(Source{ID: "x-ui", Path: "/etc/shadow", Core: "xray"}); e == nil {
		t.Fatal("arbitrary path accepted")
	}
}
