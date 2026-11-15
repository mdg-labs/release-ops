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
	"github.com/mdg-labs/release-ops/internal/renovate/redact"
)

const (
	// DefaultClaimWait is how long a claim waits for a job before it answers 204.
	DefaultClaimWait = 25 * time.Second
	// WriteTimeout is the write timeout the internal listener runs with: it outlasts the
	// longest claim wait with room to write the response, so a long-poll is never cut off.
	WriteTimeout = DefaultClaimWait + 30*time.Second
	// maxBodyBytes bounds every request body but a result's.
	maxBodyBytes = 64 << 10
	// maxResultBytes bounds a result: a run record holds a log excerpt and a report of up to
	// their caps each, which JSON escaping can double.
	maxResultBytes = 512 << 10
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
	// GlobalConfigRepo and GlobalConfigRef locate the global Renovate config the worker
	// fetches at job start; both are sent with every claimed job. Empty means the instance
	// mounts the config into the worker.
	GlobalConfigRepo, GlobalConfigRef string
	// AfterResult, when set, is called with the repository's id once a posted result has been
	// stored, whether the attempt succeeded or failed: Renovate may have changed the
	// repository's PRs either way, and the PR reconcile follows (specs.html section 5.1). It
	// runs on the request's goroutine and must not block.
	AfterResult func(repositoryID string)
}

type handler struct {
	q                 queue.Queue
	secretSum         [sha256.Size]byte
	claimWait         time.Duration
	tokens            TokenIssuer
	tokenRepositories []string
	globalConfigRepo  string
	globalConfigRef   string
	// redactor strips the worker secret and anything secret-shaped from a stored run record,
	// as a second line behind the worker's own redaction.
	redactor    *redact.Redactor
	afterResult func(repositoryID string)
}

// NewHandler returns the worker routes over q. Every request, including one for a path no
// route serves, is authenticated first; a path outside the table then answers 404.
func NewHandler(q queue.Queue, opts Options) http.Handler {
	h := &handler{
		q: q, secretSum: sha256.Sum256([]byte(opts.Secret)), claimWait: opts.ClaimWait,
		tokens: opts.Tokens, tokenRepositories: slices.Clone(opts.TokenRepositories),
		globalConfigRepo: opts.GlobalConfigRepo, globalConfigRef: opts.GlobalConfigRef,
		redactor: redact.New(opts.Secret), afterResult: opts.AfterResult,
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
			job.GlobalConfigRepo, job.GlobalConfigRef = h.globalConfigRepo, h.globalConfigRef
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
	if !decodeLimited(w, r, &req, maxResultBytes) {
		return
	}
	id := r.PathValue("id")
	repositoryID := ""
	if h.afterResult != nil {
		if held, err := h.q.HeldJob(r.Context(), id, req.LeaseOwner); err == nil {
			repositoryID = held.RepositoryID
		} else if !errors.Is(err, queue.ErrLeaseLost) {
			slog.Warn("internal api: the repository of a result could not be read; no PR reconcile follows", "job", id, "err", err)
		}
	}
	var run *queue.RunRecord
	if req.Run != nil {
		rec, msg := h.checkedRun(*req.Run)
		if msg != "" {
			writeError(w, http.StatusUnprocessableEntity, msg)
			return
		}
		run = &rec
	}
	var err error
	switch req.Outcome {
	case queue.OutcomeSucceeded:
		if run != nil {
			err = h.q.CompleteRun(r.Context(), id, req.LeaseOwner, *run)
		} else {
			err = h.q.Complete(r.Context(), id, req.LeaseOwner)
		}
	case queue.OutcomeFailed:
		_, err = h.q.Fail(r.Context(), id, req.LeaseOwner, queue.FailParams{
			RetryAfter: time.Duration(max(req.RetryAfterSeconds, 0)) * time.Second,
			Run:        run,
		})
	default:
		writeError(w, http.StatusUnprocessableEntity, "outcome must be succeeded or failed")
		return
	}
	h.finish(w, r, "result", err)
	if err == nil && repositoryID != "" {
		h.afterResult(repositoryID)
	}
}

// checkedRun validates a posted run record against the caps and redacts it; msg is the reason
// it is refused, empty when it is accepted.
func (h *handler) checkedRun(run queue.RunRecord) (rec queue.RunRecord, msg string) {
	switch {
	case run.ReportStatus != "" && run.ReportStatus != queue.ReportOK && run.ReportStatus != queue.ReportMissing && run.ReportStatus != queue.ReportUnreadable:
		return rec, "report_status must be ok, missing or unreadable"
	case len(run.LogExcerpt) > queue.MaxLogExcerptBytes:
		return rec, "log_excerpt is too large"
	case len(run.Report) > queue.MaxReportBytes:
		return rec, "report is too large"
	case len(run.ErrorMessage) > queue.MaxErrorMessageBytes || len(run.ErrorCode) > 64:
		return rec, "error_code or error_message is too large"
	}
	run.ErrorCode = h.redactor.String(run.ErrorCode)
	run.ErrorMessage = h.redactor.String(run.ErrorMessage)
	run.LogExcerpt = h.redactor.String(run.LogExcerpt)
	run.Report = h.redactor.String(run.Report)
	return run, ""
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
	return decodeLimited(w, r, v, maxBodyBytes)
}

func decodeLimited(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
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
