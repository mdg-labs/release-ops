// Package hooks serves the public GitHub webhook endpoint (specs.html § 2.10).
//
// A request is authenticated by the HMAC of its raw body, not by a session: the body is
// capped, verified against the stored webhook secret and only then parsed, deduplicated by
// its X-GitHub-Delivery GUID and routed by its X-GitHub-Event to the handler registered
// for the event. The deduplication record and the work of the handler share one
// transaction, and the answer is 202 once it commits. A request whose signature fails is
// counted per client IP and answered 429 once the IP has failed too often; valid deliveries
// are never counted.
package hooks
