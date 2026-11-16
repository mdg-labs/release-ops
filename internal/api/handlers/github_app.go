package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/store"
)

// maxPrivateKeyBody bounds the rotation request: a PEM RSA key is a few kilobytes, and JSON
// can at most double its size by escaping the line breaks.
const maxPrivateKeyBody = 64 << 10

// AppKeyVerifier checks a candidate private key against GitHub without storing it. It
// returns the App id the key was verified for.
type AppKeyVerifier interface {
	VerifyKey(ctx context.Context, privateKeyPEM []byte) (appID int64, err error)
}

// AppKeyStore replaces the stored private key.
type AppKeyStore interface {
	ReplacePrivateKey(ctx context.Context, appID int64, privateKeyPEM []byte) error
}

// GitHubAppHandlers serves the GitHub App's key rotation.
type GitHubAppHandlers struct {
	Verifier AppKeyVerifier
	Apps     AppKeyStore
}

type rotateKeyRequest struct {
	PrivateKey string `json:"privateKey"`
}

// keyRejectedMessage is the one message for every key that cannot be used, so a response does
// not tell which check failed.
const keyRejectedMessage = "the key could not be verified"

// RotatePrivateKey handles PUT /api/v1/github-app/private-key (specs.html §3.4). The new key
// replaces the stored one only once GitHub has accepted a JWT signed with it for this
// instance's App; any failure before that leaves the stored key unchanged. A key that is
// missing, not a key, rejected by GitHub or another App's answers 422 VALIDATION_ERROR with
// the same message. Neither the request's key nor the stored one appears in a response or a
// log line.
func (h *GitHubAppHandlers) RotatePrivateKey(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPrivateKeyBody)
	var req rotateKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		auth.WriteError(w, "VALIDATION_ERROR", keyRejectedMessage, http.StatusUnprocessableEntity)
		return
	}
	key := []byte(req.PrivateKey)
	req.PrivateKey = ""
	defer clear(key)
	if len(key) == 0 {
		auth.WriteError(w, "VALIDATION_ERROR", keyRejectedMessage, http.StatusUnprocessableEntity)
		return
	}

	appID, err := h.Verifier.VerifyKey(r.Context(), key)
	switch {
	case err == nil:
	case errors.Is(err, github.ErrInvalidPrivateKey), errors.Is(err, github.ErrAppRejected), errors.Is(err, github.ErrAppMismatch):
		auth.WriteError(w, "VALIDATION_ERROR", keyRejectedMessage, http.StatusUnprocessableEntity)
		return
	case errors.Is(err, store.ErrGitHubAppNotFound):
		auth.WriteError(w, "NOT_FOUND", "the GitHub App is not configured", http.StatusNotFound)
		return
	case errors.Is(err, github.ErrRequestFailed):
		auth.WriteError(w, "GITHUB_UNAVAILABLE", "GitHub could not be reached to check the key; try again", http.StatusBadGateway)
		return
	default:
		slog.Error("verify github app private key failed", "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to verify the key", http.StatusInternalServerError)
		return
	}

	if err := h.Apps.ReplacePrivateKey(r.Context(), appID, key); err != nil {
		if errors.Is(err, store.ErrGitHubAppNotFound) {
			auth.WriteError(w, "NOT_FOUND", "the GitHub App is not configured", http.StatusNotFound)
			return
		}
		slog.Error("replace github app private key failed", "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to store the key", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
