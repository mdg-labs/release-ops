package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mdg-labs/release-ops/internal/api/handlers"
	"github.com/mdg-labs/release-ops/internal/store"
)

type panicJobs struct{ t *testing.T }

func (p panicJobs) List(context.Context, store.JobListFilter) ([]store.JobListItem, int, error) {
	p.t.Error("the handler listed jobs without a principal")
	return nil, 0, nil
}

func (p panicJobs) Get(context.Context, string, bool, []string) (store.JobDetail, error) {
	p.t.Error("the handler read a job without a principal")
	return store.JobDetail{}, nil
}

type panicProblems struct{ t *testing.T }

func (p panicProblems) Read(context.Context, store.ProblemFilter) (store.ProblemSet, error) {
	p.t.Error("the handler read problems without a principal")
	return store.ProblemSet{}, nil
}

// The routes sit behind Authenticate, which stores the caller's role and scope; a handler
// refuses a request with no principal on its own, so a route moved out of that group reads
// nothing.
func TestTheJobAndProblemHandlersNeedAPrincipal(t *testing.T) {
	t.Parallel()

	jobs := &handlers.JobHandlers{Jobs: panicJobs{t}}
	problems := &handlers.ProblemHandlers{Problems: panicProblems{t}}
	for name, serve := range map[string]http.HandlerFunc{"list jobs": jobs.List, "get job": jobs.Get, "problems": problems.List} {
		rec := httptest.NewRecorder()
		serve(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a principal = %d %s, want 401", name, rec.Code, rec.Body.String())
		}
	}
}
