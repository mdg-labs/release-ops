package github

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/mdg-labs/release-ops/internal/store"
)

// renovateConfigFiles are the file names Renovate reads its configuration from, by directory
// (the root is ""), as listed in configFileNames of Renovate 44.239.1, the version the worker
// pins. The "renovate" key of package.json is read too but deliberately left out: it is
// deprecated, and detecting it would mean fetching and parsing the file instead of listing
// directories. A repository configured only that way is recorded as having no config.
var renovateConfigFiles = map[string][]string{
	"": {
		"renovate.json", "renovate.jsonc", "renovate.json5",
		".renovaterc", ".renovaterc.json", ".renovaterc.jsonc", ".renovaterc.json5",
	},
	".github": {"renovate.json", "renovate.jsonc", "renovate.json5"},
	".gitlab": {"renovate.json", "renovate.jsonc", "renovate.json5"},
}

// Installations lists the installations of the App, with the App JWT.
func (c *AppClient) Installations(ctx context.Context) ([]Installation, error) {
	jwt, _, err := c.appJWT(ctx)
	if err != nil {
		return nil, err
	}
	return c.api.ListInstallations(ctx, jwt)
}

// Repositories lists every repository the installation gives the App access to.
func (c *AppClient) Repositories(ctx context.Context, installationID int64) ([]Repository, error) {
	tok, err := c.Token(ctx, installationID)
	if err != nil {
		return nil, err
	}
	return c.api.ListInstallationRepositories(ctx, tok.Value())
}

// HasRenovateConfig reports whether the repository (owner/name) has a Renovate config file at
// one of the locations Renovate reads. It lists the root, and the .github and .gitlab
// directories when the root has them, so a repository costs one to three calls.
func (c *AppClient) HasRenovateConfig(ctx context.Context, installationID int64, fullName string) (bool, error) {
	tok, err := c.Token(ctx, installationID)
	if err != nil {
		return false, err
	}
	root, err := c.api.ListDirectory(ctx, tok.Value(), fullName, "")
	if err != nil {
		return false, err
	}
	if hasFile(root, renovateConfigFiles[""]) {
		return true, nil
	}
	for _, dir := range []string{".github", ".gitlab"} {
		if !slices.ContainsFunc(root, func(e ContentEntry) bool { return e.Type == "dir" && e.Name == dir }) {
			continue
		}
		entries, err := c.api.ListDirectory(ctx, tok.Value(), fullName, dir)
		if err != nil {
			return false, err
		}
		if hasFile(entries, renovateConfigFiles[dir]) {
			return true, nil
		}
	}
	return false, nil
}

func hasFile(entries []ContentEntry, names []string) bool {
	return slices.ContainsFunc(entries, func(e ContentEntry) bool {
		return e.Type == "file" && slices.Contains(names, e.Name)
	})
}

// Syncer keeps the stored installation and its repositories in step with GitHub. Sync is the
// one function the startup, the periodic and the polling paths call; the webhook handlers
// apply an event's payload through the same store.InstallationSync.
type Syncer struct {
	app   *AppClient
	store store.InstallationSync

	mu      sync.Mutex
	trigger chan struct{}
}

// NewSyncer returns a Syncer that reads GitHub through app and writes through st.
func NewSyncer(app *AppClient, st store.InstallationSync) *Syncer {
	return &Syncer{app: app, store: st, trigger: make(chan struct{}, 1)}
}

// Trigger asks Run for a sync soon. It never blocks; triggers that arrive while one is
// pending are one.
func (s *Syncer) Trigger() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

