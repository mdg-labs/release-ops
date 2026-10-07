package source

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseTokenSecret returns the optional access token from a decrypted source integration
// payload ({"token": "..."}). An empty token is allowed (public repos).
func ParseTokenSecret(secret []byte) (string, error) {
	var creds struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(secret, &creds); err != nil {
		return "", fmt.Errorf("parse integration secret: %w", err)
	}
	return strings.TrimSpace(creds.Token), nil
}
