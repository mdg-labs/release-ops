package hooks

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

// Delivery is a verified webhook delivery handed to the handler of its event.
type Delivery struct {
	// GUID is the X-GitHub-Delivery value.
	GUID  string
	Event string
	// Payload is the raw body, verified against the webhook secret.
	Payload []byte
	// GitHubInstallationID is the installation id the payload names, 0 when it names none.
	GitHubInstallationID int64
	// ReceivedAt is when the request arrived.
	ReceivedAt time.Time
}

// Handler applies a delivery. It runs inside the transaction that records the delivery's
// GUID: every write it makes through tx commits together with that record, and an error
// rolls both back, so GitHub's redelivery finds the delivery unseen. A handler makes no
// call to GitHub, because the request waits for it.
type Handler func(ctx context.Context, tx *sql.Tx, d Delivery) error

// Acknowledge is a Handler that accepts a delivery and does nothing with it.
func Acknowledge(context.Context, *sql.Tx, Delivery) error { return nil }

// Routes maps an event name to its handler. An event without a handler is answered 202 and
// ignored. Handlers are registered before the server starts and may be replaced.
type Routes struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewRoutes returns the routing table with the installation events acknowledged. The
// consumers of the other subscribed events (issues, pull_request, push) register through
// Register.
func NewRoutes() *Routes {
	r := &Routes{handlers: map[string]Handler{}}
	r.Register("installation", Acknowledge)
	r.Register("installation_repositories", Acknowledge)
	return r
}

// Register sets the handler of an event, replacing the one it had.
func (r *Routes) Register(event string, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[event] = h
}

func (r *Routes) lookup(event string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[event]
	return h, ok
}
