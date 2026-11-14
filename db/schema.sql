-- Release Ops — application schema (app.db), v2
-- Auth (users + sessions + auth_identities) in the same file — Go-only writer; see specs.html § Authentication
-- Data is scoped by installation (specs.html § 4.4): repositories hang off installations, and jobs,
-- update PRs and their tickets hang off repositories.
-- Migrations in migrations/ are generated from this file by sqlite-migrate and applied by the Go server on startup

PRAGMA foreign_keys = ON;

-- email and password_hash are NULL for a user who signs in with GitHub only (auth_identities);
-- they go with the local accounts.
CREATE TABLE users (
  id TEXT PRIMARY KEY,
  email TEXT UNIQUE,
  password_hash TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
) STRICT;

-- Server-side session store (e.g. alexedwards/scs SQLite store)
CREATE TABLE sessions (
  token TEXT PRIMARY KEY,
  data BLOB NOT NULL,
  expiry REAL NOT NULL,
  user_id TEXT REFERENCES users(id)
) STRICT;

CREATE INDEX idx_sessions_expiry ON sessions (expiry);
CREATE INDEX idx_sessions_user_id ON sessions (user_id);

-- Legacy local-account settings (invitation and password-reset windows); removed with local accounts.
CREATE TABLE app_settings (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  invite_token_expiry_hours INTEGER NOT NULL DEFAULT 168 CHECK (
    invite_token_expiry_hours >= 1 AND invite_token_expiry_hours <= 720
  ),
  password_reset_token_expiry_minutes INTEGER NOT NULL DEFAULT 60 CHECK (
    password_reset_token_expiry_minutes >= 5 AND password_reset_token_expiry_minutes <= 1440
  ),
  updated_at TEXT NOT NULL
) STRICT;

-- Settings of the whole instance (singleton).
CREATE TABLE instance_settings (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  concurrency_cap INTEGER NOT NULL DEFAULT 2 CHECK (concurrency_cap >= 1),
  bootstrap_admin_user_id TEXT REFERENCES users(id),
  updated_at TEXT NOT NULL
) STRICT;

-- Sign-in identity of a user: the numeric GitHub user id in v2.0, never the login.
CREATE TABLE auth_identities (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider TEXT NOT NULL CHECK (provider IN ('github')),
  subject TEXT NOT NULL,
  login TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE (provider, subject)
) STRICT;

CREATE INDEX idx_auth_identities_user_id ON auth_identities (user_id);

-- Break-glass login links (specs.html § 3.6): the SHA-256 of the token, never the token. A link
-- is redeemed once, by a guarded write that matches only an unconsumed, unexpired row.
CREATE TABLE login_link_tokens (
  id TEXT PRIMARY KEY,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TEXT NOT NULL,
  consumed_at TEXT,
  created_at TEXT NOT NULL
) STRICT;

-- One-time tokens for invitations, password reset, email change and the first-start setup
-- token (raw token never stored)
CREATE TABLE auth_tokens (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK (
    kind IN ('invitation', 'password_reset', 'email_change', 'setup')
  ),
  email TEXT NOT NULL,
  token_hash TEXT NOT NULL,
  new_email TEXT,
  invited_by_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
  expires_at TEXT NOT NULL,
  used_at TEXT,
  created_at TEXT NOT NULL
) STRICT;

CREATE INDEX idx_auth_tokens_token_hash ON auth_tokens (token_hash);

-- kind: ticket systems only — kaneo, jira, linear. GitHub access is the App (github_app).
CREATE TABLE integrations (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK (kind IN ('kaneo', 'jira', 'linear')),
  name TEXT NOT NULL,
  base_url TEXT,
  encrypted_payload TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  CHECK (
    (kind = 'kaneo' AND base_url IS NOT NULL)
    OR (kind = 'jira' AND base_url IS NOT NULL)
    OR (kind = 'linear' AND base_url IS NULL)
  )
) STRICT;

-- One row per target project/team under a ticket integration.
-- Status mapping, create defaults and the ticket filter are per project (workflows differ).
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
  filter TEXT NOT NULL DEFAULT '{"updateTypes":[],"labels":[],"skipAutomerged":true}' CHECK (json_valid(filter)),
  UNIQUE (integration_id, external_project_id),
  CHECK (json_valid(create_config) AND json_valid(status_mapping))
) STRICT;

