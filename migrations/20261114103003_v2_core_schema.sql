-- sqlite-migrate: checksum c5daa140123a5e463bf4a85654d19d3ede9527f80b2690ea3c052982cd8694a6

ALTER TABLE ticket_projects ADD COLUMN filter text NOT NULL DEFAULT '{"updateTypes":[],"labels":[],"skipAutomerged":true}' CHECK (json_valid(filter));

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

CREATE TABLE installation_settings (
  installation_id TEXT PRIMARY KEY REFERENCES installations(id) ON DELETE CASCADE,
  default_ticket_project_id TEXT REFERENCES ticket_projects(id) ON DELETE SET NULL,
  global_config_content TEXT,
  global_config_source TEXT CHECK (global_config_source IN ('repository', 'mounted')),
  global_config_captured_at TEXT,
  updated_at TEXT NOT NULL
) STRICT;

CREATE INDEX idx_installation_settings_ticket_project ON installation_settings (default_ticket_project_id);

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

CREATE UNIQUE INDEX idx_installations_one_not_removed ON installations ((1)) WHERE status <> 'removed';

CREATE TABLE instance_settings (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  concurrency_cap INTEGER NOT NULL DEFAULT 2 CHECK (concurrency_cap >= 1),
  bootstrap_admin_user_id TEXT REFERENCES users(id),
  updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE job_events (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  job_run_id TEXT REFERENCES job_runs(id) ON DELETE CASCADE,
  at TEXT NOT NULL,
  kind TEXT NOT NULL,
  message TEXT
) STRICT;

CREATE INDEX idx_job_events_job ON job_events (job_id, at);

CREATE INDEX idx_job_events_run ON job_events (job_run_id);

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

CREATE INDEX idx_job_runs_job ON job_runs (job_id);

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

CREATE INDEX idx_jobs_claim ON jobs (state, priority, run_after);

CREATE INDEX idx_jobs_finished ON jobs (finished_at) WHERE finished_at IS NOT NULL;

CREATE UNIQUE INDEX idx_jobs_one_queued_per_repository ON jobs (repository_id) WHERE state = 'queued';

CREATE UNIQUE INDEX idx_jobs_one_running_per_repository ON jobs (repository_id) WHERE state = 'running';

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

CREATE INDEX idx_repositories_ticket_project ON repositories (ticket_project_id);

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

CREATE INDEX idx_update_pr_dependencies_pr ON update_pr_dependencies (update_pr_id);

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

CREATE INDEX idx_update_prs_state ON update_prs (repository_id, state);

CREATE INDEX idx_update_prs_superseded_by ON update_prs (superseded_by_id);

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

CREATE INDEX idx_update_tickets_project ON update_tickets (ticket_project_id);

CREATE INDEX idx_update_tickets_state ON update_tickets (state);

DROP TABLE "monitored_repo_notifications";

DROP TABLE "monitored_repos";

DROP TABLE "notification_targets";

DROP TABLE "poll_run_events";

DROP TABLE "poll_runs";

CREATE TABLE "app_settings_sqlite_migrate_new" (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  invite_token_expiry_hours INTEGER NOT NULL DEFAULT 168 CHECK (
    invite_token_expiry_hours >= 1 AND invite_token_expiry_hours <= 720
  ),
  password_reset_token_expiry_minutes INTEGER NOT NULL DEFAULT 60 CHECK (
    password_reset_token_expiry_minutes >= 5 AND password_reset_token_expiry_minutes <= 1440
  ),
  updated_at TEXT NOT NULL
) STRICT;

INSERT INTO "app_settings_sqlite_migrate_new" ("id", "invite_token_expiry_hours", "password_reset_token_expiry_minutes", "updated_at")
SELECT "id", "invite_token_expiry_hours", "password_reset_token_expiry_minutes", "updated_at"
FROM "app_settings";

CREATE TABLE "integrations_sqlite_migrate_new" (
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

INSERT INTO "integrations_sqlite_migrate_new" (rowid, "id", "kind", "name", "base_url", "encrypted_payload", "created_at", "updated_at")
SELECT rowid, "id", "kind", "name", "base_url", "encrypted_payload", "created_at", "updated_at"
FROM "integrations";

DROP TABLE "app_settings";

ALTER TABLE "app_settings_sqlite_migrate_new" RENAME TO "app_settings";

DROP TABLE "integrations";

ALTER TABLE "integrations_sqlite_migrate_new" RENAME TO "integrations";

CREATE INDEX idx_integrations_kind ON integrations (kind);
