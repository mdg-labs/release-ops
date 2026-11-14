package internalapi

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
)

type idleQueue struct {
	queue.Queue
	changed chan struct{}
}

func (q idleQueue) Changed() <-chan struct{} { return q.changed }

func (idleQueue) Claim(context.Context) (*queue.ClaimedJob, error) { return nil, queue.ErrNoJob }

// Shutdown must not wait out a claim long-poll: the requests in flight are cancelled.
func TestShutdownEndsAWaitingClaim(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(idleQueue{changed: make(chan struct{})}, Options{Secret: "s", ClaimWait: time.Minute})

	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- serve(ctx, ln, h, 2*time.Minute) }()

	claimDone := make(chan struct{})
	go func() {
		defer close(claimDone)
		req, _ := http.NewRequest(http.MethodPost, "http://"+ln.Addr().String()+queue.PathClaim, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer s")
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}()
	time.Sleep(200 * time.Millisecond)

	cancel()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("serve = %v, want a clean shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown waited for the claim long-poll")
	}
	<-claimDone
}
