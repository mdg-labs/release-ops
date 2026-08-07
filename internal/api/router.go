package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/mdg-labs/release-ops/internal/api/handlers"
	"github.com/mdg-labs/release-ops/internal/store"
	storedb "github.com/mdg-labs/release-ops/internal/store/db"
)

const apiV1Prefix = "/api/v1"

// ServerDeps holds dependencies for the production HTTP router.
type ServerDeps struct {
	DB                 *sql.DB
	Session            *scs.SessionManager
	Queries            *storedb.Queries
	Store              *store.Store
	PollRunner         handlers.PollRunner
	IntegrationTester  handlers.IntegrationTester
	NotificationTester handlers.NotificationTester
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
	r.Use(middleware.Recoverer)

	r.Get("/healthz", Health)

	api := MountAPI(r)
	if deps != nil {
		RegisterAll(api, deps)
	}

	return r
}

// MountAPI registers the /api/v1 subrouter on r and returns it for route registration.
func MountAPI(r chi.Router) chi.Router {
	api := chi.NewRouter()
	api.Use(jsonContentType)
	api.NotFound(notFoundJSON)

	r.Mount(apiV1Prefix, api)
	return api
}

// Serve listens on addr until ctx is cancelled, then shuts down gracefully.
func Serve(ctx context.Context, addr string, handler http.Handler) error {
	if host, _, err := net.SplitHostPort(addr); err == nil && host != "127.0.0.1" {
		return errors.New("server must bind to 127.0.0.1 only")
	}

	srv := &http.Server{
		Addr:    addr,
		Handler: handler,
	}

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
