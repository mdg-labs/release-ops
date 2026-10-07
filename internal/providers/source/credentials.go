package source

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ParseTokenSecret returns the optional access token from a decrypted source integration
// payload ({"token": "..."}). An empty token is allowed (public repos), and so is an empty
// payload, which an integration created without a token stores.
func ParseTokenSecret(secret []byte) (string, error) {
	if len(bytes.TrimSpace(secret)) == 0 {
		return "", nil
	}
	var creds struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(secret, &creds); err != nil {
		return "", fmt.Errorf("parse integration secret: %w", err)
	}
	return strings.TrimSpace(creds.Token), nil
}
