package ipintel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

func fixtureClient(risk string, calls *atomic.Int32) *Client {
	return &Client{cache: map[string]entry{}, fetch: func(_ context.Context, address string, result any) error {
		calls.Add(1)
		raw := ""
		switch {
		case strings.Contains(address, "network-info"):
			raw = `{"status":"ok","data":{"prefix":"8.8.8.0/24","asns":["15169"]}}`
		case strings.Contains(address, "as-overview"):
			raw = `{"status":"ok","data":{"resource":"15169","holder":"GOOGLE - Google LLC"}}`
		case strings.Contains(address, "dns-blocklists"):
			raw = risk
		case strings.HasPrefix(address, "https://ipwho.is/"):
			raw = `{"success":true,"ip":"8.8.8.8","country":"United States","country_code":"US","city":"San Jose","connection":{"isp":"Google LLC","org":"Google LLC"}}`
		default:
			return errors.New("unexpected target")
		}
		return json.Unmarshal([]byte(raw), result)
	}}
}
func TestProviderMappingCacheAndNoCleanResidentialClaim(t *testing.T) {
	var calls atomic.Int32
	c := fixtureClient(`{"status":"ok","data":{"pending_results":false,"parameters":{"resource":"8.8.8.8"},"blocklists":{"example":{"name":"Example","operator":"Fixture"}},"data":{"example":{"status":"notlisted","query_time":"2026-10-11T00:00:00Z"}}}}`, &calls)
	r, err := c.Lookup(t.Context(), "8.8.8.8")
	if err != nil || r.IP != "8.8.8.8" || r.Prefix != "8.8.8.0/24" || len(r.ASNs) != 1 || r.ASNs[0].Number != 15169 || r.Geography.CountryCode != "US" || r.Reputation.State != "no_listings_in_checked_sources" || r.Residential != "unknown" || calls.Load() != 4 {
		t.Fatal(r, err, calls.Load())
	}
	cached, err := c.Lookup(t.Context(), "8.8.8.8")
	if err != nil || !cached.Cached || cached.CheckedAt != r.CheckedAt || calls.Load() != 4 {
		t.Fatal("cache changed evidence or repeated provider calls")
	}
	for _, ip := range []string{"127.0.0.1", "169.254.169.254", "192.0.2.1", "8.8.8.8:443", "::ffff:8.8.8.8", "https://8.8.8.8", "8.8.08.8"} {
		if _, err = c.Lookup(t.Context(), ip); err == nil {
			t.Fatal("unsafe metadata target", ip)
		}
	}
	if calls.Load() != 4 {
		t.Fatal("unsafe address contacted providers")
	}
}
func TestPendingFailureEmptyAndPartialDoNotMeanClean(t *testing.T) {
	cases := []struct{ body, want string }{
		{`{"status":"ok","data":{"pending_results":true,"parameters":{"resource":"8.8.8.8"},"blocklists":{"x":{}},"data":{}}}`, "pending"},
		{`{"status":"ok","data":{"pending_results":false,"parameters":{"resource":"8.8.8.8"},"blocklists":{"x":{}},"data":{}}}`, "unknown"},
		{`{"status":"ok","data":{"pending_results":false,"parameters":{"resource":"8.8.8.8"},"blocklists":{"x":{}},"data":{"x":{"status":"failed"}}}}`, "unknown"},
		{`{"status":"ok","data":{"pending_results":false,"parameters":{"resource":"8.8.8.8"},"blocklists":{"x":{},"y":{}},"data":{"x":{"status":"notlisted"}}}}`, "unknown"},
		{`{"status":"ok","data":{"pending_results":true,"parameters":{"resource":"8.8.8.8"},"blocklists":{"x":{}},"data":{"x":{"status":"listed"}}}}`, "listed"},
		{`{"status":"ok","data":{"pending_results":false,"parameters":{"resource":"1.1.1.1"},"data":{}}}`, "unknown"},
	}
	for _, tc := range cases {
		var calls atomic.Int32
		r, err := fixtureClient(tc.body, &calls).Lookup(t.Context(), "8.8.8.8")
		if err != nil || r.Reputation.State != tc.want {
			t.Fatal(r.Reputation, tc.want, err)
		}
	}
	c := &Client{cache: map[string]entry{}, fetch: func(context.Context, string, any) error { return errors.New("provider failed") }}
	r, err := c.Lookup(t.Context(), "8.8.8.8")
	if err != nil || r.Geography.State != "unknown" || r.Reputation.State != "unknown" || len(r.ASNs) != 0 {
		t.Fatal("source failure invented observations")
	}
}
func TestFixedHTTPSOriginsAndReturnedRecipientBinding(t *testing.T) {
	c := New()
	for _, target := range []string{"http://stat.ripe.net/data", "https://127.0.0.1/", "https://evil.example/data", "https://stat.ripe.net@evil.example/"} {
		if c.fetch(t.Context(), target, &struct{}{}) == nil {
			t.Fatal("nonfixed provider URL allowed")
		}
	}
	c.fetch = func(_ context.Context, address string, out any) error {
		return json.Unmarshal([]byte(`{"success":true,"ip":"1.1.1.1","country_code":"US"}`), out)
	}
	if c.geo(t.Context(), "8.8.8.8").State != "unknown" {
		t.Fatal("wrong queried recipient accepted")
	}
}
