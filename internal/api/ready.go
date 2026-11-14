package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
)

// pinger is the database handle /readyz pings.
type pinger interface {
	PingContext(ctx context.Context) error
}

// readyTimeout bounds the database ping of one probe.
const readyTimeout = 2 * time.Second

type readyResponse struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// Ready handles GET /readyz (no auth, specs.html § 7.8): 200 only when the database answers
// a ping and a worker has been heard from within queue.WorkerContactWindow, 503 otherwise.
// The reason names the failing check and never carries an error text, a path or a secret.
// A missing dependency is not ready.
func Ready(db pinger, q queue.Queue, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reason := notReadyReason(r.Context(), db, q, now)
		w.Header().Set("Content-Type", "application/json")
		if reason != "" {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(readyResponse{Status: "unavailable", Reason: reason})
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(readyResponse{Status: "ok"})
	}
}

func notReadyReason(ctx context.Context, db pinger, q queue.Queue, now func() time.Time) string {
	if db == nil {
		return "database unavailable"
	}
	pingCtx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		slog.Warn("readiness: database ping failed", "err", err)
		return "database unavailable"
	}
	if q == nil {
		return "queue unavailable"
	}
	at, ok, err := q.LastWorkerContact(ctx)
	switch {
	case err != nil:
		slog.Warn("readiness: reading the worker contact failed", "err", err)
		return "queue unavailable"
	case !ok:
		return "no worker has contacted the app"
	case now().Sub(at) > queue.WorkerContactWindow:
		return "worker contact is stale"
	}
	return ""
}