-- The GitHub App (singleton). The three secrets are encrypted payloads (AES-256-GCM, specs.html § 4.8)
-- with no plaintext twin; key_id names the key they were encrypted with.
CREATE TABLE github_app (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  app_id INTEGER NOT NULL,
  slug TEXT NOT NULL,
  client_id TEXT NOT NULL,
  html_url TEXT NOT NULL,
  owner_login TEXT NOT NULL,
  owner_type TEXT NOT NULL CHECK (owner_type IN ('User', 'Organization')),
  bot_user_id INTEGER,
  webhook_active INTEGER NOT NULL CHECK (webhook_active IN (0, 1)),
  encrypted_private_key TEXT NOT NULL,
  encrypted_client_secret TEXT NOT NULL,
  encrypted_webhook_secret TEXT NOT NULL,
  key_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE installations (
  id TEXT PRIMARY KEY,
  github_installation_id INTEGER NOT NULL UNIQUE,
  account_id INTEGER NOT NULL,
  account_login TEXT NOT NULL,
  account_type TEXT NOT NULL CHECK (account_type IN ('User', 'Organization')),
  status TEXT NOT NULL CHECK (status IN ('active', 'suspended', 'removed')),
  repository_selection TEXT NOT NULL CHECK (repository_selection IN ('all', 'selected')),
  permissions TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(permissions)),
  rate_limit_remaining INTEGER,
  rate_limit_limit INTEGER,
  rate_limit_reset_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
) STRICT;

-- At most one installation that is not removed (single-tenant, specs.html § 3.4).
CREATE UNIQUE INDEX idx_installations_one_not_removed ON installations ((1)) WHERE status <> 'removed';

-- Settings of one installation.
CREATE TABLE installation_settings (
  installation_id TEXT PRIMARY KEY REFERENCES installations(id) ON DELETE CASCADE,
  default_ticket_project_id TEXT REFERENCES ticket_projects(id) ON DELETE SET NULL,
  global_config_content TEXT,
  global_config_source TEXT CHECK (global_config_source IN ('repository', 'mounted')),
  global_config_captured_at TEXT,
  updated_at TEXT NOT NULL
) STRICT;

-- GitHub's delivery GUIDs the webhook endpoint has accepted (specs.html § 2.10): a GUID seen again does
-- nothing. A row is written in the transaction of the work the delivery caused, and rows are pruned after
-- the retention of internal/store/webhook_deliveries.go. installation_id is NULL while the delivery's
-- installation is not recorded.
CREATE TABLE webhook_deliveries (
  guid TEXT PRIMARY KEY,
  event TEXT NOT NULL,
  installation_id TEXT REFERENCES installations(id) ON DELETE SET NULL,
  received_at TEXT NOT NULL
) STRICT;

CREATE INDEX idx_webhook_deliveries_received_at ON webhook_deliveries (received_at);

-- The installed repositories. A repository that leaves the installation keeps its row and history
-- (removed_at set).
CREATE TABLE repositories (
  id TEXT PRIMARY KEY,
  installation_id TEXT NOT NULL REFERENCES installations(id) ON DELETE RESTRICT,
  github_repository_id INTEGER NOT NULL UNIQUE,
  full_name TEXT NOT NULL,
  default_branch TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  ticket_project_id TEXT REFERENCES ticket_projects(id) ON DELETE SET NULL,
  config_state TEXT NOT NULL DEFAULT 'unknown' CHECK (config_state IN ('unknown', 'present', 'missing')),
  dashboard_issue_number INTEGER,
  removed_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (installation_id, full_name)
) STRICT;

-- The repository-access list of a viewer's session (specs.html § 3.9): the repositories the user
-- can access, written at sign-in and never refreshed. Rows go with the session and with a deleted
-- repository; an admin session has none, and a viewer session without rows sees no repository.
CREATE TABLE session_repositories (
  session_token TEXT NOT NULL REFERENCES sessions(token) ON DELETE CASCADE,
  repository_id TEXT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
  PRIMARY KEY (session_token, repository_id)
) STRICT;

CREATE INDEX idx_session_repositories_repository ON session_repositories (repository_id);

-- One row per bot update PR of a repository.
CREATE TABLE update_prs (
  id TEXT PRIMARY KEY,
  repository_id TEXT NOT NULL REFERENCES repositories(id) ON DELETE RESTRICT,
  number INTEGER NOT NULL,
  node_id TEXT NOT NULL,
  url TEXT NOT NULL,
  title TEXT NOT NULL,
  head_ref TEXT NOT NULL,
  head_sha TEXT,
  state TEXT NOT NULL CHECK (state IN ('open', 'merged', 'closed', 'superseded')),
  superseded_by_id TEXT REFERENCES update_prs(id) ON DELETE SET NULL,
  labels TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(labels)),
  automerge INTEGER CHECK (automerge IN (0, 1)),
  release_notes TEXT,
  report_sha TEXT,
  opened_at TEXT,
  merged_at TEXT,
  closed_at TEXT,
  last_seen_at TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (repository_id, number)
) STRICT;

