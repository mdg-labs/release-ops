-- sqlite-migrate: checksum a2a93a7b5c0cc1ec875174f8336256770959c1e15e6e6cb717fd2b3aab1a3efc

CREATE TABLE login_link_tokens (
  id TEXT PRIMARY KEY,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TEXT NOT NULL,
  consumed_at TEXT,
  created_at TEXT NOT NULL
) STRICT;

CREATE TABLE "users_sqlite_migrate_new" (
  id TEXT PRIMARY KEY,
  email TEXT UNIQUE,
  password_hash TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
) STRICT;

INSERT INTO "users_sqlite_migrate_new" (rowid, "id", "email", "password_hash", "created_at", "updated_at")
SELECT rowid, "id", "email", "password_hash", "created_at", "updated_at"
FROM "users";

DROP TABLE "users";

ALTER TABLE "users_sqlite_migrate_new" RENAME TO "users";
