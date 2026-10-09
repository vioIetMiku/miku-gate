package gateway

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"strings"
	"sync"

	"mikugate/internal/security"
)

// The first successful authentication verifies the stored Argon2 hash. A keyed
// in-memory digest then avoids 64MiB Argon2 work for every multiplexed proxy flow.
// Credentials cannot be rotated while this runtime is active; stop revokes it.
func newAuthenticator(username, hash string, slots chan struct{}) (func(string, string) bool, error) {
	if username == "" || !security.ValidPasswordHash(hash) || strings.ContainsAny(username, "\x00\r\n:") {
		return nil, errors.New("代理认证材料不可用")
	}
	var key, digest [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return nil, err
	}
	var mu sync.Mutex
	ready := false
	sum := func(password string) [32]byte {
		mac := hmac.New(sha256.New, key[:])
		mac.Write([]byte(password))
		var value [32]byte
		copy(value[:], mac.Sum(nil))
		return value
	}
	return func(user, password string) bool {
		if len(user) > 255 || len(password) > 1024 || subtle.ConstantTimeCompare([]byte(username), []byte(user)) != 1 {
			return false
		}
		candidate := sum(password)
		mu.Lock()
		cached, expected := ready, digest
		mu.Unlock()
		if cached {
			return subtle.ConstantTimeCompare(candidate[:], expected[:]) == 1
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			return false
		}
		mu.Lock()
		cached, expected = ready, digest
		mu.Unlock()
		if cached {
			return subtle.ConstantTimeCompare(candidate[:], expected[:]) == 1
		}
		if !security.VerifyPassword(hash, password) {
			return false
		}
		mu.Lock()
		ready = true
		digest = candidate
		mu.Unlock()
		return true
	}, nil
}
