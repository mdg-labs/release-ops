package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/mdg-labs/release-ops/internal/api/auth"
	"github.com/mdg-labs/release-ops/internal/api/handlers"
	"github.com/mdg-labs/release-ops/internal/api/hooks"
	apimw "github.com/mdg-labs/release-ops/internal/api/middleware"
	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/store"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

const apiV1Prefix = "/api/v1"

// ServerDeps holds dependencies for the production HTTP router.
type ServerDeps struct {
	DB                *sql.DB
	Session           *scs.SessionManager
	Queries           *storedb.Queries
	Store             *store.Store
	IntegrationTester handlers.IntegrationTester
	// AppURL is the browser-facing URL of the instance (APP_URL).
	AppURL string
	// WebhookURL is the configured WEBHOOK_URL, empty when unset, and GitHubBaseURL the
	// configured GITHUB_BASE_URL; with GitHub they serve the manifest flow on /setup.
	// A WebhookURL also serves POST /hooks/github; without one that route does not exist.
	WebhookURL    string
	GitHubBaseURL string
	GitHub        *github.Client
	// GitHubApp mints the installation tokens of "Enable repo". It must carry the rate-limit
	// budget GitHub uses, so the action draws on the installation's shared budget; without it
	// the route is not mounted.
	GitHubApp *github.AppClient
	// GitHubOAuth runs the OAuth sign-in. When nil it is built from GitHub and GitHubBaseURL;
	// tests set it to a fake.
	GitHubOAuth auth.OAuthProvider
	// Roles derives the role of a GitHub user from the installation's account and
	// memberships, at sign-in and every 15 minutes of a session. Without it only the
	// bootstrap admin can sign in, and a session whose role is due for a re-check is refused.
	Roles auth.RoleEvaluator
	// BootstrapAdminGitHubLogin is BOOTSTRAP_ADMIN_GITHUB_LOGIN.
	BootstrapAdminGitHubLogin string
	// WebhookRoutes holds the handlers of the webhook events. When nil, the route serves
	// the default table, which acknowledges the installation events and ignores the rest.
	WebhookRoutes *hooks.Routes
	// UpdatePRs stores the update PRs of pull_request deliveries and wakes the ticket engine.
	// Without it those deliveries only start runs.
	UpdatePRs hooks.UpdatePRTracker
	// Queue is read by /readyz for the last worker contact. Without it the server is never
	// ready.
	Queue queue.Queue
}

// NewRouter builds the root HTTP handler with health and API routes.
func NewRouter() http.Handler {
	return newBaseRouter(nil)
}

// NewServerRouter builds the production handler with auth routes and session middleware.
func NewServerRouter(deps *ServerDeps) http.Handler {
	return newBaseRouter(deps)
}

func newBaseRouter(deps *ServerDeps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(requestLogger)
	r.Use(recoverer)

	r.Get("/healthz", Health)

	api := MountAPI(r)
	if deps != nil {
		var db pinger
		if deps.DB != nil {
			db = deps.DB
		}
		r.Get("/readyz", Ready(db, deps.Queue, time.Now))
		RegisterAll(api, deps)
		registerWebhook(r, deps)
	}

	return r
}

// registerWebhook serves POST /hooks/github outside /api/v1: GitHub authenticates by
// signature, not by session. Without WEBHOOK_URL no webhook is configured and the route
// is not mounted.
func registerWebhook(r chi.Router, deps *ServerDeps) {
	if deps.WebhookURL == "" || deps.Store == nil {
		return
	}
	routes := deps.WebhookRoutes
	if routes == nil {
		routes = hooks.NewRoutes()
	}
	if jobs, ok := deps.Queue.(hooks.RunEnqueuer); ok {
		hooks.RegisterRenovateHandlers(routes, hooks.RenovateTriggers{
			Queue:        jobs,
			Repositories: deps.Store,
			Bot:          deps.Store.GitHubApp(),
		})
	}
	if deps.UpdatePRs != nil {
		hooks.RegisterUpdatePRHandler(routes, hooks.UpdatePRs{
			Tracker:      deps.UpdatePRs,
			Repositories: deps.Store,
			Bot:          deps.Store.GitHubApp(),
		})
	}
	r.Post(hooks.Path, (&hooks.Receiver{
		Secrets:    deps.Store.GitHubApp(),
		Deliveries: deps.Store.WebhookDeliveries(),
		Routes:     routes,
		// Own limiter: the failure counts are not shared with the auth endpoints.
		SignatureFailure: apimw.NewRateLimiter().WebhookSignatureFailure,
	}).ServeHTTP)
}

// MountAPI registers the /api/v1 subrouter on r and returns it for route registration.
func MountAPI(r chi.Router) chi.Router {
	api := chi.NewRouter()
	api.Use(jsonContentType)
	api.NotFound(notFoundJSON)

	r.Mount(apiV1Prefix, api)
	return api
}

// serverTimeouts bound how long a client may hold a connection. WriteTimeout is longer than
// the 30-second outbound call of the connection-test endpoint, so that endpoint can answer.
var serverTimeouts = struct{ readHeader, read, write, idle time.Duration }{
	readHeader: 10 * time.Second,
	read:       30 * time.Second,
	write:      60 * time.Second,
	idle:       2 * time.Minute,
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: serverTimeouts.readHeader,
		ReadTimeout:       serverTimeouts.read,
		WriteTimeout:      serverTimeouts.write,
		IdleTimeout:       serverTimeouts.idle,
	}
}

// Serve listens on addr until ctx is cancelled, then shuts down gracefully.
func Serve(ctx context.Context, addr string, handler http.Handler) error {
	if host, _, err := net.SplitHostPort(addr); err == nil && host != "127.0.0.1" {
		return errors.New("server must bind to 127.0.0.1 only")
	}

	srv := newHTTPServer(addr, handler)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func jsonContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

func notFoundJSON(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"code":    "NOT_FOUND",
			"message": "not found",
		},
	})
}

// ListRoutes returns registered HTTP method/path pairs for tests and diagnostics.
func ListRoutes(r chi.Router) ([]string, error) {
	var paths []string
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		paths = append(paths, method+" "+route)
		return nil
	})
	return paths, err
}

// recoverer answers 500 to a request whose handler panicked and logs the panic and its stack
// through the default logger, so they are redacted like every other line; chi's own recoverer
// writes the stack to stderr directly.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if rec == http.ErrAbortHandler {
				panic(rec)
			}
			slog.Error("request: handler panicked", "panic", rec, "stack", string(debug.Stack()))
			if r.Header.Get("Connection") != "Upgrade" {
				w.WriteHeader(http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration", time.Since(start),
		)
	})
}