-- The dependencies an update PR changes; replaced as a set.
CREATE TABLE update_pr_dependencies (
  id TEXT PRIMARY KEY,
  update_pr_id TEXT NOT NULL REFERENCES update_prs(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  manager TEXT,
  datasource TEXT,
  from_version TEXT,
  to_version TEXT,
  update_type TEXT CHECK (
    update_type IN ('major', 'minor', 'patch', 'digest', 'lockfile', 'other')
  ),
  source TEXT NOT NULL CHECK (source IN ('report', 'body'))
) STRICT;

-- At most one ticket per update PR. The row exists before the ticket does (pending_since).
CREATE TABLE update_tickets (
  id TEXT PRIMARY KEY,
  update_pr_id TEXT NOT NULL UNIQUE REFERENCES update_prs(id) ON DELETE RESTRICT,
  ticket_project_id TEXT NOT NULL REFERENCES ticket_projects(id) ON DELETE RESTRICT,
  external_id TEXT,
  external_url TEXT,
  state TEXT NOT NULL CHECK (state IN ('pending', 'open', 'done', 'cancelled', 'superseded')),
  pending_since TEXT,
  synced_hash TEXT,
  pr_comment_id INTEGER,
  pr_comment_hash TEXT,
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt_at TEXT,
  last_error TEXT,
  last_error_at TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
) STRICT;

-- The persistent job queue (specs.html § 2.8): at most one queued and one running job per repository.
CREATE TABLE jobs (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK (kind IN ('renovate_run')),
  repository_id TEXT NOT NULL REFERENCES repositories(id) ON DELETE RESTRICT,
  priority TEXT NOT NULL CHECK (priority IN ('high', 'normal')),
  state TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'running', 'succeeded', 'failed')),
  run_after TEXT NOT NULL,
  attempts INTEGER NOT NULL DEFAULT 0,
  lease_owner TEXT,
  lease_expires_at TEXT,
  last_heartbeat_at TEXT,
  rerun_requested INTEGER NOT NULL DEFAULT 0 CHECK (rerun_requested IN (0, 1)),
  "trigger" TEXT NOT NULL CHECK (
    "trigger" IN (
      'dashboard_checkbox', 'pr_checkbox', 'pr_closed', 'push',
      'repository_added', 'schedule', 'polling', 'run_now'
    )
  ),
  matched_markers TEXT CHECK (matched_markers IS NULL OR json_valid(matched_markers)),
  webhook_received_at TEXT,
  started_at TEXT,
  created_at TEXT NOT NULL,
  finished_at TEXT
) STRICT;

CREATE UNIQUE INDEX idx_jobs_one_queued_per_repository ON jobs (repository_id) WHERE state = 'queued';
CREATE UNIQUE INDEX idx_jobs_one_running_per_repository ON jobs (repository_id) WHERE state = 'running';

-- One row per attempt of a job.
CREATE TABLE job_runs (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  attempt INTEGER NOT NULL,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  outcome TEXT NOT NULL CHECK (
    outcome IN ('running', 'succeeded', 'no_config', 'failed', 'aborted', 'lease_expired')
  ),
  exit_code INTEGER,
  error_code TEXT,
  error_message TEXT,
  retry_after TEXT,
  report_status TEXT CHECK (report_status IN ('ok', 'missing', 'unreadable')),
  log_excerpt TEXT,
  UNIQUE (job_id, attempt)
) STRICT;

-- What happened to a job, in order. Values are redacted before they are written.
CREATE TABLE job_events (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  job_run_id TEXT REFERENCES job_runs(id) ON DELETE CASCADE,
  at TEXT NOT NULL,
  kind TEXT NOT NULL,
  message TEXT
) STRICT;

CREATE INDEX idx_integrations_kind ON integrations (kind);
CREATE INDEX idx_ticket_projects_integration ON ticket_projects (integration_id);
CREATE INDEX idx_installation_settings_ticket_project ON installation_settings (default_ticket_project_id);
CREATE INDEX idx_repositories_ticket_project ON repositories (ticket_project_id);
CREATE INDEX idx_update_prs_state ON update_prs (repository_id, state);
CREATE INDEX idx_update_prs_superseded_by ON update_prs (superseded_by_id);
CREATE INDEX idx_update_pr_dependencies_pr ON update_pr_dependencies (update_pr_id);
CREATE INDEX idx_update_tickets_project ON update_tickets (ticket_project_id);
CREATE INDEX idx_update_tickets_state ON update_tickets (state);
CREATE INDEX idx_jobs_claim ON jobs (state, priority, run_after);
CREATE INDEX idx_jobs_finished ON jobs (finished_at) WHERE finished_at IS NOT NULL;
CREATE INDEX idx_job_runs_job ON job_runs (job_id);
CREATE INDEX idx_job_events_job ON job_events (job_id, at);
CREATE INDEX idx_job_events_run ON job_events (job_run_id);
