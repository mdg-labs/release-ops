package github

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"time"
)

const (
	// jwtLifetime is how long an App JWT is valid; GitHub accepts at most ten minutes.
	jwtLifetime = 9 * time.Minute
	// jwtBackdate puts iat in the past so a clock slightly ahead of GitHub's does not make
	// the token "issued in the future".
	jwtBackdate = 30 * time.Second
)

// ErrInvalidPrivateKey is returned when the App's private key is not a PEM-encoded RSA key.
// The key itself is never part of an error.
var ErrInvalidPrivateKey = errors.New("github app private key is not a PEM-encoded RSA key")

// SignAppJWT returns the RS256 JWT that authenticates as the App: iss is the App id, iat is
// shortly before now and exp is nine minutes after it.
func SignAppJWT(appID int64, privateKeyPEM []byte, now time.Time) (string, error) {
	key, err := parseRSAKey(privateKeyPEM)
	if err != nil {
		return "", err
	}

	header, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(struct {
		Iat int64 `json:"iat"`
		Exp int64 `json:"exp"`
		Iss int64 `json:"iss"`
	}{
		Iat: now.Add(-jwtBackdate).Unix(),
		Exp: now.Add(jwtLifetime).Unix(),
		Iss: appID,
	})
	if err != nil {
		return "", err
	}

	enc := base64.RawURLEncoding
	signingInput := enc.EncodeToString(header) + "." + enc.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", errors.New("sign github app jwt")
	}
	return signingInput + "." + enc.EncodeToString(sig), nil
}

func parseRSAKey(privateKeyPEM []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(privateKeyPEM)
	if block == nil {
		return nil, ErrInvalidPrivateKey
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, ErrInvalidPrivateKey
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, ErrInvalidPrivateKey
	}
	return key, nil
}
