package hooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mdg-labs/release-ops/internal/store"
)

const (
	// Path is where the endpoint is served.
	Path = "/hooks/github"
	// MaxBodyBytes caps the request body, which the handler reads before it can
	// authenticate the sender.
	MaxBodyBytes = 1 << 20

	signaturePrefix = "sha256="
	maxGUIDLength   = 128
)

// Secrets reads the webhook secret of the stored GitHub App.
type Secrets interface {
	// WebhookSecret returns store.ErrGitHubAppNotFound before an App is stored.
	WebhookSecret(ctx context.Context) ([]byte, error)
}

// Deliveries records accepted deliveries together with the work they cause.
type Deliveries interface {
	Receive(ctx context.Context, d store.WebhookDelivery, apply func(ctx context.Context, tx *sql.Tx) error) (duplicate bool, err error)
}

// Receiver is the http.Handler of POST /hooks/github.
type Receiver struct {
	Secrets    Secrets
	Deliveries Deliveries
	Routes     *Routes
	// SignatureFailure, when set, is called for each request whose signature check fails and
	// reports whether the client is over its limit of failures, and the Retry-After to send.
	SignatureFailure func(r *http.Request) (retryAfter time.Duration, limited bool)
	// Now defaults to time.Now.
	Now func() time.Time
}

// ServeHTTP answers with a bare status: 413 for a body over MaxBodyBytes, 503 while no App
// is stored, 401 for a missing or wrong signature (429 with Retry-After once the client has
// failed too often; a valid signature is never counted or refused for it), 400 for a verified request that lacks
// a delivery GUID or event or carries an unreadable payload, 500 when the delivery could
// not be recorded or applied (nothing is recorded then, so a redelivery is processed), and
// 202 otherwise, including for a duplicate GUID and an event nothing handles.
func (rc *Receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	now := time.Now
	if rc.Now != nil {
		now = rc.Now
	}
	receivedAt := now()

	if r.ContentLength > MaxBodyBytes {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	secret, err := rc.Secrets.WebhookSecret(r.Context())
	if err != nil {
		if errors.Is(err, store.ErrGitHubAppNotFound) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		slog.Error("webhook: read secret", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	valid := validSignature(secret, body, r.Header.Get("X-Hub-Signature-256"))
	clear(secret)
	if !valid {
		if rc.SignatureFailure != nil {
			if retryAfter, limited := rc.SignatureFailure(r); limited {
				w.Header().Set("Retry-After", retryAfterSeconds(retryAfter))
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
		}
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	guid := r.Header.Get("X-GitHub-Delivery")
	event := r.Header.Get("X-GitHub-Event")
	if guid == "" || len(guid) > maxGUIDLength || event == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	handle, ok := rc.Routes.lookup(event)
	if !ok {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	var payload struct {
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	d := Delivery{GUID: guid, Event: event, Payload: body, GitHubInstallationID: payload.Installation.ID, ReceivedAt: receivedAt}

	var afterCommit []func()
	d.afterCommit = func(f func()) { afterCommit = append(afterCommit, f) }
	duplicate, err := rc.Deliveries.Receive(r.Context(), store.WebhookDelivery{
		GUID: guid, Event: event, GitHubInstallationID: d.GitHubInstallationID, ReceivedAt: receivedAt,
	}, func(ctx context.Context, tx *sql.Tx) error { return handle(ctx, tx, d) })
	if err != nil {
		slog.Error("webhook: apply delivery", "event", event, "delivery", guid, "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if !duplicate {
		for _, f := range afterCommit {
			f()
		}
	}
	w.WriteHeader(http.StatusAccepted)
}

// validSignature compares the header, sha256=<hex of HMAC-SHA256(secret, body)>, with the
// signature of body in constant time.
func validSignature(secret, body []byte, header string) bool {
	hexSum, ok := strings.CutPrefix(header, signaturePrefix)
	if !ok {
		return false
	}
	got, err := hex.DecodeString(hexSum)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

func retryAfterSeconds(d time.Duration) string {
	return strconv.Itoa(max(1, int(d.Round(time.Second)/time.Second)))
}
