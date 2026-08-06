-- Release Ops — application schema (app.db)
-- Auth tables (BetterAuth) live in auth.db — see specs.html § Authentication
-- Apply via golang-migrate from worker on startup

PRAGMA foreign_keys = ON;

CREATE TABLE app_settings (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  poll_interval_minutes INTEGER NOT NULL DEFAULT 360 CHECK (poll_interval_minutes >= 5),
  updated_at TEXT NOT NULL
);

-- kind: source → github, gitlab | ticket → phasical, jira, linear
CREATE TABLE integrations (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK (kind IN ('github', 'gitlab', 'phasical', 'jira', 'linear')),
  name TEXT NOT NULL,
  base_url TEXT,
  encrypted_payload TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  CHECK (
    (kind = 'github' AND base_url IS NULL)
    OR (kind = 'gitlab' AND base_url IS NOT NULL)
    OR (kind = 'phasical' AND base_url IS NOT NULL)
    OR (kind = 'jira' AND base_url IS NOT NULL)
    OR (kind = 'linear' AND base_url IS NULL)
  )
);

CREATE TABLE monitored_repos (
  id TEXT PRIMARY KEY,
  source_kind TEXT NOT NULL CHECK (source_kind IN ('github', 'gitlab')),
  project_path TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  source_integration_id TEXT REFERENCES integrations(id),
  ticket_kind TEXT NOT NULL CHECK (ticket_kind IN ('phasical', 'jira', 'linear')),
  ticket_integration_id TEXT NOT NULL REFERENCES integrations(id),
  ticket_config TEXT NOT NULL,
  last_known_tag TEXT,
  last_polled_at TEXT,
  last_error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (source_kind, project_path),
  CHECK (
    json_valid(ticket_config)
    AND (
      (ticket_kind = 'phasical' AND json_extract(ticket_config, '$.projectId') IS NOT NULL)
      OR (ticket_kind = 'jira' AND json_extract(ticket_config, '$.projectKey') IS NOT NULL)
      OR (ticket_kind = 'linear' AND json_extract(ticket_config, '$.teamId') IS NOT NULL)
    )
  )
);

CREATE TABLE poll_runs (
  id TEXT PRIMARY KEY,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  status TEXT NOT NULL CHECK (status IN ('running', 'success', 'partial', 'failed')),
  repos_checked INTEGER NOT NULL DEFAULT 0,
  tickets_created INTEGER NOT NULL DEFAULT 0,
  errors_json TEXT NOT NULL DEFAULT '[]'
);

CREATE TABLE poll_run_events (
  id TEXT PRIMARY KEY,
  poll_run_id TEXT NOT NULL REFERENCES poll_runs(id) ON DELETE CASCADE,
  monitored_repo_id TEXT REFERENCES monitored_repos(id) ON DELETE SET NULL,
  action TEXT NOT NULL CHECK (action IN ('baseline', 'skip', 'create', 'error')),
  detail TEXT,
  created_at TEXT NOT NULL
);

CREATE INDEX idx_integrations_kind ON integrations (kind);
CREATE INDEX idx_monitored_repos_enabled ON monitored_repos (enabled);
CREATE INDEX idx_monitored_repos_source ON monitored_repos (source_kind);
CREATE INDEX idx_poll_run_events_run ON poll_run_events (poll_run_id);
CREATE INDEX idx_poll_runs_started ON poll_runs (started_at);
