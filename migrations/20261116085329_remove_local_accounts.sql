-- sqlite-migrate: checksum f908ae4ce579689d90cb1902fdb2cd55cdee65e46f6a5372a6b12fcb28e67ee1

DROP TABLE "app_settings";

CREATE TABLE "auth_tokens_sqlite_migrate_new" (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL CHECK (kind IN ('setup')),
  token_hash TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  used_at TEXT,
  created_at TEXT NOT NULL
) STRICT;

INSERT INTO "auth_tokens_sqlite_migrate_new" (rowid, "id", "kind", "token_hash", "expires_at", "used_at", "created_at")
SELECT rowid, "id", "kind", "token_hash", "expires_at", "used_at", "created_at"
FROM "auth_tokens";

CREATE TABLE "users_sqlite_migrate_new" (
  id TEXT PRIMARY KEY,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
) STRICT;

INSERT INTO "users_sqlite_migrate_new" (rowid, "id", "created_at", "updated_at")
SELECT rowid, "id", "created_at", "updated_at"
FROM "users";

DROP TABLE "auth_tokens";

ALTER TABLE "auth_tokens_sqlite_migrate_new" RENAME TO "auth_tokens";

DROP TABLE "users";

ALTER TABLE "users_sqlite_migrate_new" RENAME TO "users";

CREATE INDEX idx_auth_tokens_token_hash ON auth_tokens (token_hash);
