-- sqlite-migrate: checksum cde94596b8fc35c6fac6680a2e14a6873b8309f3f01f1cdd1942efcf4a9f0e8a

ALTER TABLE update_tickets ADD COLUMN target_state text CHECK (target_state in ('done', 'cancelled', 'superseded'));

ALTER TABLE update_tickets ADD COLUMN hands_off_at text;

CREATE TABLE "ticket_projects_sqlite_migrate_new" (
  id TEXT PRIMARY KEY,
  integration_id TEXT NOT NULL REFERENCES integrations(id) ON DELETE RESTRICT,
  external_project_id TEXT NOT NULL,
  name TEXT NOT NULL,
  create_config TEXT NOT NULL,
  status_mapping TEXT NOT NULL,
  content_templates TEXT NOT NULL DEFAULT '{"title":"","description":"","updateComment":"","supersedeComment":""}' CHECK (json_valid(content_templates)),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  filter TEXT NOT NULL DEFAULT '{"updateTypes":[],"labels":[],"skipAutomerged":true}' CHECK (json_valid(filter)),
  UNIQUE (integration_id, external_project_id),
  CHECK (json_valid(create_config) AND json_valid(status_mapping))
) STRICT;

INSERT INTO "ticket_projects_sqlite_migrate_new" (rowid, "id", "integration_id", "external_project_id", "name", "create_config", "status_mapping", "content_templates", "created_at", "updated_at", "filter")
SELECT rowid, "id", "integration_id", "external_project_id", "name", "create_config", "status_mapping", "content_templates", "created_at", "updated_at", "filter"
FROM "ticket_projects";

DROP TABLE "ticket_projects";

ALTER TABLE "ticket_projects_sqlite_migrate_new" RENAME TO "ticket_projects";

CREATE INDEX idx_ticket_projects_integration ON ticket_projects (integration_id);
