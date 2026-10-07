-- sqlite-migrate: checksum 2c2d1df56b98db2d194464d2f24b754237f771ea1091a4cc3f7e81dec6c1c890

ALTER TABLE integrations ADD COLUMN is_default integer NOT NULL DEFAULT 0 CHECK (is_default in (0, 1) and (is_default = 0 or kind in ('github', 'gitlab', 'gitea', 'forgejo', 'codeberg')));

CREATE UNIQUE INDEX idx_integrations_default_kind ON integrations (kind) WHERE is_default = 1;
