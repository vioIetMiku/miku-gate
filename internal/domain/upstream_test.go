package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestUpstreamValidationAndRedaction(t *testing.T) {
	c := UpstreamConfig{Kind: "https", Host: "proxy.example", Port: 443, Username: "u", Password: "private-value"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.DNS != "cloudflare" {
		t.Fatal("missing fixed resolver")
	}
	for _, value := range []any{c, c.View(""), UpstreamInput{Password: c.Password}} {
		if strings.Contains(fmt.Sprintf("%+v", value), c.Password) {
			t.Fatal("secret formatted")
		}
	}
	for _, value := range []any{c, c.View("")} {
		raw, err := json.Marshal(value)
		if err != nil || strings.Contains(string(raw), c.Password) {
			t.Fatal("secret in API JSON", err)
		}
	}
	if !c.View("").HasPassword || c.View("").Password != "" {
		t.Fatal("bad redacted view")
	}
}
func TestUnsafeUpstreamsRejected(t *testing.T) {
	good := UpstreamConfig{Kind: "https", Host: "proxy.example", Port: 443}
	cases := []UpstreamConfig{}
	for _, host := range []string{"169.254.169.254", "10.0.0.1", "::1", "::ffff:127.0.0.1", "localhost", "host.local", "127.1", "2130706433", "0x7f000001", "127.000.0.1", "proxy.example/path", "bad\r\n.example"} {
		c := good
		c.Host = host
		cases = append(cases, c)
	}
	for _, kind := range []string{"http", "socks5", "direct", "file"} {
		c := good
		c.Kind = kind
		cases = append(cases, c)
	}
	c := good
	c.Username = "user"
	cases = append(cases, c)
	c = good
	c.Password = "secret"
	cases = append(cases, c)
	c = good
	c.Host = "127.0.0.1"
	c.AllowLoopback = true
	c.Port = 22
	cases = append(cases, c)
	for i, c := range cases {
		if err := c.Validate(); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
	for _, kind := range []string{"http", "socks5"} {
		c := good
		c.Kind = kind
		c.AllowPlaintext = true
		if err := c.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	c = good
	c.Kind = "socks5"
	c.Host = "127.0.0.1"
	c.Port = 20001
	c.AllowLoopback = true
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestCredentialReuseCannotChangeTarget(t *testing.T) {
	a := UpstreamConfig{Kind: "https", Host: "proxy.example", Port: 443, Username: "user", Password: "secret"}
	b := a
	b.Password = ""
	b.DNS = "google"
	if !a.SameCredentialTarget(b) {
		t.Fatal("resolver does not change credential recipient")
	}
	b.Host = "other.example"
	if a.SameCredentialTarget(b) {
		t.Fatal("credential exfiltration target accepted")
	}
	in := UpstreamInput{Kind: "https", Host: "proxy.example", Port: 443, Username: "user", ReusePassword: true}
	if err := in.Validate(); err != nil {
		t.Fatal(err)
	}
	in.Password = "new"
	if err := in.Validate(); err == nil {
		t.Fatal("ambiguous secret update accepted")
	}
}
