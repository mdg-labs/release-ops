package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/mdg-labs/release-ops/internal/logging"
)

func TestHealthReturnsJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	Health(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var body healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("status field = %q, want ok", body.Status)
	}
}

func TestRouterRegisteredPaths(t *testing.T) {
	r := chi.NewRouter()
	r.Get("/healthz", Health)
	MountAPI(r)

	paths, err := ListRoutes(r)
	if err != nil {
		t.Fatalf("ListRoutes: %v", err)
	}

	if !slices.Contains(paths, "GET /healthz") {
		t.Fatalf("registered paths = %v, missing GET /healthz", paths)
	}

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/unregistered")
	if err != nil {
		t.Fatalf("GET /api/v1/unregistered: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestMountAPIExportsSubrouter(t *testing.T) {
	root := chi.NewRouter()
	api := MountAPI(root)

	if api == nil {
		t.Fatal("MountAPI returned nil subrouter")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/unknown", nil)
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
}

func TestNewRouterHealthEndpoint(t *testing.T) {
	srv := httptest.NewServer(NewRouter())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("body = %s, want JSON status ok", body)
	}
}

func TestServeBindsLoopbackOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := Serve(ctx, "0.0.0.0:0", NewRouter())
	if err == nil || !strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("Serve on 0.0.0.0 error = %v, want bind restriction error", err)
	}
}

func TestServeGracefulShutdownCompletesInFlightRequests(t *testing.T) {
	var started atomic.Bool
	release := make(chan struct{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started.Store(true)
		<-release
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: handler,
	}

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()

	respCh := make(chan *http.Response, 1)
	errClientCh := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get("http://" + ln.Addr().String())
		if err != nil {
			errClientCh <- err
			return
		}
		respCh <- resp
	}()

	deadline := time.Now().Add(2 * time.Second)
	for !started.Load() {
		if time.Now().After(deadline) {
			t.Fatal("in-flight request did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	shutdownDone := make(chan error, 1)
	go func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownDone <- srv.Shutdown(shutdownCtx)
	}()

	time.Sleep(50 * time.Millisecond)
	close(release)

	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not complete")
	}

	select {
	case err := <-errClientCh:
		t.Fatalf("client request failed: %v", err)
	case resp := <-respCh:
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	}

	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("Serve error = %v", err)
	}
}

func TestServerTimeoutsAreSet(t *testing.T) {
	srv := newHTTPServer("127.0.0.1:0", http.NotFoundHandler())

	if srv.ReadHeaderTimeout <= 0 || srv.ReadTimeout <= 0 || srv.WriteTimeout <= 0 || srv.IdleTimeout <= 0 {
		t.Fatalf("timeouts = header %v read %v write %v idle %v, want all set",
			srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout)
	}
	// The connection-test endpoint makes an outbound call of up to 30 seconds
	// (integrationtester.New) and must be able to answer it.
	if srv.WriteTimeout <= 30*time.Second {
		t.Errorf("WriteTimeout = %v, want more than the 30s outbound call", srv.WriteTimeout)
	}
	if srv.ReadHeaderTimeout >= srv.ReadTimeout {
		t.Errorf("ReadHeaderTimeout %v should be shorter than ReadTimeout %v", srv.ReadHeaderTimeout, srv.ReadTimeout)
	}
}

// A client that trickles its request headers is cut off by Serve's ReadHeaderTimeout. The
// timeout is shortened for the test; the server is built by Serve itself.
func TestServeCutsOffASlowHeaderClient(t *testing.T) {
	saved := serverTimeouts
	serverTimeouts.readHeader = 300 * time.Millisecond
	t.Cleanup(func() { serverTimeouts = saved })

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()

	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, addr, NewRouter()) }()
	t.Cleanup(func() {
		cancel()
		if err := <-served; err != nil {
			t.Errorf("serve = %v, want a clean shutdown", err)
		}
	})

	var conn net.Conn
	for deadline := time.Now().Add(5 * time.Second); ; {
		if conn, err = net.Dial("tcp", addr); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not start: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer func() { _ = conn.Close() }()

	// A request line and a header, then silence: the headers are never finished.
	if _, err := conn.Write([]byte("GET /healthz HTTP/1.1\r\nHost: x\r\nX-Slow: ")); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	start := time.Now()
	_, err = io.ReadAll(conn)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("the connection stayed open for %v, want it closed near the 300ms header timeout", elapsed)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		t.Fatalf("the server did not close the slow connection: %v", err)
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A handler that panics with a secret in the panic value is answered with 500, and the panic
// and its stack are logged through the default logger, redacted.
func TestARecoveredPanicIsLoggedThroughTheRedactingHandler(t *testing.T) {
	const secret = "opaque-secret-in-a-panic-value-0001"
	previous := slog.Default()
	out := &syncBuffer{}
	logging.Install(out)
	logging.Register(secret)
	t.Cleanup(func() {
		slog.SetDefault(previous)
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags)
	})

	r := chi.NewRouter()
	r.Use(requestLogger)
	r.Use(recoverer)
	r.Get("/boom", func(http.ResponseWriter, *http.Request) { panic(fmt.Errorf("handler failed with %s", secret)) })
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/boom")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("a panicking handler = %d, want 500", resp.StatusCode)
	}
	text := out.String()
	if strings.Contains(text, secret) || !strings.Contains(text, "handler panicked") || !strings.Contains(text, logging.Placeholder) || !strings.Contains(text, "status=500") {
		t.Fatalf("the panic was not logged redacted:\n%s", text)
	}
}
