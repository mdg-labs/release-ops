package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/api/handlers"
	"github.com/mdg-labs/release-ops/internal/store"
)

// panicTickets fails the test when a handler clears a marker.
type panicTickets struct {
	store.UpdateTicketRepository
	t *testing.T
}

func (p panicTickets) ClearMarker(context.Context, string, time.Time) error {
	p.t.Error("the handler cleared a pending marker without an admin principal")
	return nil
}

// The route sits behind RequireAdmin; the handler refuses a caller with no admin principal on
// its own, so a route moved out of the admin group does not let anyone clear a marker.
func TestClearPendingMarkerHandlerEnforcesTheAdminRoleItself(t *testing.T) {
	t.Parallel()

	h := &handlers.UpdateTicketHandlers{Tickets: panicTickets{t: t}}
	rec := httptest.NewRecorder()
	h.ClearPendingMarker(rec, httptest.NewRequest(http.MethodPost, "/update-tickets/ut-1/clear-pending-marker", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("clear without a principal = %d %s, want 401", rec.Code, rec.Body.String())
	}
}
