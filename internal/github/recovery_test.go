package github_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/github/githubtest"
	"github.com/mdg-labs/release-ops/internal/store"
)

type recoveryRig struct {
	fake     *githubtest.Fake
	recovery *github.Recovery
}

func newRecoveryRig(t *testing.T, creds github.CredentialsSource) *recoveryRig {
	t.Helper()
	fake := githubtest.New(t, func() time.Time { return t0 })
	if creds == nil {
		creds = staticCreds{appID: githubtest.AppID, pem: fake.PEM}
	}
	app := github.NewAppClient(github.NewClient(fake.URL, 5*time.Second), creds, github.AppClientOptions{Now: func() time.Time { return t0 }})
	return &recoveryRig{fake: fake, recovery: github.NewRecovery(app, github.RecoveryOptions{Now: func() time.Time { return t0 }})}
}

func delivery(id int64, guid string, age time.Duration, status int) githubtest.Delivery {
	return githubtest.Delivery{ID: id, GUID: guid, DeliveredAt: t0.Add(-age), StatusCode: status, Event: "installation"}
}

func TestRedeliverRepeatsEachFailedGUIDOnceWithTheAppJWT(t *testing.T) {
	t.Parallel()
	r := newRecoveryRig(t, nil)
	// Newest first, as GitHub lists them. guid-a failed twice and is repeated once, from its
	// latest attempt; guid-b failed and was later accepted; guid-c did not get an answer.
	r.fake.SetDeliveries(
		delivery(60, "guid-c", time.Minute, 0),
		delivery(50, "guid-b", 2*time.Minute, 202),
		delivery(40, "guid-a", 3*time.Minute, 500),
		delivery(30, "guid-b", 4*time.Minute, 500),
		delivery(20, "guid-a", 5*time.Minute, 502),
	)

	got, err := r.recovery.Redeliver(context.Background())
	if err != nil || got != 2 {
		t.Fatalf("Redeliver() = %d, %v, want 2 redeliveries", got, err)
	}
	if ids := r.fake.Redeliveries(); !slices.Equal(ids, []int64{40, 60}) {
		t.Errorf("redelivered %v, want [40 60]: the latest failed attempt of guid-a and of guid-c, oldest first", ids)
	}
	for _, req := range r.fake.Requests() {
		if !req.JWTValid {
			t.Errorf("%s %s was not made with a valid App JWT", req.Method, req.Path)
		}
	}
}

func TestRedeliverSkipsAGUIDThatSucceededSince(t *testing.T) {
	t.Parallel()
	r := newRecoveryRig(t, nil)
	r.fake.SetDeliveries(
		delivery(40, "guid-a", time.Minute, 202),
		delivery(30, "guid-a", 2*time.Minute, 500),
	)

	got, err := r.recovery.Redeliver(context.Background())
	if err != nil || got != 0 || len(r.fake.Redeliveries()) != 0 {
		t.Fatalf("Redeliver() = %d, %v, redelivered %v, want nothing for an already handled delivery", got, err, r.fake.Redeliveries())
	}
}

func TestRedeliverLooksBackOnlyWithinTheLookbackAndStopsReading(t *testing.T) {
	t.Parallel()
	r := newRecoveryRig(t, nil)
	r.fake.SetPageSize(2)
	r.fake.SetDeliveries(
		delivery(60, "guid-new", time.Hour, 500),
		delivery(50, "guid-edge", 23*time.Hour, 500),
		delivery(40, "guid-old", 25*time.Hour, 500),
		delivery(30, "guid-older", 26*time.Hour, 500),
		delivery(20, "guid-oldest", 27*time.Hour, 500),
	)

	got, err := r.recovery.Redeliver(context.Background())
	if err != nil || got != 2 {
		t.Fatalf("Redeliver() = %d, %v, want 2", got, err)
	}
	if ids := r.fake.Redeliveries(); !slices.Equal(ids, []int64{50, 60}) {
		t.Errorf("redelivered %v, want [50 60]: nothing older than 24 hours", ids)
	}
	pages := 0
	for _, req := range r.fake.Requests() {
		if req.Method == http.MethodGet && req.Path == "/app/hook/deliveries" {
			pages++
		}
	}
	if pages != 2 {
		t.Errorf("read %d pages, want 2: the listing ends at the first delivery outside the lookback", pages)
	}
}

