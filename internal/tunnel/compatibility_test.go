package tunnel

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProfileCheckDoesNotClaimLivenessOrExposeSecrets(t *testing.T) {
	pki := testPKI(t)
	cases := []struct{ raw, mode string }{{pki.profile("8.8.8.8"), "strict"}, {pki.profile("8.8.8.8") + "cipher AES-128-CBC\nauth SHA1\n", "legacy-candidate"}, {pki.profile("8.8.8.8") + "plugin /tmp/unsafe.so\n", "unsupported"}}
	for _, test := range cases {
		result := CheckProfile(test.raw, "8.8.8.8")
		if result.Mode != test.mode || result.NetworkTested || result.ServerIdentityVerified || !result.ExpectedPeerNameRequired {
			t.Fatal("false policy/liveness report", result)
		}
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "PRIVATE KEY") || strings.Contains(string(raw), pki.ca) || strings.Contains(string(raw), "miku-static-analysis-not-a-peer") {
			t.Fatal("static check exposed executable config or credential material")
		}
	}
	if result := CheckProfile(strings.Repeat("x", MaxProfileBytes+1), "8.8.8.8"); result.Mode != "unsupported" || result.ProfileSHA256 != "" {
		t.Fatal("oversize profile not rejected early")
	}
}
