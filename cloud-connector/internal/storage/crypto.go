package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
)

// TokenCipher encrypts provider token material before it touches the
// database, so a database dump alone never yields usable credentials. The
// key comes from the environment (TOKEN_CIPHER_KEY) — the standard "key in
// env / KMS, ciphertext in DB" split.
type TokenCipher struct {
	aead cipher.AEAD
}

// NewTokenCipher builds an AES-256-GCM cipher from a 32-byte key.
func NewTokenCipher(key []byte) (*TokenCipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("token cipher key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &TokenCipher{aead: aead}, nil
}

// Encrypt seals plaintext; the random nonce is prefixed to the ciphertext.
// Empty plaintext maps to nil so "no token" stays distinguishable.
func (c *TokenCipher) Encrypt(plaintext string) ([]byte, error) {
	if plaintext == "" {
		return nil, nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, []byte(plaintext), nil), nil
}

// Decrypt opens data produced by Encrypt.
func (c *TokenCipher) Decrypt(data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}
	ns := c.aead.NonceSize()
	if len(data) < ns {
		return "", fmt.Errorf("ciphertext shorter than nonce")
	}
	plain, err := c.aead.Open(nil, data[:ns], data[ns:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypting token: %w", err)
	}
	return string(plain), nil
}
