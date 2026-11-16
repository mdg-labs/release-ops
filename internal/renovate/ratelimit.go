package renovate

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mdg-labs/release-ops/internal/queue"
	"github.com/mdg-labs/release-ops/internal/ratelimit"
)

// maxLogDepth bounds how deep a log record is searched for a response.
const maxLogDepth = 6

// rateLimitFromLog reads the last rate-limit state GitHub's API gave Renovate during a run, from
// Renovate's JSON log (one object per line). Renovate's own calls are not intercepted, so what
// it logs of a failed request is the only record: an error record carries the response that
// caused it as an object with a numeric statusCode and a headers object, and the url of the
// request beside it under options (the shape of a serialized HTTP error) or on the object itself.
// Only a response from apiHost counts, so the headers of a package registry or any other host
// Renovate talks to never reach the installation's budget. nil when the log holds none.
func rateLimitFromLog(log, apiHost string, now time.Time) *queue.RateLimitReport {
	var last *ratelimit.Signal
	for _, line := range strings.Split(log, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		for _, resp := range responsesIn(record, "", 0) {
			if !strings.EqualFold(hostOf(resp.url), apiHost) {
				continue
			}
			if sig := ratelimit.Parse(resp.status, resp.headers, now); !sig.Empty() {
				last = &sig
			}
		}
	}
	if last == nil {
		return nil
	}
	rep := &queue.RateLimitReport{Limited: last.Limited}
	if last.HasRemaining {
		rep.Remaining = &last.Remaining
	}
	if last.HasLimit {
		rep.Limit = &last.Limit
	}
	if !last.ResetAt.IsZero() {
		rep.ResetUnix = last.ResetAt.Unix()
	}
	rep.RetryAfterSeconds = int64((last.RetryAfter + time.Second - 1) / time.Second)
	return rep
}

// retryAfterOf is how long after now a report asks the attempt to wait before a retry.
func retryAfterOf(rep *queue.RateLimitReport, now time.Time) time.Duration {
	if rep == nil || !rep.Limited {
		return 0
	}
	sig := ratelimit.Signal{Limited: true, RetryAfter: time.Duration(rep.RetryAfterSeconds) * time.Second}
	if rep.Remaining != nil {
		sig.Remaining, sig.HasRemaining = *rep.Remaining, true
	}
	if rep.ResetUnix > 0 {
		sig.ResetAt = time.Unix(rep.ResetUnix, 0).UTC()
	}
	return sig.Until(now).Sub(now)
}

type loggedResponse struct {
	status  int
	headers http.Header
	url     string
}

// responsesIn finds the responses inside a log record: every object with a numeric statusCode and
// a headers object. Its url is the object's own, the options.url beside it in the parent (the
// request a serialized HTTP error carries next to its response), or that of the nearest object
// above it.
func responsesIn(v any, inheritedURL string, depth int) []loggedResponse {
	if depth > maxLogDepth {
		return nil
	}
	var out []loggedResponse
	switch v := v.(type) {
	case map[string]any:
		u := inheritedURL
		for _, key := range []string{"url", "requestUrl"} {
			if s, ok := v[key].(string); ok && s != "" {
				u = s
				break
			}
		}
		status, hasStatus := v["statusCode"].(float64)
		if headers, ok := v["headers"].(map[string]any); ok && hasStatus {
			out = append(out, loggedResponse{status: int(status), headers: toHeader(headers), url: u})
		}
		requestURL := ""
		if options, ok := v["options"].(map[string]any); ok {
			requestURL, _ = options["url"].(string)
		}
		for key, child := range v {
			childURL := u
			if key == "response" && requestURL != "" {
				childURL = requestURL
			}
			out = append(out, responsesIn(child, childURL, depth+1)...)
		}
	case []any:
		for _, child := range v {
			out = append(out, responsesIn(child, inheritedURL, depth+1)...)
		}
	}
	return out
}

func toHeader(m map[string]any) http.Header {
	h := http.Header{}
	for name, value := range m {
		switch value := value.(type) {
		case string:
			h.Add(name, value)
		case float64:
			h.Add(name, strconv.FormatFloat(value, 'f', -1, 64))
		case []any:
			for _, item := range value {
				if s, ok := item.(string); ok {
					h.Add(name, s)
				}
			}
		}
	}
	return h
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

// rateLimitedFailure is the failed attempt of a run GitHub's rate limit stopped.
func rateLimitedFailure(rep *queue.RateLimitReport, retryAfter time.Duration) *failure {
	f := fail(queue.ErrorCodeRateLimited, fmt.Sprintf("GitHub's rate limit stopped the run; it is retried in %s", retryAfter.Round(time.Second)))
	f.retryAfter, f.rateLimit = retryAfter, rep
	return f
}
