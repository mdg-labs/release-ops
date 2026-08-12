import assert from "node:assert/strict";
import { mkdtempSync, readdirSync, readFileSync, rmSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { postProcessSqldiff } from "./migrate-diff.mjs";

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
