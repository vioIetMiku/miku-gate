package tunnel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
)

func TestVerifiedPeerFingerprintIsRenderedAndStrictlyBounded(t *testing.T) {
	pki := testPKI(t)
	block, _ := pem.Decode([]byte(pki.serverCert))
	sum := sha256.Sum256(block.Bytes)
	fingerprint := hex.EncodeToString(sum[:])
	raw := pki.profile("8.8.8.8")
	profile, err := NormalizeWithOptions(raw, "8.8.8.8", NormalizeOptions{ExpectedPeerName: "miku-lab-server", ExpectedPeerFingerprint: fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	text := profile.render()
	for _, required := range []string{"peer-fingerprint " + colonFingerprint(fingerprint) + "\n", "verify-x509-name miku-lab-server name\n", "remote-cert-tls server\n", "tls-version-min 1.2\n"} {
		if !strings.Contains(text, required) {
			t.Fatal("missing explicit peer binding", required)
		}
	}
	for _, invalid := range []string{"ab", strings.Repeat("A", 64), strings.Repeat("g", 64), fingerprint + "\nplugin /tmp/evil", colonFingerprint(fingerprint)} {
		if _, err = NormalizeWithOptions(raw, "8.8.8.8", NormalizeOptions{ExpectedPeerName: "miku-lab-server", ExpectedPeerFingerprint: invalid}); err == nil {
			t.Fatal("invalid fingerprint accepted", invalid)
		}
	}
}

func TestFailureCategoryIsBoundedAndDoesNotEchoPeerLogs(t *testing.T) {
	for _, test := range []struct{ raw, want string }{{"secret material: ca md too weak", "certificate_algorithm_rejected"}, {"server says VERIFY X509NAME ERROR credential=do-not-echo", "server_name_rejected"}, {"arbitrary peer text private-key-line", "session_unavailable"}} {
		actual := FailureCode(errors.New(test.raw))
		if actual != test.want || strings.Contains(actual, "secret") || strings.Contains(actual, "private") {
			t.Fatal("unbounded failure diagnostic", actual)
		}
	}
}
