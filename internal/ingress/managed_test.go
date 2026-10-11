package ingress

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestManagedPlansDoNotProduceKeysOrPretendApplied(t *testing.T) {
	for _, core := range []string{"xray", "sing-box"} {
		for _, template := range Templates() {
			r := ManagedRequest{Core: core, Template: template.ID, Address: "192.0.2.10", Port: 8443, ServerName: "entry.example"}
			if template.NeedsRealityTarget {
				r.RealityHostname = r.ServerName
			}
			p, e := ManagedPlanFor(r)
			if e != nil || p.Applied || p.CoreValidated || p.PublicManagementConfigured || !p.UDPBlocked {
				t.Fatal("false plan")
			}
			raw, _ := json.Marshal(p)
			if strings.Contains(string(raw), "private_key") {
				t.Fatal("preview made secrets")
			}
		}
	}
	r := ManagedRequest{Core: "xray", Template: "vless-tls-vision", Address: "entry.example", Port: 443, ServerName: "entry.example"}
	if _, e := ManagedPlanFor(ManagedRequest{}); e == nil {
		t.Fatal("empty request")
	}
	if _, e := ManagedPlanFor(ManagedRequest{Core: "xray", Template: "vless-reality-vision", Address: "entry.example", Port: 443, ServerName: "entry.example", RealityHostname: "other.example"}); e == nil {
		t.Fatal("mismatching SNI")
	}
	if _, e := GenerateManaged(r, Proxy{20000, "u", "p"}, nil, ManagedFiles{}, nil); e == nil {
		t.Fatal("TLS without verified cert")
	}
}
func TestManagedRealityGenerationClientCredentialAndSecretBoundaries(t *testing.T) {
	for _, core := range []string{"xray", "sing-box"} {
		r := ManagedRequest{Core: core, Template: "vless-reality-vision", Address: "192.0.2.10", Listen: "0.0.0.0", Port: 8443, ServerName: "target.example", RealityHostname: "target.example"}
		target := RealityTarget{Hostname: r.ServerName, Address: "192.0.2.20", Port: 443, CheckedAt: time.Now(), NotAfter: time.Now().Add(time.Hour * 24), TLSVersion: "1.3", ALPN: "h2", Curve: "X25519", Suitable: true, verified: true}
		b, e := GenerateManaged(r, Proxy{20000, "miku-user", "miku-password"}, nil, ManagedFiles{}, &target)
		if e != nil {
			t.Fatal(e)
		}
		if len(b.ServerJSON) == 0 || len(b.ClientJSON) == 0 || b.ServerSHA256 != digest(b.ServerJSON) {
			t.Fatal("missing private bundle")
		}
		raw, _ := json.Marshal(b)
		if strings.Contains(string(raw), "miku-password") || strings.Contains(string(raw), "privateKey") || strings.Contains(string(raw), "private_key") || strings.Contains(string(raw), "vless://") {
			t.Fatal("API-safe serialization leaked secrets")
		}
		if strings.Contains(string(b.ClientJSON), "miku-password") || strings.Contains(string(b.ClientJSON), "privateKey") || strings.Contains(string(b.ClientJSON), "private_key") {
			t.Fatal("server secrets in client")
		}
		uri, e := url.Parse(b.URI)
		if e != nil || uri.Scheme != "vless" || uri.Query().Get("security") != "reality" || uri.Query().Get("flow") != "xtls-rprx-vision" || len(uri.Query().Get("sid")) != 16 || len(uri.Query().Get("pbk")) != 43 {
			t.Fatal("client import incomplete")
		}
		m, e := parseJSON(b.ServerJSON)
		if e != nil {
			t.Fatal(e)
		}
		for _, out := range array(m["outbounds"]) {
			if text(object(out)["type"]) == "direct" || text(object(out)["protocol"]) == "freedom" {
				t.Fatal("managed core has direct fallback")
			}
		}
		forged := target
		forged.verified = false
		if _, e := GenerateManaged(r, Proxy{20000, "u", "p"}, nil, ManagedFiles{}, &forged); e == nil {
			t.Fatal("unverified target accepted")
		}
		stale := target
		stale.CheckedAt = time.Now().Add(-11 * time.Minute)
		if _, e := GenerateManaged(r, Proxy{20000, "u", "p"}, nil, ManagedFiles{}, &stale); e == nil {
			t.Fatal("stale target accepted")
		}
		b2, e := GenerateManaged(r, Proxy{20000, "u", "p"}, nil, ManagedFiles{}, &target)
		if e != nil || b2.URI == b.URI {
			t.Fatal("reused client secret")
		}
	}
}
func TestRealityMetadataHasFixedTenCandidates(t *testing.T) {
	if len(RealityCandidates()) != 10 {
		t.Fatal("candidate count")
	}
	for _, c := range RealityCandidates() {
		if c.Port != 443 || c.Hostname == "" || c.Hint == "" {
			t.Fatal("unqualified candidate")
		}
	}
}
