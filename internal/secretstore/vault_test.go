package secretstore

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptionBindsRecordAndSurvivesRestart(t *testing.T) {
	key := filepath.Join(t.TempDir(), "key")
	v, err := Open(key, true)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("upstream-private-secret")
	one, err := v.Seal("instance:exit-1", plain)
	if err != nil {
		t.Fatal(err)
	}
	two, err := v.Seal("instance:exit-1", plain)
	if err != nil || bytes.Equal(one, two) || bytes.Contains(one, plain) {
		t.Fatal("plaintext or reused nonce", err)
	}
	reopened, err := Open(key, false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Open("instance:exit-1", one)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatal("restart decryption", err)
	}
	if _, err = v.Open("instance:exit-2", one); !errors.Is(err, ErrCiphertext) {
		t.Fatal("cross-record ciphertext accepted")
	}
	one[len(one)-1] ^= 1
	if _, err = v.Open("instance:exit-1", one); !errors.Is(err, ErrCiphertext) {
		t.Fatal("tampered ciphertext accepted")
	}
}
func TestMissingOrCorruptKeyIsNotOverwritten(t *testing.T) {
	key := filepath.Join(t.TempDir(), "key")
	if _, err := Open(key, false); !errors.Is(err, ErrKey) {
		t.Fatal("missing key recreated")
	}
	if _, err := os.Stat(key); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("key unexpectedly created")
	}
	if err := os.WriteFile(key, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(key, true); !errors.Is(err, ErrKey) {
		t.Fatal("corrupt key accepted")
	}
	if got, _ := os.ReadFile(key); string(got) != "keep" {
		t.Fatal("key overwritten")
	}
}
func TestVaultBounds(t *testing.T) {
	v, err := Open(filepath.Join(t.TempDir(), "key"), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.Seal("x", make([]byte, maxPlaintext+1)); err == nil {
		t.Fatal("oversize")
	}
	for _, b := range [][]byte{nil, {1, 2}, make([]byte, 5000)} {
		if _, err = v.Open("x", b); err == nil {
			t.Fatal("invalid ciphertext")
		}
	}
}
