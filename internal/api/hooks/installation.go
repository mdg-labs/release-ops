package hooks

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mdg-labs/release-ops/internal/store"
)

// RegisterInstallationHandlers replaces the acknowledging handlers of the installation and
// installation_repositories events with ones that apply the payload through sync, the
// functions the startup and periodic sync use. changed is called after a delivery adds
// repositories, so the sync can fill in what a payload does not carry (the default branch
// and the Renovate config); it may be nil. No handler calls GitHub.
func RegisterInstallationHandlers(r *Routes, sync store.InstallationSync, changed func()) {
	h := installationHandlers{sync: sync, changed: changed}
	r.Register("installation", h.installation)
	r.Register("installation_repositories", h.installationRepositories)
}

type installationHandlers struct {
	sync    store.InstallationSync
	changed func()
}

type eventRepository struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
}

type eventInstallation struct {
	ID      int64 `json:"id"`
	Account struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"account"`
	RepositorySelection string            `json:"repository_selection"`
	Permissions         map[string]string `json:"permissions"`
	SuspendedAt         *time.Time        `json:"suspended_at"`
}

func (i eventInstallation) valid() bool {
	return i.ID > 0 && i.Account.ID > 0 && i.Account.Login != "" &&
		(i.Account.Type == "User" || i.Account.Type == "Organization") &&
		(i.RepositorySelection == "all" || i.RepositorySelection == "selected")
}

func toSynced(repos []eventRepository) ([]store.SyncedRepository, error) {
	out := make([]store.SyncedRepository, 0, len(repos))
	for _, r := range repos {
		owner, name, ok := strings.Cut(r.FullName, "/")
		if r.ID <= 0 || !ok || owner == "" || name == "" || strings.Contains(name, "/") {
			return nil, errors.New("payload holds something that is not a repository")
		}
		out = append(out, store.SyncedRepository{GitHubID: r.ID, FullName: r.FullName})
	}
	return out, nil
}

func (h installationHandlers) installation(ctx context.Context, tx *sql.Tx, d Delivery) error {
	var p struct {
		Action       string            `json:"action"`
		Installation eventInstallation `json:"installation"`
		Repositories []eventRepository `json:"repositories"`
	}
	if err := json.Unmarshal(d.Payload, &p); err != nil {
		return fmt.Errorf("installation event: unreadable payload: %w", err)
	}
	if p.Installation.ID <= 0 {
		return errors.New("installation event: payload names no installation")
	}

	if p.Action == "deleted" {
		return h.sync.RemoveInstallation(ctx, tx, p.Installation.ID)
	}
	if !p.Installation.valid() {
		return errors.New("installation event: payload holds no usable installation")
	}

	status := "active"
	if p.Action == "suspend" || (p.Action != "unsuspend" && p.Installation.SuspendedAt != nil) {
		status = "suspended"
	}
	permissions := "{}"
	if p.Installation.Permissions != nil {
		raw, err := json.Marshal(p.Installation.Permissions)
		if err != nil {
			return fmt.Errorf("installation event: %w", err)
		}
		permissions = string(raw)
	}
	outcome, err := h.sync.ApplyInstallation(ctx, tx, store.NewInstallation{
		GitHubInstallationID: p.Installation.ID,
		AccountID:            p.Installation.Account.ID,
		AccountLogin:         p.Installation.Account.Login,
		AccountType:          p.Installation.Account.Type,
		Status:               status,
		RepositorySelection:  p.Installation.RepositorySelection,
		Permissions:          permissions,
	})
	if err != nil {
		return err
	}
	if outcome == store.InstallationRefused {
		slog.Warn("webhook: another installation is already recorded; ignoring", "installation_id", p.Installation.ID)
		return nil
	}

	if len(p.Repositories) > 0 {
		repos, err := toSynced(p.Repositories)
		if err != nil {
			return fmt.Errorf("installation event: %w", err)
		}
		if err := h.sync.UpsertRepositories(ctx, tx, p.Installation.ID, repos); err != nil {
			return err
		}
	}
	h.notify(outcome == store.InstallationRecorded || len(p.Repositories) > 0)
	return nil
}

func (h installationHandlers) installationRepositories(ctx context.Context, tx *sql.Tx, d Delivery) error {
	var p struct {
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
		Added   []eventRepository `json:"repositories_added"`
		Removed []eventRepository `json:"repositories_removed"`
	}
	if err := json.Unmarshal(d.Payload, &p); err != nil {
		return fmt.Errorf("installation_repositories event: unreadable payload: %w", err)
	}
	if p.Installation.ID <= 0 {
		return errors.New("installation_repositories event: payload names no installation")
	}
	added, err := toSynced(p.Added)
	if err != nil {
		return fmt.Errorf("installation_repositories event: %w", err)
	}
	removed, err := toSynced(p.Removed)
	if err != nil {
		return fmt.Errorf("installation_repositories event: %w", err)
	}

	err = h.apply(ctx, tx, p.Installation.ID, added, removed)
	if errors.Is(err, store.ErrInstallationNotLive) {
		slog.Warn("webhook: repositories of an installation that is not recorded; ignoring", "installation_id", p.Installation.ID)
		return nil
	}
	if err != nil {
		return err
	}
	h.notify(len(added) > 0)
	return nil
}

func (h installationHandlers) apply(ctx context.Context, tx *sql.Tx, installationID int64, added, removed []store.SyncedRepository) error {
	if len(added) > 0 {
		if err := h.sync.UpsertRepositories(ctx, tx, installationID, added); err != nil {
			return err
		}
	}
	if len(removed) > 0 {
		ids := make([]int64, len(removed))
		for i, r := range removed {
			ids[i] = r.GitHubID
		}
		return h.sync.RemoveRepositories(ctx, tx, installationID, ids)
	}
	return nil
}

func (h installationHandlers) notify(changed bool) {
	if changed && h.changed != nil {
		h.changed()
	}
}
