package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

const (
	keyByteLen = 32
	keyHexLen  = 64
	nonceSize  = 12
	keyIDBytes = 8
)

// ErrInvalidKey is returned when an encryption key is not 64 hex characters.
var ErrInvalidKey = errors.New("encryption key must be 64 hexadecimal characters")

// ErrInvalidWireFormat is returned when an encrypted payload cannot be decoded.
var ErrInvalidWireFormat = errors.New("invalid encrypted payload wire format")

// Cipher encrypts and decrypts credential payloads for encrypted_payload columns.
type Cipher struct {
	aead  cipher.AEAD
	keyID string
}

// ParseKey decodes APP_ENCRYPTION_KEY: exactly 64 hexadecimal characters (32 bytes).
func ParseKey(hexKey string) ([]byte, error) {
	if len(hexKey) != keyHexLen {
		return nil, ErrInvalidKey
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, ErrInvalidKey
	}
	if len(key) != keyByteLen {
		return nil, ErrInvalidKey
	}
	return key, nil
}

// NewCipher creates a Cipher from a 32-byte AES-256 key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != keyByteLen {
		return nil, fmt.Errorf("encryption key must be %d bytes", keyByteLen)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead, keyID: keyFingerprint(key)}, nil
}

// keyFingerprint is a short label for a key that reveals nothing usable about it: the first
// eight bytes of SHA-256 over a domain-separated copy of the key, as hex.
func keyFingerprint(key []byte) string {
	sum := sha256.Sum256(append([]byte("release-ops/encryption-key-id/v1:"), key...))
	return hex.EncodeToString(sum[:keyIDBytes])
}

// KeyID identifies the key this Cipher encrypts with. Stored next to a payload it shows which
// key sealed it, so a key mismatch is detectable before decryption is attempted.
func (c *Cipher) KeyID() string {
	return c.keyID
}

// NewCipherFromHex creates a Cipher from a 64-character hex-encoded key.
func NewCipherFromHex(hexKey string) (*Cipher, error) {
	key, err := ParseKey(hexKey)
	if err != nil {
		return nil, err
	}
	return NewCipher(key)
}

// Encrypt returns base64(nonce + ciphertext + tag) per specs §4.8.
func (c *Cipher) Encrypt(plaintext []byte) (string, error) {
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt decodes wire-format payloads produced by Encrypt.
func (c *Cipher) Decrypt(wire string) ([]byte, error) {
	blob, err := base64.StdEncoding.DecodeString(wire)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid base64", ErrInvalidWireFormat)
	}
	if len(blob) < nonceSize+c.aead.Overhead() {
		return nil, ErrInvalidWireFormat
	}
	nonce := blob[:nonceSize]
	ciphertext := blob[nonceSize:]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}
