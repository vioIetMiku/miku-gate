package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

func Token(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func Digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, 2, 64*1024, 2, 32)
	return fmt.Sprintf("argon2id$v19$m65536t2p2$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}
func passwordParts(encoded string) ([]byte, []byte, bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" || parts[1] != "v19" || parts[2] != "m65536t2p2" || len(parts[3]) != 22 || len(parts[4]) != 43 {
		return nil, nil, false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(salt) != 16 {
		return nil, nil, false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(expected) != 32 {
		return nil, nil, false
	}
	return salt, expected, true
}
func ValidPasswordHash(encoded string) bool { _, _, ok := passwordParts(encoded); return ok }
func VerifyPassword(encoded, password string) bool {
	if len(password) > 1024 {
		return false
	}
	salt, expected, ok := passwordParts(encoded)
	if !ok {
		return false
	}
	key := argon2.IDKey([]byte(password), salt, 2, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(key, expected) == 1
}