// Run syncs at once and then on every tick and every Trigger, until ctx ends. report gets
// the result of every pass, nil when it succeeded; a failed pass does not stop the next.
func (s *Syncer) Run(ctx context.Context, ticks <-chan time.Time, report func(error)) {
	for {
		if err := s.Sync(ctx); err == nil || ctx.Err() == nil {
			report(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		case <-s.trigger:
		}
	}
}

// Sync reconciles the stored installations and repositories with what GitHub lists for the
// App. Before the App is stored it does nothing. When GitHub's list of installations cannot
// be read, nothing is changed and nothing is marked removed. A failure on one installation
// does not stop the others; the errors of all are returned together.
func (s *Syncer) Sync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	listed, err := s.app.Installations(ctx)
	if errors.Is(err, store.ErrGitHubAppNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list installations: %w", err)
	}

	seen := make(map[int64]bool, len(listed))
	for _, inst := range listed {
		seen[inst.ID] = true
	}

	live, err := s.store.LiveInstallationIDs(ctx)
	if err != nil {
		return fmt.Errorf("list recorded installations: %w", err)
	}
	// An installation GitHub no longer lists is removed first, so a reinstalled App's new
	// installation can take its place in the same pass.
	var errs []error
	for _, id := range live {
		if seen[id] {
			continue
		}
		err := s.store.Transaction(ctx, func(tx *sql.Tx) error { return s.store.RemoveInstallation(ctx, tx, id) })
		if err != nil {
			errs = append(errs, fmt.Errorf("installation %d: %w", id, err))
		}
	}
	for _, inst := range listed {
		if err := s.syncInstallation(ctx, inst); err != nil {
			errs = append(errs, fmt.Errorf("installation %d: %w", inst.ID, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Syncer) syncInstallation(ctx context.Context, inst Installation) error {
	status := "active"
	if inst.SuspendedAt != nil {
		status = "suspended"
	}
	permissions := "{}"
	if inst.Permissions != nil {
		raw, err := json.Marshal(inst.Permissions)
		if err != nil {
			return err
		}
		permissions = string(raw)
	}

	var outcome store.InstallationOutcome
	err := s.store.Transaction(ctx, func(tx *sql.Tx) error {
		var err error
		outcome, err = s.store.ApplyInstallation(ctx, tx, store.NewInstallation{
			GitHubInstallationID: inst.ID,
			AccountID:            inst.Account.ID,
			AccountLogin:         inst.Account.Login,
			AccountType:          inst.Account.Type,
			Status:               status,
			RepositorySelection:  inst.RepositorySelection,
			Permissions:          permissions,
		})
		return err
	})
	if err != nil {
		return err
	}
	if outcome == store.InstallationRefused {
		slog.Warn("github sync: another installation is already recorded; ignoring", "installation_id", inst.ID)
		return nil
	}
	if status != "active" {
		return nil
	}

	repos, err := s.app.Repositories(ctx, inst.ID)
	if err != nil {
		return fmt.Errorf("list repositories: %w", err)
	}
	states, err := s.store.RepositoryConfigStates(ctx, inst.ID)
	if err != nil {
		return err
	}

	synced := make([]store.SyncedRepository, 0, len(repos))
	unchecked := 0
	for _, repo := range repos {
		sr := store.SyncedRepository{GitHubID: repo.ID, FullName: repo.FullName, DefaultBranch: repo.DefaultBranch}
		// A recorded config is not read again: the job that runs on the repository records
		// a config that has gone, and re-reading every repository each pass would spend
		// the installation's rate limit on files that rarely change.
		if states[repo.ID] != store.ConfigPresent {
			present, err := s.app.HasRenovateConfig(ctx, inst.ID, repo.FullName)
			switch {
			case err == nil && present:
				sr.ConfigState = store.ConfigPresent
			case err == nil:
				sr.ConfigState = store.ConfigMissing
			case ctx.Err() != nil:
				return ctx.Err()
			default:
				unchecked++
			}
		}
		synced = append(synced, sr)
	}

	err = s.store.Transaction(ctx, func(tx *sql.Tx) error {
		return s.store.ReplaceRepositories(ctx, tx, inst.ID, synced)
	})
	if err != nil {
		return err
	}
	if unchecked > 0 {
		return fmt.Errorf("the Renovate config of %d repositories could not be read and keeps its recorded state", unchecked)
	}
	return nil
}
