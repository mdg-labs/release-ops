-- Release Ops — application schema (app.db)
-- Auth (users + sessions) in the same file — Go-only writer; see specs.html § Authentication
-- Apply via golang-migrate from container entrypoint / Go server on startup

PRAGMA foreign_keys = ON;

CREATE TABLE users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

-- Server-side session store (e.g. alexedwards/scs SQLite store)
CREATE TABLE sessions (
  token TEXT PRIMARY KEY,
  data BLOB NOT NULL,
  expiry REAL NOT NULL,
  user_id TEXT REFERENCES users(id)
);

CREATE INDEX idx_sessions_expiry ON sessions (expiry);
CREATE INDEX idx_sessions_user_id ON sessions (user_id);

CREATE TABLE app_settings (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  poll_interval_minutes INTEGER NOT NULL DEFAULT 360 CHECK (poll_interval_minutes >= 5),
  invite_token_expiry_hours INTEGER NOT NULL DEFAULT 168 CHECK (
    invite_token_expiry_hours >= 1 AND invite_token_expiry_hours <= 720
  ),
  password_reset_token_expiry_minutes INTEGER NOT NULL DEFAULT 60 CHECK (
    password_reset_token_expiry_minutes >= 5 AND password_reset_token_expiry_minutes <= 1440
  ),
  updated_at TEXT NOT NULL
);

-- One-time tokens for invitations, password reset, and email change (raw token never stored)
CREATE TABLE auth_tokens (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK (
    kind IN ('invitation', 'password_reset', 'email_change')
  ),
  email TEXT NOT NULL,
  token_hash TEXT NOT NULL,
  new_email TEXT,
  invited_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
  expires_at TEXT NOT NULL,
  used_at TEXT,
  created_at TEXT NOT NULL
);

CREATE INDEX idx_auth_tokens_token_hash ON auth_tokens (token_hash);

-- kind: source → github, gitlab, gitea, forgejo, codeberg | ticket → phasical, jira, linear
CREATE TABLE integrations (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK (
    kind IN ('github', 'gitlab', 'gitea', 'forgejo', 'codeberg', 'phasical', 'jira', 'linear')
  ),
  name TEXT NOT NULL,
  base_url TEXT,
  encrypted_payload TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  CHECK (
    (kind = 'github' AND base_url IS NULL)
    OR (kind = 'gitlab' AND base_url IS NOT NULL)
    OR (kind = 'gitea' AND base_url IS NOT NULL)
    OR (kind = 'forgejo' AND base_url IS NOT NULL)
    OR (kind = 'codeberg' AND base_url IS NULL)
    OR (kind = 'phasical' AND base_url IS NOT NULL)
    OR (kind = 'jira' AND base_url IS NOT NULL)
    OR (kind = 'linear' AND base_url IS NULL)
  )
);

-- One row per target project/team under a ticket integration.
-- Status mapping and create defaults are per project (workflows differ).
CREATE TABLE ticket_projects (
  id TEXT PRIMARY KEY,
  integration_id TEXT NOT NULL REFERENCES integrations(id) ON DELETE RESTRICT,
  external_project_id TEXT NOT NULL,
  name TEXT NOT NULL,
  create_config TEXT NOT NULL,
  status_mapping TEXT NOT NULL,
  content_templates TEXT NOT NULL DEFAULT '{"title":"","description":"","supersedeComment":""}' CHECK (json_valid(content_templates)),
  on_open_ticket_policy TEXT NOT NULL DEFAULT 'supersede' CHECK (
    on_open_ticket_policy IN ('supersede', 'merge', 'skip_if_open')
  ),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (integration_id, external_project_id),
  CHECK (json_valid(create_config) AND json_valid(status_mapping))
);

CREATE TABLE monitored_repos (
  id TEXT PRIMARY KEY,
  source_kind TEXT NOT NULL CHECK (
    source_kind IN ('github', 'gitlab', 'gitea', 'forgejo', 'codeberg')
  ),
  project_path TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  source_integration_id TEXT REFERENCES integrations(id),
  ticket_project_id TEXT NOT NULL REFERENCES ticket_projects(id) ON DELETE RESTRICT,
  open_ticket_external_id TEXT,
  open_ticket_tag TEXT,
  last_known_tag TEXT,
  last_polled_at TEXT,
  last_error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_release_published_at TEXT,
  include_prereleases INTEGER NOT NULL DEFAULT 0 CHECK (include_prereleases IN (0, 1)),
  UNIQUE (source_kind, project_path)
);

-- Shoutrrr URL targets (slack://, ntfy://, generic://, …)
CREATE TABLE notification_targets (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  shoutrrr_url_encrypted TEXT NOT NULL,
  events_json TEXT NOT NULL DEFAULT '["create","error","supersede"]',
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  CHECK (json_valid(events_json))
);

-- Optional per-repo notification routing (empty = all global enabled targets)
CREATE TABLE monitored_repo_notifications (
  monitored_repo_id TEXT NOT NULL REFERENCES monitored_repos(id) ON DELETE CASCADE,
  notification_target_id TEXT NOT NULL REFERENCES notification_targets(id) ON DELETE CASCADE,
  PRIMARY KEY (monitored_repo_id, notification_target_id)
);

CREATE TABLE poll_runs (
  id TEXT PRIMARY KEY,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  status TEXT NOT NULL CHECK (status IN ('running', 'success', 'partial', 'failed')),
  repos_checked INTEGER NOT NULL DEFAULT 0,
  tickets_created INTEGER NOT NULL DEFAULT 0,
  tickets_superseded INTEGER NOT NULL DEFAULT 0,
  errors_json TEXT NOT NULL DEFAULT '[]',
  trigger_source TEXT NOT NULL DEFAULT 'scheduled' CHECK (trigger_source IN ('manual', 'scheduled'))
);

CREATE TABLE poll_run_events (
  id TEXT PRIMARY KEY,
  poll_run_id TEXT NOT NULL REFERENCES poll_runs(id) ON DELETE CASCADE,
  monitored_repo_id TEXT REFERENCES monitored_repos(id) ON DELETE SET NULL,
  action TEXT NOT NULL CHECK (
    action IN ('baseline', 'skip', 'create', 'supersede', 'merge', 'skip_open', 'error')
  ),
  detail TEXT,
  created_at TEXT NOT NULL
);

CREATE INDEX idx_integrations_kind ON integrations (kind);
CREATE INDEX idx_ticket_projects_integration ON ticket_projects (integration_id);
CREATE INDEX idx_monitored_repos_enabled ON monitored_repos (enabled);
CREATE INDEX idx_monitored_repos_source ON monitored_repos (source_kind);
CREATE INDEX idx_monitored_repos_ticket_project ON monitored_repos (ticket_project_id);
CREATE INDEX idx_notification_targets_enabled ON notification_targets (enabled);
CREATE INDEX idx_poll_run_events_run ON poll_run_events (poll_run_id);
CREATE INDEX idx_poll_runs_started ON poll_runs (started_at);
