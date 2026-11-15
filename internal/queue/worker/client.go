// Package worker is the release-ops worker: it claims jobs from the app's internal listener,
// runs each through the handler registered for its kind and reports the result. It has no
// database access and never imports the store; the internal routes are its only way in.
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
)

const (
	// claimTimeout bounds a claim request: the app's long-poll wait plus slack.
	claimTimeout = 60 * time.Second
	// callTimeout bounds every other request.
	callTimeout = 15 * time.Second
	// maxResponseBytes bounds what the worker reads of a response.
	maxResponseBytes = 64 << 10
)

// ErrLeaseLost is returned when the app answers that the worker no longer holds the lease.
var ErrLeaseLost = errors.New("lease is not held for this job")

// Client talks to the app's internal listener.
type Client struct {
	base   string
	secret string
	http   *http.Client
}

// NewClient returns a client for the internal listener at baseURL, authenticated with the
// worker secret. It does not follow redirects, so the secret goes to the configured host only.
func NewClient(baseURL, secret string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	hc := *httpClient
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: strings.TrimRight(baseURL, "/"), secret: secret, http: &hc}
}

// Claim long-polls for a job. It returns nil when the wait ended with no job.
func (c *Client) Claim(ctx context.Context) (*queue.ClaimedJob, error) {
	ctx, cancel := context.WithTimeout(ctx, claimTimeout)
	defer cancel()
	status, body, err := c.post(ctx, queue.PathClaim, struct{}{})
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusNoContent:
		return nil, nil
	case http.StatusOK:
		var job queue.ClaimedJob
		if err := json.Unmarshal(body, &job); err != nil {
			return nil, fmt.Errorf("claim: decode response: %w", err)
		}
		return &job, nil
	default:
		return nil, statusError("claim", status)
	}
}

// Token asks for the installation token of a job this worker holds. The app refuses a job the
// worker does not hold (ErrLeaseLost); the token and its expiry are returned as the app sent them.
func (c *Client) Token(ctx context.Context, jobID, leaseOwner string) (queue.TokenResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	status, body, err := c.post(ctx, jobPath(queue.PathToken, jobID), queue.LeaseRequest{LeaseOwner: leaseOwner})
	if err != nil {
		return queue.TokenResponse{}, err
	}
	if status != http.StatusOK {
		return queue.TokenResponse{}, statusError("token", status)
	}
	var tok queue.TokenResponse
	if err := json.Unmarshal(body, &tok); err != nil || tok.Token == "" {
		return queue.TokenResponse{}, errors.New("token: the response holds no token")
	}
	return tok, nil
}

// Heartbeat renews the lease. It reports an abort when the app says to stop.
func (c *Client) Heartbeat(ctx context.Context, jobID, leaseOwner string) (queue.HeartbeatResult, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	status, body, err := c.post(ctx, jobPath(queue.PathHeartbeat, jobID), queue.LeaseRequest{LeaseOwner: leaseOwner})
	if err != nil {
		return queue.HeartbeatResult{}, err
	}
	if status != http.StatusOK {
		return queue.HeartbeatResult{}, statusError("heartbeat", status)
	}
	var res queue.HeartbeatResult
	if err := json.Unmarshal(body, &res); err != nil {
		return queue.HeartbeatResult{}, fmt.Errorf("heartbeat: decode response: %w", err)
	}
	return res, nil
}

// Result reports the outcome of an attempt.
func (c *Client) Result(ctx context.Context, jobID string, req queue.ResultRequest) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return c.noContent(ctx, "result", jobPath(queue.PathResult, jobID), req)
}

// Release gives the lease back without a result.
func (c *Client) Release(ctx context.Context, jobID, leaseOwner string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return c.noContent(ctx, "release", jobPath(queue.PathRelease, jobID), queue.LeaseRequest{LeaseOwner: leaseOwner})
}

func (c *Client) noContent(ctx context.Context, op, path string, req any) error {
	status, _, err := c.post(ctx, path, req)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return statusError(op, status)
	}
	return nil
}

func (c *Client) post(ctx context.Context, path string, payload any) (int, []byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, body, nil
}

func jobPath(pattern, jobID string) string {
	return strings.Replace(pattern, "{id}", jobID, 1)
}

func statusError(op string, status int) error {
	if status == http.StatusConflict {
		return fmt.Errorf("%s: %w", op, ErrLeaseLost)
	}
	return fmt.Errorf("%s: unexpected status %d", op, status)
}
