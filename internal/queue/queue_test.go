package queue

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBackoffDoublesAndCapsWithJitter(t *testing.T) {
	cfg := Config{BackoffBase: 30 * time.Second, BackoffMax: 5 * time.Minute, BackoffJitter: 0.2}

	for _, tc := range []struct {
		attempts int
		rnd      float64
		want     time.Duration
	}{
		{1, 0.5, 30 * time.Second},
		{2, 0.5, 60 * time.Second},
		{3, 0.5, 120 * time.Second},
		{6, 0.5, 5 * time.Minute},
		{20, 0.5, 5 * time.Minute},
		{1, 0, 24 * time.Second},
		{1, 0.999999, 36 * time.Second},
	} {
		got := cfg.Backoff(tc.attempts, tc.rnd)
		if diff := got - tc.want; diff < -time.Millisecond || diff > time.Millisecond {
			t.Errorf("Backoff(%d, %v) = %v, want %v", tc.attempts, tc.rnd, got, tc.want)
		}
	}
}

func TestNotifierWakesEveryWaiterOnce(t *testing.T) {
	var n Notifier
	a, b := n.Changed(), n.Changed()
	n.Notify()
	for i, ch := range []<-chan struct{}{a, b} {
		select {
		case <-ch:
		default:
			t.Fatalf("waiter %d was not woken", i)
		}
	}
	next := n.Changed()
	select {
	case <-next:
		t.Fatal("a later waiter was woken by an earlier Notify")
	default:
	}
	n.Notify()
	n.Notify()
}

type countingQueue struct {
	Queue
	reaps chan struct{}
	err   error
}

func (q *countingQueue) Reap(context.Context) (ReapResult, error) {
	q.reaps <- struct{}{}
	return ReapResult{}, q.err
}

func TestRunReaperReapsAtStartAndKeepsGoingAfterAnError(t *testing.T) {
	q := &countingQueue{reaps: make(chan struct{}, 16), err: errors.New("boom")}
	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunReaper(ctx, q, 5*time.Millisecond, func(err error) { errs <- err })
	}()

	for i := range 3 {
		select {
		case <-q.reaps:
		case <-time.After(2 * time.Second):
			t.Fatalf("reap %d did not happen", i+1)
		}
	}
	if len(errs) == 0 {
		t.Error("a failing reap was not reported")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunReaper did not stop")
	}
}
