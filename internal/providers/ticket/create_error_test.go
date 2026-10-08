package ticket_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mdg-labs/release-ops/internal/providers/ticket"
)

// createProviders returns one provider per tracker kind, all pointed at baseURL.
func createProviders(t *testing.T, baseURL string, client *http.Client) map[string]ticket.TicketProvider {
	t.Helper()
	kaneo, err := ticket.NewKaneoProvider(baseURL, "key", client)
	if err != nil {
		t.Fatalf("NewKaneoProvider: %v", err)
	}
	jira, err := ticket.NewJiraProvider(baseURL, "user@company.com", "token", client)
	if err != nil {
		t.Fatalf("NewJiraProvider: %v", err)
	}
	linear, err := ticket.NewLinearProviderWithEndpoint("key", baseURL, client)
	if err != nil {
		t.Fatalf("NewLinearProviderWithEndpoint: %v", err)
	}
	return map[string]ticket.TicketProvider{"kaneo": kaneo, "jira": jira, "linear": linear}
}

func createInput() ticket.TicketInput {
	return ticket.TicketInput{
		Title:   "Release v1.0.0",
		Project: ticket.TicketProject{ExternalProjectID: "proj-1"},
	}
}

// TestCreateDefinitelyNotStored pins the rule the poll engine relies on to decide whether
// a failed create may be retried: true only for a 4xx answer or a request that never left,
// false for every failure after which the tracker may have stored the ticket.
func TestCreateDefinitelyNotStored(t *testing.T) {
	t.Parallel()

	answer := func(status int, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}
	}
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    bool
	}{
		{"400 is rejected", answer(http.StatusBadRequest, `{"error":"bad"}`), true},
		{"401 is rejected", answer(http.StatusUnauthorized, ``), true},
		{"404 is rejected", answer(http.StatusNotFound, ``), true},
		{"408 is rejected", answer(http.StatusRequestTimeout, ``), true},
		{"429 is rejected", answer(http.StatusTooManyRequests, ``), true},
		{"500 is unknown", answer(http.StatusInternalServerError, ``), false},
		{"502 is unknown", answer(http.StatusBadGateway, ``), false},
		{"503 is unknown", answer(http.StatusServiceUnavailable, ``), false},
		{"2xx with an unreadable body is unknown", answer(http.StatusOK, `not json`), false},
		{"2xx with an empty object is unknown", answer(http.StatusCreated, `{}`), false},
		{"connection dropped mid-request is unknown", func(w http.ResponseWriter, _ *http.Request) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(tc.handler)
			t.Cleanup(server.Close)
			for kind, provider := range createProviders(t, server.URL, server.Client()) {
				_, err := provider.CreateTicket(context.Background(), createInput())
				if err == nil {
					t.Fatalf("%s: CreateTicket succeeded, want an error", kind)
				}
				if got := ticket.CreateDefinitelyNotStored(err); got != tc.want {
					t.Errorf("%s: CreateDefinitelyNotStored(%v) = %v, want %v", kind, err, got, tc.want)
				}
			}
		})
	}
}

func TestCreateDefinitelyNotStoredTimeoutWhileInFlightIsUnknown(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); server.Close() })

	for kind, provider := range createProviders(t, server.URL, server.Client()) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		_, err := provider.CreateTicket(ctx, createInput())
		cancel()
		if err == nil {
			t.Fatalf("%s: CreateTicket succeeded, want a timeout", kind)
		}
		if ticket.CreateDefinitelyNotStored(err) {
			t.Errorf("%s: a deadline while the request was in flight (%v) must be unknown", kind, err)
		}
	}
}

func TestCreateDefinitelyNotStoredWhenTheRequestNeverLeft(t *testing.T) {
	t.Parallel()
	closed := httptest.NewServer(http.NotFoundHandler())
	url := closed.URL
	closed.Close() // nothing listens any more: the dial is refused

	for kind, provider := range createProviders(t, url, nil) {
		_, err := provider.CreateTicket(context.Background(), createInput())
		if err == nil {
			t.Fatalf("%s: CreateTicket succeeded against a closed server", kind)
		}
		if !ticket.CreateDefinitelyNotStored(err) {
			t.Errorf("%s: a refused connection (%v) must count as not stored", kind, err)
		}
	}

	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	for kind, provider := range createProviders(t, server.URL, server.Client()) {
		input := createInput()
		input.Project.ExternalProjectID = " "
		_, err := provider.CreateTicket(context.Background(), input)
		if err == nil || !ticket.CreateDefinitelyNotStored(err) {
			t.Errorf("%s: a missing project id (%v) fails before any request, want not stored", kind, err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err = provider.CreateTicket(ctx, createInput())
		if err == nil || !ticket.CreateDefinitelyNotStored(err) {
			t.Errorf("%s: a context cancelled before the call (%v) sends nothing, want not stored", kind, err)
		}
	}
}

func TestCreateDefinitelyNotStoredNil(t *testing.T) {
	t.Parallel()
	if ticket.CreateDefinitelyNotStored(nil) {
		t.Fatal("nil error must not count as not stored")
	}
}
