package crypto

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

const testKeyHex = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func mustCipher(t *testing.T, hexKey string) *Cipher {
	t.Helper()
	c, err := NewCipherFromHex(hexKey)
	if err != nil {
		t.Fatalf("NewCipherFromHex() error = %v", err)
	}
	return c
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	c := mustCipher(t, testKeyHex)
	plaintext := []byte("github-pat-secret-token")

	wire, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	got, err := c.Decrypt(wire)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("Decrypt() = %q, want %q", got, plaintext)
	}
}

func TestWrongKeyFailsDecrypt(t *testing.T) {
	c1 := mustCipher(t, testKeyHex)
	wrongKey := strings.Repeat("f", keyHexLen)
	c2 := mustCipher(t, wrongKey)

	wire, err := c1.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	_, err = c2.Decrypt(wire)
	if err == nil {
		t.Fatal("Decrypt() with wrong key expected error, got nil")
	}
}

func TestParseKeyValidation(t *testing.T) {
	valid, err := ParseKey(testKeyHex)
	if err != nil {
		t.Fatalf("ParseKey(valid) error = %v", err)
	}
	if len(valid) != keyByteLen {
		t.Fatalf("ParseKey(valid) len = %d, want %d", len(valid), keyByteLen)
	}

	_, err = ParseKey(strings.Repeat("0", keyHexLen-1))
	if !errors.Is(err, ErrInvalidKey) {
		t.Errorf("short key error = %v, want ErrInvalidKey", err)
	}

	_, err = ParseKey(strings.Repeat("0", keyHexLen+1))
	if !errors.Is(err, ErrInvalidKey) {
		t.Errorf("long key error = %v, want ErrInvalidKey", err)
	}

	_, err = ParseKey(strings.Repeat("g", keyHexLen))
	if !errors.Is(err, ErrInvalidKey) {
		t.Errorf("non-hex key error = %v, want ErrInvalidKey", err)
	}

	_, err = ParseKey("")
	if !errors.Is(err, ErrInvalidKey) {
		t.Errorf("empty key error = %v, want ErrInvalidKey", err)
	}
}

func TestWireFormatBase64NonceCiphertextTag(t *testing.T) {
	c := mustCipher(t, testKeyHex)
	plaintext := []byte("wire-format-check")

	wire, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	blob, err := base64.StdEncoding.DecodeString(wire)
	if err != nil {
		t.Fatalf("base64 decode error = %v", err)
	}
	if len(blob) < nonceSize+c.aead.Overhead() {
		t.Fatalf("blob len = %d, want at least %d", len(blob), nonceSize+c.aead.Overhead())
	}

	nonce := blob[:nonceSize]
	ciphertextAndTag := blob[nonceSize:]
	got, err := c.aead.Open(nil, nonce, ciphertextAndTag, nil)
	if err != nil {
		t.Fatalf("manual Open() error = %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("manual Open() = %q, want %q", got, plaintext)
	}
}

func TestEmptyAndLargePayloads(t *testing.T) {
	c := mustCipher(t, testKeyHex)

	emptyWire, err := c.Encrypt(nil)
	if err != nil {
		t.Fatalf("Encrypt(empty) error = %v", err)
	}
	emptyGot, err := c.Decrypt(emptyWire)
	if err != nil {
		t.Fatalf("Decrypt(empty) error = %v", err)
	}
	if len(emptyGot) != 0 {
		t.Fatalf("Decrypt(empty) len = %d, want 0", len(emptyGot))
	}

	large := bytes.Repeat([]byte("x"), 64*1024)
	largeWire, err := c.Encrypt(large)
	if err != nil {
		t.Fatalf("Encrypt(large) error = %v", err)
	}
	largeGot, err := c.Decrypt(largeWire)
	if err != nil {
		t.Fatalf("Decrypt(large) error = %v", err)
	}
	if !bytes.Equal(largeGot, large) {
		t.Fatal("Decrypt(large) mismatch")
	}
}

func TestDifferentNoncesForSamePlaintext(t *testing.T) {
	c := mustCipher(t, testKeyHex)
	plaintext := []byte("repeat")

	w1, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() #1 error = %v", err)
	}
	w2, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt() #2 error = %v", err)
	}
	if w1 == w2 {
		t.Fatal("expected different wire outputs for same plaintext")
	}
}

func TestTamperedCiphertextFails(t *testing.T) {
	c := mustCipher(t, testKeyHex)
	wire, err := c.Encrypt([]byte("tamper-test"))
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	blob, err := base64.StdEncoding.DecodeString(wire)
	if err != nil {
		t.Fatalf("base64 decode error = %v", err)
	}
	blob[len(blob)-1] ^= 0xff
	tampered := base64.StdEncoding.EncodeToString(blob)

	_, err = c.Decrypt(tampered)
	if err == nil {
		t.Fatal("Decrypt(tampered) expected error, got nil")
	}
}

func TestDecryptErrorsDoNotLeakPlaintext(t *testing.T) {
	c := mustCipher(t, testKeyHex)
	secret := []byte("super-secret-value")
	wire, err := c.Encrypt(secret)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	wrong := mustCipher(t, strings.Repeat("a", keyHexLen))
	_, err = wrong.Decrypt(wire)
	if err == nil {
		t.Fatal("Decrypt() expected error, got nil")
	}
	if strings.Contains(err.Error(), string(secret)) {
		t.Fatalf("error leaks plaintext: %v", err)
	}
}
