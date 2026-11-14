package internalapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/queue"
)

const (
	// DefaultClaimWait is how long a claim waits for a job before it answers 204.
	DefaultClaimWait = 25 * time.Second
	// maxBodyBytes bounds every request body.
	maxBodyBytes = 64 << 10
)

// TokenIssuer mints the installation access token of a job.
type TokenIssuer interface {
	// JobToken returns a token for a job, scoped to the target repository (owner/name) plus the
	// extra repositories.
	JobToken(ctx context.Context, installationID int64, target string, extra []string) (github.InstallationToken, error)
}

// Options configures the handler.
type Options struct {
	// Secret is the shared worker secret every request must carry as a bearer credential.
	Secret string
	// ClaimWait bounds a claim's long-poll; it must stay below the server's write timeout.
	ClaimWait time.Duration
	// Tokens mints the job tokens of the token route; without it the route is not served.
	Tokens TokenIssuer
	// TokenRepositories are the repositories a job's token covers besides the job's own, from
	// instance configuration: a request never names repositories.
	TokenRepositories []string
}

type handler struct {
	q                 queue.Queue
	secretSum         [sha256.Size]byte
	claimWait         time.Duration
	tokens            TokenIssuer
	tokenRepositories []string
}

// NewHandler returns the worker routes over q. Every request, including one for a path no
// route serves, is authenticated first; a path outside the table then answers 404.
func NewHandler(q queue.Queue, opts Options) http.Handler {
	h := &handler{
		q: q, secretSum: sha256.Sum256([]byte(opts.Secret)), claimWait: opts.ClaimWait,
		tokens: opts.Tokens, tokenRepositories: slices.Clone(opts.TokenRepositories),
	}
	if h.claimWait <= 0 {
		h.claimWait = DefaultClaimWait
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST "+queue.PathClaim, h.claim)
	if h.tokens != nil {
		mux.HandleFunc("POST "+queue.PathToken, h.token)
	}
	mux.HandleFunc("POST "+queue.PathHeartbeat, h.heartbeat)
	mux.HandleFunc("POST "+queue.PathResult, h.result)
	mux.HandleFunc("POST "+queue.PathRelease, h.release)
	return h.authenticate(mux)
}

// authenticate compares the bearer secret in constant time, over fixed-length digests so
// the comparison does not depend on the length of what the client sent.
func (h *handler) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		sum := sha256.Sum256([]byte(token))
		if !ok || subtle.ConstantTimeCompare(sum[:], h.secretSum[:]) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *handler) claim(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	timer := time.NewTimer(h.claimWait)
	defer timer.Stop()

	for {
		changed := h.q.Changed()
		job, err := h.q.Claim(ctx)
		switch {
		case err == nil:
			if ctx.Err() != nil {
				h.releaseUnsent(job)
				return
			}
			writeJSON(w, http.StatusOK, job)
			return
		case !errors.Is(err, queue.ErrNoJob):
			if ctx.Err() == nil {
				slog.Error("internal api: claim failed", "err", err)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
			return
		}

		select {
		case <-changed:
		case <-timer.C:
			w.WriteHeader(http.StatusNoContent)
			return
		case <-ctx.Done():
			return
		}
	}
}

// releaseUnsent gives back a job claimed for a client that has gone, so its lease does not
// have to expire.
func (h *handler) releaseUnsent(job *queue.ClaimedJob) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.q.Release(ctx, job.ID, job.LeaseOwner); err != nil {
		slog.Error("internal api: release of an unsent claim failed", "job", job.ID, "err", err)
	}
}

// token issues the installation token of a job the caller holds under a live lease. The
// repositories come from the job and from instance configuration; the request carries only
// the lease. Any job the caller does not hold gets 409 and no call to GitHub.
func (h *handler) token(w http.ResponseWriter, r *http.Request) {
	var req queue.LeaseRequest
	if !decode(w, r, &req) {
		return
	}
	ctx := r.Context()
	id := r.PathValue("id")
	job, err := h.q.HeldJob(ctx, id, req.LeaseOwner)
	switch {
	case errors.Is(err, queue.ErrLeaseLost):
		writeError(w, http.StatusConflict, "lease is not held for this job")
		return
	case err != nil:
		serverError(w, r, "token", err)
		return
	}

	tok, err := h.tokens.JobToken(ctx, job.InstallationID, job.RepositoryFullName, h.tokenRepositories)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("internal api: token request failed", "job", id, "err", err)
			writeError(w, http.StatusBadGateway, "token could not be issued")
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, queue.TokenResponse{Token: tok.Value(), ExpiresAt: tok.ExpiresAt})
}

func (h *handler) heartbeat(w http.ResponseWriter, r *http.Request) {
	var req queue.LeaseRequest
	if !decode(w, r, &req) {
		return
	}
	res, err := h.q.Heartbeat(r.Context(), r.PathValue("id"), req.LeaseOwner)
	if err != nil {
		serverError(w, r, "heartbeat", err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *handler) result(w http.ResponseWriter, r *http.Request) {
	var req queue.ResultRequest
	if !decode(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	var err error
	switch req.Outcome {
	case queue.OutcomeSucceeded:
		err = h.q.Complete(r.Context(), id, req.LeaseOwner)
	case queue.OutcomeFailed:
		_, err = h.q.Fail(r.Context(), id, req.LeaseOwner, queue.FailParams{
			RetryAfter: time.Duration(max(req.RetryAfterSeconds, 0)) * time.Second,
		})
	default:
		writeError(w, http.StatusUnprocessableEntity, "outcome must be succeeded or failed")
		return
	}
	h.finish(w, r, "result", err)
}

func (h *handler) release(w http.ResponseWriter, r *http.Request) {
	var req queue.LeaseRequest
	if !decode(w, r, &req) {
		return
	}
	h.finish(w, r, "release", h.q.Release(r.Context(), r.PathValue("id"), req.LeaseOwner))
}

func (h *handler) finish(w http.ResponseWriter, r *http.Request, op string, err error) {
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, queue.ErrLeaseLost):
		writeError(w, http.StatusConflict, "lease is not held for this job")
	default:
		serverError(w, r, op, err)
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	if r.Context().Err() != nil {
		return
	}
	slog.Error("internal api: request failed", "op", op, "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
