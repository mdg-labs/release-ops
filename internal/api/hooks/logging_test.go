package hooks_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/api/hooks"
	"github.com/mdg-labs/release-ops/internal/logging"
)

type opaqueSecret struct{ value string }

func (o opaqueSecret) WebhookSecret(context.Context) ([]byte, error) { return []byte(o.value), nil }

// The webhook secret is registered where the receiver reads it, whether or not the delivery
// that read it was signed correctly.
func TestReadingTheWebhookSecretRegistersIt(t *testing.T) {
	t.Parallel()
	const value = "opaque-webhook-secret-for-hooks-test-0001"
	rc := &hooks.Receiver{Secrets: opaqueSecret{value}, Routes: hooks.NewRoutes()}

	req := httptest.NewRequest(http.MethodPost, hooks.Path, bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-Hub-Signature-256", "sha256=0000")
	rec := httptest.NewRecorder()
	rc.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a bad signature = %d, want 401", rec.Code)
	}
	if got := logging.Redact("secret " + value); strings.Contains(got, value) {
		t.Fatalf("the webhook secret is not removed from log text: %q", got)
	}
}
