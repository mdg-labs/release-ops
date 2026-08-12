#!/usr/bin/env node
/**
 * Fails if db/schema.sql drifts from migrations/ (SQLite sqldiff — no Atlas).
 * Run: npm run db:check
 */
import {
  existsSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  rmSync,
} from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { schemasMatchSemantically } from "../migrate-diff.mjs";

const root = join(dirname(fileURLToPath(import.meta.url)), "../..");
const SCHEMA = join(root, "db/schema.sql");
const MIGRATIONS = join(root, "migrations");

if (!existsSync(SCHEMA)) {
  console.error("db:check — missing db/schema.sql");
  process.exit(1);
}

const sqliteProbes = [
  ["sqlite3", ["--version"]],
  ["sqldiff", ["--help"]],
];
for (const [bin, probeArgs] of sqliteProbes) {
  const check = spawnSync(bin, probeArgs, { encoding: "utf8" });
  if (check.status !== 0) {
    console.warn(
      `db:check — ${bin} not installed; skip until P01 scaffold (apt: sqlite3 sqlite3-tools)`
    );
    process.exit(0);
  }
}

if (!existsSync(MIGRATIONS)) {
  console.error("db:check — migrations/ missing; run: make migrate-diff name=init");
  process.exit(1);
}

const tmp = mkdtempSync(join(tmpdir(), "release-ops-dbcheck-"));
const currentDb = join(tmp, "current.db");
const desiredDb = join(tmp, "desired.db");

function runSqlite(db, sql) {
  const r = spawnSync("sqlite3", [db], { input: sql, encoding: "utf8" });
  if (r.status !== 0) {
    console.error(r.stderr || r.stdout);
    process.exit(1);
  }
}

try {
  runSqlite(currentDb, "PRAGMA foreign_keys=ON;");
  const ups = readdirSync(MIGRATIONS)
    .filter((f) => f.endsWith(".up.sql"))
    .sort();
  for (const f of ups) {
    runSqlite(currentDb, readFileSync(join(MIGRATIONS, f), "utf8"));
  }
  runSqlite(desiredDb, readFileSync(SCHEMA, "utf8"));

  const diff = spawnSync("sqldiff", [currentDb, desiredDb], { encoding: "utf8" });
  if (diff.status !== 0) {
    console.error("db:check — sqldiff failed:", diff.stderr);
    process.exit(1);
  }

  if (diff.stdout.trim()) {
    if (schemasMatchSemantically(currentDb, desiredDb)) {
      console.log(
        "db:check OK — db/schema.sql matches migrations/ (semantic; column order differs only)"
      );
      process.exit(0);
    }
    console.error("db:check FAIL — db/schema.sql and migrations/ are out of sync.");
    console.error("Edit db/schema.sql then run: make migrate-diff name=<change>");
    console.error("--- sqldiff output ---\n", diff.stdout);
    process.exit(1);
  }

  console.log("db:check OK — db/schema.sql matches migrations/");
} finally {
  rmSync(tmp, { recursive: true, force: true });
}
