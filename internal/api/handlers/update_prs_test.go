package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mdg-labs/release-ops/internal/api/handlers"
	"github.com/mdg-labs/release-ops/internal/store"
)

// panicLister fails the test when a handler reads update PRs.
type panicLister struct{ t *testing.T }

func (p panicLister) List(context.Context, store.UpdatePRListFilter) ([]store.UpdatePRListItem, int, error) {
	p.t.Error("the handler listed update PRs without a principal")
	return nil, 0, nil
}

// The route sits behind Authenticate, which stores the caller's scope; the handler refuses a
// request with no principal on its own, so a route moved out of that group lists nothing.
func TestUpdatePRListHandlerNeedsAPrincipal(t *testing.T) {
	t.Parallel()

	h := &handlers.UpdatePRHandlers{PRs: panicLister{t}}
	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/update-prs", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("list without a principal = %d %s, want 401", rec.Code, rec.Body.String())
	}
}
