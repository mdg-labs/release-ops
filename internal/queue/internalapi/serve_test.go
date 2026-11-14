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

// The claim long-poll must be answered before the server's write timeout cuts the response:
// the production wait is shorter than the production WriteTimeout, with room to write.
func TestWriteTimeoutOutlastsTheClaimWait(t *testing.T) {
	if DefaultClaimWait >= WriteTimeout {
		t.Fatalf("DefaultClaimWait %v must stay below WriteTimeout %v", DefaultClaimWait, WriteTimeout)
	}
	if WriteTimeout-DefaultClaimWait < 10*time.Second {
		t.Errorf("WriteTimeout %v leaves under 10s to write a claim answer after the %v wait", WriteTimeout, DefaultClaimWait)
	}
}

// The same relation at a small scale, through serve: a claim that waits out its whole wait
// still gets its 204 when the write timeout is longer than the wait.
func TestClaimWaitEndsInAnAnswerInsideTheWriteTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	const wait = 300 * time.Millisecond
	h := NewHandler(idleQueue{changed: make(chan struct{})}, Options{Secret: "s", ClaimWait: wait})

	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- serve(ctx, ln, h, wait+time.Second) }()
	t.Cleanup(func() {
		cancel()
		if err := <-served; err != nil {
			t.Errorf("serve = %v, want a clean shutdown", err)
		}
	})

	req, _ := http.NewRequest(http.MethodPost, "http://"+ln.Addr().String()+queue.PathClaim, strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer s")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("claim = %d, want 204 after the wait", resp.StatusCode)
	}
}
