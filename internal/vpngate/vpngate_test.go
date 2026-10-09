package vpngate

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

const goodConfig = "client\ndev tun\nproto tcp-client\nremote 8.8.8.8 443\nresolv-retry infinite\nnobind\nremote-cert-tls server\n"

func TestConfigRejectsUnsafeDirectives(t *testing.T) {
	if err := ValidateConfig(goodConfig, "8.8.8.8"); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"up /tmp/payload", "script-security 2", "plugin /tmp/code.so", "auth-user-pass /etc/shadow", "ca /etc/shadow", "log /tmp/file", "management 0.0.0.0 1000", "setenv LD_PRELOAD /tmp/x", "remote 1.1.1.1 80", "<ca>\nunclosed"} {
		if ValidateConfig(goodConfig+line+"\n", "8.8.8.8") == nil {
			t.Errorf("accepted %s", line)
		}
	}
	if ValidateConfig(strings.ReplaceAll(goodConfig, "8.8.8.8", "127.0.0.1"), "127.0.0.1") == nil {
		t.Fatal("loopback accepted")
	}
}
func TestParseCSV(t *testing.T) {
	csv := "*vpn_servers\n#HostName,IP,CountryShort,CountryLong,Ping,Speed,NumVpnSessions,OpenVPN_ConfigData_Base64\ntest,8.8.8.8,JP,Japan,20,10000000,3," + base64.StdEncoding.EncodeToString([]byte(goodConfig)) + "\n*\n"
	nodes, skipped, err := Parse(csv, time.Now())
	if err != nil || len(nodes) != 1 || skipped != 0 {
		t.Fatalf("%v %d %d", err, len(nodes), skipped)
	}
	if nodes[0].Type != "unknown" || nodes[0].Status != "untested" || nodes[0].AdvertisedPing != 20 {
		t.Fatal("fabricated classification")
	}
	if _, _, err = Parse("<html>not CSV</html>", time.Now()); err == nil {
		t.Fatal("HTML accepted")
	}
	if _, _, err = Parse(strings.Repeat("x", MaxBytes+1), time.Now()); err == nil {
		t.Fatal("oversized input")
	}
}
func FuzzConfig(f *testing.F) {
	f.Add(goodConfig)
	f.Add("up evil")
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 256*1024 {
			t.Skip()
		}
		_ = ValidateConfig(s, "8.8.8.8")
	})
}
