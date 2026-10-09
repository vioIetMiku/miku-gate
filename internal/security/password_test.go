package security

import (
	"strings"
	"testing"
)

func TestPassword(t *testing.T) {
	hash, err := HashPassword("a-safe-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hash, "a-safe-test-password") {
		t.Fatal("plaintext hash")
	}
	if !VerifyPassword(hash, "a-safe-test-password") || VerifyPassword(hash, "wrong") {
		t.Fatal("verification mismatch")
	}
	for _, bad := range []string{"", "argon2id$v19$m999999t2p2$abc$abc", hash + "$extra"} {
		if VerifyPassword(bad, "anything") {
			t.Fatal("malformed hash accepted")
		}
	}
}
func TestTokens(t *testing.T) {
	a, _ := Token(32)
	b, _ := Token(32)
	if a == b || len(a) < 40 || Digest(a) == a {
		t.Fatal("bad token")
	}
}
