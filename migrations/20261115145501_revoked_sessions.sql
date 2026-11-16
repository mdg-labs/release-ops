-- sqlite-migrate: checksum 8389d134eb67892a87721bf59beaf92940d03991838c4b23f5301f1658057d0f

CREATE TABLE revoked_sessions (
  token_hash TEXT PRIMARY KEY,
  expiry REAL NOT NULL
) STRICT;

CREATE INDEX idx_revoked_sessions_expiry ON revoked_sessions (expiry);
