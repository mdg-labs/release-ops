package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/store"
)

// UpdateTicketHandlers serves the admin actions on the ticket of an update PR. The route sits
// behind RequireAdmin and the handler checks the role again, because clearing a marker lets the
// next event create a ticket that may duplicate one the ticket system already holds.
type UpdateTicketHandlers struct {
	Tickets store.UpdateTicketRepository
	// Now defaults to time.Now.
	Now func() time.Time
}

// ClearPendingMarker is POST /update-tickets/{id}/clear-pending-marker: it drops the pending
// marker of an update ticket that has none stored, together with its error and its retry, so
// the next event of its PR may create the ticket (specs.html section 5.2.2). The operator asks
// for it after looking in the ticket system for a ticket that carries the row's key. A ticket
// that is stored, or whose marker is gone already, is 409 and changes nothing.
func (h *UpdateTicketHandlers) ClearPendingMarker(w http.ResponseWriter, r *http.Request) {
	if !requireAdminRole(w, r) {
		return
	}
	id := chi.URLParam(r, "id")
	now := time.Now()
	if h.Now != nil {
		now = h.Now()
	}
	err := h.Tickets.ClearMarker(r.Context(), id, now.UTC())
	switch {
	case err == nil:
	case errors.Is(err, store.ErrUpdateTicketNotFound):
		auth.WriteError(w, "NOT_FOUND", "update ticket not found", http.StatusNotFound)
		return
	case errors.Is(err, store.ErrNoPendingMarker):
		auth.WriteError(w, "NO_PENDING_MARKER", "the update ticket has no pending marker to clear", http.StatusConflict)
		return
	default:
		slog.Error("update tickets: clearing a pending marker failed", "update_ticket", id, "err", err)
		auth.WriteError(w, "INTERNAL_ERROR", "failed to clear the pending marker", http.StatusInternalServerError)
		return
	}
	slog.Info("update tickets: an admin cleared a pending marker", "update_ticket", id)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
}
