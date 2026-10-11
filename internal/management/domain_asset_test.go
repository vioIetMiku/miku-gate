package management

import "testing"

func TestDomainReuseIsExactNotAnIPBootstrapOrArbitraryOrigin(t *testing.T) {
	ip, _ := ParseOrigin("https://192.0.2.10:8443")
	if _, _, err := DomainAsset(ip, "/unknown/cert", "/unknown/key", "entry.example"); err == nil {
		t.Fatal("IP bootstrap reused as trusted DNS cert")
	}
	domain, _ := ParseOrigin("https://entry.example:8443")
	for _, host := range []string{"other.example", "../entry.example", "https://entry.example", "192.0.2.10"} {
		if _, _, err := DomainAsset(domain, "/unknown/cert", "/unknown/key", host); err == nil {
			t.Fatal("cross-domain/arbitrary path request accepted")
		}
	}
	domain.Authority = "evil.example:8443"
	if _, _, err := DomainAsset(domain, "/unknown/cert", "/unknown/key", "entry.example"); err == nil {
		t.Fatal("forged endpoint metadata")
	}
}
