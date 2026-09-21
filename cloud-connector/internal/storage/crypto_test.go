package storage

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestTokenCipherRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	c, err := NewTokenCipher(key)
	if err != nil {
		t.Fatal(err)
	}

	plain := "refresh-token-abcdef-1234"
	ct, err := c.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ct, []byte(plain)) {
		t.Fatal("ciphertext contains plaintext")
	}
	got, err := c.Decrypt(ct)
	if err != nil {
		t.Fatal(err)
	}
	if got != plain {
		t.Fatalf("round trip: %q != %q", got, plain)
	}
}

func TestTokenCipherEmptyMapsToNil(t *testing.T) {
	key := make([]byte, 32)
	c, _ := NewTokenCipher(key)
	ct, err := c.Encrypt("")
	if err != nil || ct != nil {
		t.Fatalf("Encrypt(\"\") = %v, %v; want nil, nil", ct, err)
	}
	got, err := c.Decrypt(nil)
	if err != nil || got != "" {
		t.Fatalf("Decrypt(nil) = %q, %v", got, err)
	}
}

func TestTokenCipherRejectsTamperedCiphertext(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	c, _ := NewTokenCipher(key)
	ct, _ := c.Encrypt("secret")
	ct[len(ct)-1] ^= 0xFF
	if _, err := c.Decrypt(ct); err == nil {
		t.Fatal("expected authentication failure on tampered ciphertext")
	}
}

func TestTokenCipherRejectsWrongKeySize(t *testing.T) {
	if _, err := NewTokenCipher(make([]byte, 16)); err == nil {
		t.Fatal("expected error for 16-byte key")
	}
}

func TestTokenCipherNonceUnique(t *testing.T) {
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	c, _ := NewTokenCipher(key)
	a, _ := c.Encrypt("same plaintext")
	b, _ := c.Encrypt("same plaintext")
	if bytes.Equal(a, b) {
		t.Fatal("two encryptions of the same plaintext must differ (random nonce)")
	}
}
