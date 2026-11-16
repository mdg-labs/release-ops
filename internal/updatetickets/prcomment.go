package updatetickets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"strings"
	"time"

	"github.com/mdg-labs/release-ops/internal/github"
	"github.com/mdg-labs/release-ops/internal/ratelimit"
	"github.com/mdg-labs/release-ops/internal/store"
)

// CommentMarker is the hidden text every PR comment of Release Ops starts with; the upsert finds
// its own comment again by it (specs.html section 5.2.3).
const CommentMarker = "<!-- release-ops:ticket -->"

// PRComments writes the ticket link comment of an update PR: it returns the id of the one comment
// of the bot that carries the marker and the body, creating it only when none exists.
type PRComments interface {
	Upsert(ctx context.Context, in github.CommentUpsert) (commentID int64, err error)
}

// commentFailure is what the engine remembers in memory about a PR comment that could not be
// written: the earliest time the next pass may try again, and how many writes failed in a row.
type commentFailure struct {
	notBefore time.Time
	count     int64
}

// commentBody is the comment of a ticket: the marker, the ticket's key and link, and its state.
// The text is fixed; it holds nothing but what the ticket row stores.
func commentBody(t *store.UpdateTicket) string {
	key := commentKey(t.ExternalID)
	var b strings.Builder
	b.WriteString(CommentMarker)
	b.WriteString("\nTracked in ")
	if link := commentLink(t.ExternalURL); link != "" {
		fmt.Fprintf(&b, "[%s ↗](%s)", key, link)
	} else {
		b.WriteString(key)
	}
	b.WriteString("\n\nRelease Ops ticket state: ")
	b.WriteString(stateText(t.State))
	b.WriteString("\n")
	return b.String()
}

func stateText(state string) string {
	switch state {
	case store.TicketStateDone:
		return "done"
	case store.TicketStateCancelled:
		return "won't do"
	case store.TicketStateSuperseded:
		return "superseded"
	}
	return "open"
}

// commentKey is the ticket's external id as text that cannot break out of a Markdown link or
// close the marker's HTML comment.
func commentKey(id string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r < ' ', r == '[', r == ']', r == '<', r == '>', r == '`', r == '\\':
			return -1
		}
		return r
	}, strings.TrimSpace(id))
}

// commentLink is the ticket's web URL as the target of a Markdown link; empty for anything that
// is not an http or https URL, so the comment never links to another scheme.
func commentLink(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return strings.NewReplacer("(", "%28", ")", "%29", " ", "%20", "<", "%3C", ">", "%3E").Replace(u.String())
}

func commentHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// comment brings the bot comment on the PR of the ticket to the ticket's current state. It
// never fails the ticket: a failed write is recorded on the ticket row for the Problems view and
// tried again by a later pass, once the pause has passed. force skips the pause, for a pass that
// has just changed the ticket. A comment whose last written body is the current one makes no call.
func (e *Engine) comment(ctx context.Context, updatePRID string, force bool) {
	if e.Comments == nil {
		return
	}
	e.cmu.Lock()
	defer e.cmu.Unlock()

	c, err := e.Tickets.Candidate(ctx, updatePRID)
	switch {
	case errors.Is(err, store.ErrUpdatePRNotFound):
		return
	case err != nil:
		if ctx.Err() == nil {
			e.logger().Error("update tickets: reading the PR for its comment failed", "update_pr", updatePRID, "error", err)
		}
		return
	}
	t := c.Ticket
	if t == nil || !t.Created {
		return
	}
	body := commentBody(t)
	hash := commentHash(body)
	if t.PRCommentID != 0 && t.PRCommentHash == hash {
		return
	}
	if !force && e.now().Before(e.commentFailures[t.ID].notBefore) {
		return
	}

	id, err := e.Comments.Upsert(ctx, github.CommentUpsert{
		InstallationID: c.GitHubInstallationID, Repository: c.FullName, Number: c.Number,
		Marker: CommentMarker, CommentID: t.PRCommentID, Body: body,
	})
	if err == nil {
		if err = e.Tickets.StorePRComment(ctx, t.ID, id, t.State, hash, e.now()); err != nil {
			err = fmt.Errorf("the comment was written but its id was not stored: %w", err)
		}
	}
	if err != nil {
		e.failComment(ctx, t.ID, updatePRID, err)
		return
	}
	delete(e.commentFailures, t.ID)
}

// failComment records a failed comment write on the ticket row and sets the pause before the
// next pass tries it again: the backoff, or the delay a rate limit asked for.
func (e *Engine) failComment(ctx context.Context, rowID, updatePRID string, cause error) {
	if ctx.Err() != nil {
		return
	}
	if e.commentFailures == nil {
		e.commentFailures = map[string]commentFailure{}
	}
	f := e.commentFailures[rowID]
	delay := backoff(f.count)
	if asked, limited := ratelimit.DelayOf(cause); limited && errors.Is(cause, ratelimit.ErrRateLimited) {
		rnd := rand.Float64
		if e.Rand != nil {
			rnd = e.Rand
		}
		delay = max(delay, ratelimit.Jittered(asked, rateLimitJitter, rnd()))
	}
	now := e.now()
	e.commentFailures[rowID] = commentFailure{notBefore: now.Add(delay), count: f.count + 1}

	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), failureWriteTimeout)
	defer cancel()
	if err := e.Tickets.NotePRCommentError(writeCtx, rowID, redact(cause), now); err != nil {
		e.logger().Error("update tickets: recording the PR comment failure failed", "update_pr", updatePRID, "error", err)
	}
	if errors.Is(cause, ratelimit.ErrRateLimited) {
		e.logger().Warn("update tickets: the PR comment is deferred until the GitHub rate limit resets", "update_pr", updatePRID, "error", cause)
		return
	}
	e.logger().Error("update tickets: writing the PR comment failed", "update_pr", updatePRID, "error", cause)
}

// commentSweep writes the comments that are due.
func (e *Engine) commentSweep(ctx context.Context) {
	if e.Comments == nil {
		return
	}
	ids, err := e.Tickets.CommentablePRs(ctx)
	if err != nil {
		if ctx.Err() == nil {
			e.logger().Error("update tickets: listing the PR comments that are due failed", "error", err)
		}
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		e.comment(ctx, id, false)
	}
}
