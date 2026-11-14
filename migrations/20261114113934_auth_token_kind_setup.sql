-- sqlite-migrate: checksum 452d72ca29b18dc57bd6458741a0d5ad6aefdd19cdbb13f5f2bee88239064d37

CREATE TABLE "auth_tokens_sqlite_migrate_new" (
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

INSERT INTO "auth_tokens_sqlite_migrate_new" (rowid, "id", "kind", "email", "token_hash", "new_email", "invited_by_user_id", "expires_at", "used_at", "created_at")
SELECT rowid, "id", "kind", "email", "token_hash", "new_email", "invited_by_user_id", "expires_at", "used_at", "created_at"
FROM "auth_tokens";

DROP TABLE "auth_tokens";

ALTER TABLE "auth_tokens_sqlite_migrate_new" RENAME TO "auth_tokens";

CREATE INDEX idx_auth_tokens_token_hash ON auth_tokens (token_hash);
