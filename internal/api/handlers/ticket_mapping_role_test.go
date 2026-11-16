package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdg-labs/release-ops/internal/api/handlers"
	"github.com/mdg-labs/release-ops/internal/store"
)

// panicInstallations and panicRepositories fail the test when a handler reaches the store.
type panicInstallations struct{ t *testing.T }

func (p panicInstallations) List(context.Context) ([]store.InstallationSummary, error) {
	p.t.Error("the handler read installations without an admin principal")
	return nil, nil
}

func (p panicInstallations) SetDefaultTicketProject(context.Context, string, string) error {
	p.t.Error("the handler wrote the default ticket project without an admin principal")
	return nil
}

type panicRepositories struct {
	store.RepositoryRepository
	t *testing.T
}

func (p panicRepositories) List(context.Context, store.RepositoryFilter) ([]store.RepositoryListing, int, error) {
	p.t.Error("the handler listed repositories without an admin principal")
	return nil, 0, nil
}

// The routes sit behind RequireAdmin; the handlers that read and set ticket project mappings
// refuse a caller with no admin principal on their own, so a route moved out of the admin group
// does not expose them.
func TestTicketProjectMappingHandlersEnforceTheAdminRoleThemselves(t *testing.T) {
	t.Parallel()

	inst := &handlers.InstallationHandlers{Installations: panicInstallations{t}}
	repos := &handlers.RepositoryHandlers{Repositories: panicRepositories{t: t}}
	for name, call := range map[string]func(http.ResponseWriter, *http.Request){
		"GET /installations":        inst.List,
		"PATCH /installations/{id}": inst.Patch,
		"GET /repositories":         repos.List,
		"PATCH /repositories/{id}":  repos.Patch,
	} {
		rec := httptest.NewRecorder()
		call(rec, httptest.NewRequest(http.MethodPatch, "/x", strings.NewReader(`{"defaultTicketProjectId":null,"ticketProjectId":null}`)))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without a principal = %d %s, want 401", name, rec.Code, rec.Body.String())
		}
	}
}
