package queue

import "time"

// Paths of the worker routes on the internal listener (specs.html § 2.1).
const (
	PathClaim     = "/internal/v1/jobs/claim"
	PathToken     = "/internal/v1/jobs/{id}/token"
	PathHeartbeat = "/internal/v1/jobs/{id}/heartbeat"
	PathResult    = "/internal/v1/jobs/{id}/result"
	PathRelease   = "/internal/v1/jobs/{id}/release"
)

// Outcomes a worker reports for an attempt.
const (
	OutcomeSucceeded = "succeeded"
	// OutcomeNoConfig is an attempt Renovate skipped because the repository has no config
	// (specs.html section 2.7): the job ends succeeded and its job_runs row no_config.
	OutcomeNoConfig = "no_config"
	OutcomeFailed   = "failed"
)

// LeaseRequest is the body of a heartbeat or a release.
type LeaseRequest struct {
	LeaseOwner string `json:"lease_owner"`
}

// ResultRequest is the body of a result.
type ResultRequest struct {
	LeaseOwner string `json:"lease_owner"`
	Outcome    string `json:"outcome"`
	// RetryAfterSeconds is the delay an upstream asked for with a failed outcome.
	RetryAfterSeconds int64 `json:"retry_after_seconds,omitempty"`
	// RateLimit is what the attempt's GitHub calls said about the installation's rate limit,
	// when anything; the app records it in the installation's shared budget.
	RateLimit *RateLimitReport `json:"rate_limit,omitempty"`
	// Run is the attempt's record; absent when the worker has nothing to report beyond the
	// outcome.
	Run *RunRecord `json:"run,omitempty"`
}

// RateLimitReport is the last rate-limit state the worker saw in a response of GitHub's API
// during an attempt. Renovate's own calls are not intercepted: the worker reads what Renovate
// logged of them.
type RateLimitReport struct {
	// Limited is true when the last such response refused the call because of a rate limit.
	Limited bool `json:"limited,omitempty"`
	// Remaining and Limit are X-RateLimit-Remaining and X-RateLimit-Limit.
	Remaining *int64 `json:"remaining,omitempty"`
	Limit     *int64 `json:"limit,omitempty"`
	// ResetUnix is X-RateLimit-Reset, a Unix time in seconds.
	ResetUnix int64 `json:"reset_unix,omitempty"`
	// RetryAfterSeconds is Retry-After.
	RetryAfterSeconds int64 `json:"retry_after_seconds,omitempty"`
}

// TokenResponse is the body of a successful token request: the installation access token for
// the job's repository and the preset repositories, and when GitHub expires it.
type TokenResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}
