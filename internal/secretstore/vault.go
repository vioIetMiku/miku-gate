// Package secretstore encrypts recoverable upstream credentials with a separate
// local key. It does not protect against an attacker who can read that key too.
package secretstore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
	"os"
)

var ErrKey = errors.New("凭据密钥不可用；请恢复数据库配套的.key文件，不要覆盖或重新生成")
var ErrCiphertext = errors.New("加密凭据损坏、密钥不匹配或记录归属错误")

const maxPlaintext = 192 << 10

type Vault struct{ aead cipher.AEAD }

func Open(path string, allowCreate bool) (*Vault, error) {
	original, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && allowCreate {
		key := make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		f, createErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if createErr == nil {
			n, writeErr := f.Write(key)
			syncErr := f.Sync()
			closeErr := f.Close()
			if writeErr != nil || syncErr != nil || closeErr != nil || n != len(key) {
				return nil, ErrKey
			}
		} else if !errors.Is(createErr, os.ErrExist) {
			return nil, ErrKey
		}
		original, err = os.Lstat(path)
	}
	if err != nil || !original.Mode().IsRegular() || original.Mode()&os.ModeSymlink != 0 {
		return nil, ErrKey
	}
	f, err := openExisting(path)
	if err != nil {
		return nil, ErrKey
	}
	defer f.Close()
	current, err := f.Stat()
	if err != nil || !os.SameFile(original, current) || !validKeyInfo(current) {
		return nil, ErrKey
	}
	key, err := io.ReadAll(io.LimitReader(f, 33))
	if err != nil || len(key) != 32 {
		return nil, ErrKey
	}
	block, err := aes.NewCipher(key)
	clear(key)
	if err != nil {
		return nil, ErrKey
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: aead}, nil
}
func (v *Vault) Seal(label string, plain []byte) ([]byte, error) {
	if label == "" || len(plain) > maxPlaintext {
		return nil, ErrCiphertext
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte{1}, nonce...)
	return v.aead.Seal(out, nonce, plain, []byte(label)), nil
}
func (v *Vault) Open(label string, sealed []byte) ([]byte, error) {
	n := v.aead.NonceSize()
	if label == "" || len(sealed) < 1+n+v.aead.Overhead() || len(sealed) > 1+n+maxPlaintext+v.aead.Overhead() || sealed[0] != 1 {
		return nil, ErrCiphertext
	}
	plain, err := v.aead.Open(nil, sealed[1:1+n], sealed[1+n:], []byte(label))
	if err != nil {
		return nil, ErrCiphertext
	}
	return plain, nil
}
