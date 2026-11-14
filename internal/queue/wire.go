package queue

// Paths of the worker routes on the internal listener (specs.html § 2.1).
const (
	PathClaim     = "/internal/v1/jobs/claim"
	PathHeartbeat = "/internal/v1/jobs/{id}/heartbeat"
	PathResult    = "/internal/v1/jobs/{id}/result"
	PathRelease   = "/internal/v1/jobs/{id}/release"
)

// Outcomes a worker reports for an attempt.
const (
	OutcomeSucceeded = "succeeded"
	OutcomeFailed    = "failed"
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
}
