import assert from "node:assert/strict";
import { mkdtempSync, readdirSync, readFileSync, rmSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { postProcessSqldiff, schemasMatchSemantically } from "./migrate-diff.mjs";

const root = join(import.meta.dirname, "..");

function runSqlite(db, sql) {
  const r = spawnSync("sqlite3", [db], { input: sql, encoding: "utf8" });
  if (r.status !== 0) {
    throw new Error(r.stderr || r.stdout || "sqlite3 failed");
  }
}

function applyMigrations(db, exclude = /000004/) {
  runSqlite(db, "PRAGMA foreign_keys=ON;");
  for (const f of readdirSync(join(root, "migrations"))
    .filter((name) => name.endsWith(".up.sql") && !exclude.test(name))
    .sort()) {
    runSqlite(db, readFileSync(join(root, "migrations", f), "utf8"));
  }
}

test("matchDropTable handles sqldiff schema-mismatch comment suffix", () => {
  const raw =
    "DROP TABLE foo; -- due to schema mismatch\nCREATE TABLE foo (id TEXT PRIMARY KEY, extra TEXT);";
  const tmp = mkdtempSync(join(tmpdir(), "migrate-diff-test-"));
  const currentDb = join(tmp, "current.db");
  const desiredDb = join(tmp, "desired.db");
  try {
    runSqlite(currentDb, "CREATE TABLE foo (id TEXT PRIMARY KEY);");
    runSqlite(desiredDb, "CREATE TABLE foo (id TEXT PRIMARY KEY, extra TEXT);");
    const processed = postProcessSqldiff(raw, currentDb, desiredDb);
    assert.doesNotMatch(processed, /DROP TABLE foo/);
    assert.match(processed, /ADD COLUMN/);
  } finally {
    rmSync(tmp, { recursive: true, force: true });
  }
});

test("postProcessSqldiff rewrites mid-table column add on monitored_repos to ALTER TABLE", () => {
  const tmp = mkdtempSync(join(tmpdir(), "migrate-diff-test-"));
  const currentDb = join(tmp, "current.db");
  const desiredDb = join(tmp, "desired.db");

  try {
    applyMigrations(currentDb);
    runSqlite(desiredDb, readFileSync(join(root, "db/schema.sql"), "utf8"));

    const raw = spawnSync("sqldiff", [currentDb, desiredDb], { encoding: "utf8" }).stdout.trim();
    assert.match(raw, /DROP TABLE monitored_repos/);

    const up = postProcessSqldiff(raw, currentDb, desiredDb);
    assert.match(up, /ALTER TABLE "monitored_repos" ADD COLUMN "last_release_published_at" TEXT/);
    assert.doesNotMatch(up, /DROP TABLE "monitored_repos"/);
    assert.doesNotMatch(up, /DROP TABLE monitored_repos/);
  } finally {
    rmSync(tmp, { recursive: true, force: true });
  }
});

function withDbs(currentSql, desiredSql, fn) {
  const tmp = mkdtempSync(join(tmpdir(), "migrate-diff-test-"));
  const currentDb = join(tmp, "current.db");
  const desiredDb = join(tmp, "desired.db");
  try {
    runSqlite(currentDb, currentSql);
    runSqlite(desiredDb, desiredSql);
    const raw = spawnSync("sqldiff", [currentDb, desiredDb], { encoding: "utf8" }).stdout.trim();
    return fn({ currentDb, desiredDb, raw });
  } finally {
    rmSync(tmp, { recursive: true, force: true });
  }
}

test("column-order-only drift (ALTER-appended column) produces no migration", () => {
  withDbs(
    `CREATE TABLE t (id TEXT PRIMARY KEY, a TEXT NOT NULL, z TEXT);
     ALTER TABLE t ADD COLUMN "b" TEXT NOT NULL DEFAULT 'x' CHECK (b <> '');
     CREATE INDEX idx_t_a ON t (a);`,
    `CREATE TABLE t (id TEXT PRIMARY KEY, a TEXT NOT NULL, b TEXT NOT NULL DEFAULT 'x' CHECK (b <> ''), z TEXT);
     CREATE INDEX idx_t_a ON t (a);`,
    ({ currentDb, desiredDb, raw }) => {
      assert.match(raw, /DROP TABLE t/);
      assert.equal(postProcessSqldiff(raw, currentDb, desiredDb), "");
      assert.equal(schemasMatchSemantically(currentDb, desiredDb), true);
    }
  );
});

test("CHECK-only drift (invisible to sqldiff) emits a data-safe table rebuild", () => {
  const parent = (kinds) =>
    `CREATE TABLE p (id TEXT PRIMARY KEY, kind TEXT NOT NULL CHECK (kind IN (${kinds})), url TEXT,
       CHECK ((kind = 'a' AND url IS NULL) OR kind <> 'a'));
     CREATE INDEX idx_p_kind ON p (kind);
     CREATE TRIGGER trg_p AFTER INSERT ON p BEGIN SELECT 1; END;
     CREATE TABLE c (id TEXT PRIMARY KEY, pid TEXT NOT NULL REFERENCES p(id) ON DELETE RESTRICT);`;
  withDbs(parent("'a', 'old'"), parent("'a', 'new'"), ({ currentDb, desiredDb, raw }) => {
    assert.equal(raw, "");
    assert.equal(schemasMatchSemantically(currentDb, desiredDb), false);

    const up = postProcessSqldiff(raw, currentDb, desiredDb);
    assert.match(up, /PRAGMA foreign_keys=OFF;/);
    assert.match(up, /CREATE TABLE "p_new"[\s\S]*'new'/);
    assert.match(up, /INSERT INTO "p_new" \("id", "kind", "url"\) SELECT "id", "kind", "url" FROM "p";/);
    assert.match(up, /DROP TABLE "p";/);
    assert.match(up, /ALTER TABLE "p_new" RENAME TO "p";/);
    assert.match(up, /CREATE INDEX idx_p_kind ON p \(kind\);/);
    assert.match(up, /CREATE TRIGGER trg_p/);
    assert.match(up, /pragma_foreign_key_check/);
    assert.doesNotMatch(up, /"c"/);

    // Applying the generated SQL (FKs off, outside a transaction) keeps rows and FKs intact.
    runSqlite(currentDb, "INSERT INTO p VALUES ('1', 'a', NULL); INSERT INTO c VALUES ('x', '1');");
    runSqlite(currentDb, up);
    assert.equal(schemasMatchSemantically(currentDb, desiredDb), true);
    const check = spawnSync("sqlite3", [currentDb, "SELECT COUNT(*) FROM c; PRAGMA foreign_key_check;"], {
      encoding: "utf8",
    });
    assert.equal(check.stdout.trim(), "1");
  });
});