func TestRedeliverFollowsPagesAndTheConfiguredHostOnly(t *testing.T) {
	t.Parallel()
	r := newRecoveryRig(t, nil)
	r.fake.SetPageSize(1)
	r.fake.SetDeliveries(
		delivery(40, "guid-a", time.Minute, 500),
		delivery(30, "guid-b", 2*time.Minute, 500),
		delivery(20, "guid-c", 3*time.Minute, 500),
	)
	if got, err := r.recovery.Redeliver(context.Background()); err != nil || got != 3 {
		t.Fatalf("Redeliver() over pages = %d, %v, want 3", got, err)
	}

	r.fake.LinkTo("http://elsewhere.invalid")
	before := len(r.fake.Redeliveries())
	got, err := r.recovery.Redeliver(context.Background())
	if err == nil || got != 0 || len(r.fake.Redeliveries()) != before {
		t.Errorf("Redeliver() with a next page on another host = %d, %v, want an error and no redelivery", got, err)
	}
}

func TestRedeliverReportsAFailedListAndRedeliversNothing(t *testing.T) {
	t.Parallel()
	r := newRecoveryRig(t, nil)
	r.fake.SetDeliveries(delivery(40, "guid-a", time.Minute, 500))
	r.fake.FailDeliveryList(http.StatusBadGateway)

	got, err := r.recovery.Redeliver(context.Background())
	if !errors.Is(err, github.ErrRequestFailed) || got != 0 || len(r.fake.Redeliveries()) != 0 {
		t.Errorf("Redeliver() = %d, %v, want a request error and no redelivery", got, err)
	}
	if err != nil && strings.Contains(err.Error(), "guid-a") {
		t.Errorf("error %q names a delivery GUID", err)
	}
}

func TestRedeliverContinuesPastAFailedRedelivery(t *testing.T) {
	t.Parallel()
	r := newRecoveryRig(t, nil)
	r.fake.SetDeliveries(
		delivery(40, "guid-a", time.Minute, 500),
		delivery(30, "guid-b", 2*time.Minute, 500),
		delivery(20, "guid-c", 3*time.Minute, 500),
	)
	r.fake.FailRedelivery(30, http.StatusNotFound)

	got, err := r.recovery.Redeliver(context.Background())
	if !errors.Is(err, github.ErrRequestFailed) || got != 2 {
		t.Fatalf("Redeliver() = %d, %v, want 2 redeliveries and the error of the third", got, err)
	}
	if ids := r.fake.Redeliveries(); !slices.Equal(ids, []int64{20, 30, 40}) {
		t.Errorf("redelivery was requested for %v, want all three", ids)
	}
}

func TestRedeliverDoesNothingBeforeTheAppIsStored(t *testing.T) {
	t.Parallel()
	r := newRecoveryRig(t, staticCreds{err: store.ErrGitHubAppNotFound})

	got, err := r.recovery.Redeliver(context.Background())
	if err != nil || got != 0 || len(r.fake.Requests()) != 0 {
		t.Errorf("Redeliver() = %d, %v with %d requests, want nothing and no request", got, err, len(r.fake.Requests()))
	}
}

func TestRedeliverRejectsAListThatIsNotDeliveries(t *testing.T) {
	t.Parallel()
	r := newRecoveryRig(t, nil)
	r.fake.SetDeliveries(githubtest.Delivery{ID: 40, DeliveredAt: t0.Add(-time.Minute), StatusCode: 500})

	got, err := r.recovery.Redeliver(context.Background())
	if !errors.Is(err, github.ErrRequestFailed) || got != 0 {
		t.Errorf("Redeliver() = %d, %v, want a request error for a delivery without a GUID", got, err)
	}
}

func TestRecoveryRunRedeliversAtOnceAndOnEveryTick(t *testing.T) {
	t.Parallel()
	r := newRecoveryRig(t, nil)
	r.fake.FailDeliveryList(http.StatusServiceUnavailable)

	ctx, cancel := context.WithCancel(context.Background())
	type result struct {
		n   int
		err error
	}
	ticks := make(chan time.Time)
	reports := make(chan result)
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.recovery.Run(ctx, ticks, func(n int, err error) { reports <- result{n, err} })
	}()
	next := func() result {
		t.Helper()
		select {
		case res := <-reports:
			return res
		case <-time.After(20 * time.Second):
			t.Fatal("no pass reported")
			return result{}
		}
	}

	if res := next(); !errors.Is(res.err, github.ErrRequestFailed) {
		t.Fatalf("start-up pass = %+v, want the failed list reported", res)
	}
	r.fake.FailDeliveryList(0)
	r.fake.SetDeliveries(delivery(40, "guid-a", time.Minute, 500))
	ticks <- time.Now()
	if res := next(); res.err != nil || res.n != 1 {
		t.Fatalf("pass after a failed one = %+v, want the failed delivery redelivered: a failed list does not stop the loop", res)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("Run did not stop with its context")
	}
}
