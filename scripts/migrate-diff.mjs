#!/usr/bin/env node
/**
 * Generate golang-migrate SQL from db/schema.sql drift using SQLite sqldiff (free, no Atlas).
 *
 * Usage: node scripts/migrate-diff.mjs <name>
 *   name — snake_case description, e.g. add_foo_column
 *
 * Workflow:
 *   1. current.db  ← apply all migrations/*.up.sql
 *   2. desired.db  ← apply db/schema.sql
 *   3. sqldiff current desired → post-process → .up.sql
 *   4. sqldiff desired current → post-process → .down.sql
 *
 * Post-processing rewrites destructive DROP TABLE + CREATE TABLE pairs into
 * data-safe ALTER TABLE ADD COLUMN or SQLite table-rebuild patterns.
 */
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const SCHEMA = join(root, "db/schema.sql");
const MIGRATIONS = join(root, "migrations");

const name = process.argv[2];
if (!name || !/^[a-z][a-z0-9_]*$/.test(name)) {
  console.error("Usage: node scripts/migrate-diff.mjs <snake_case_name>");
  process.exit(1);
}

function requireBinary(bin, probeArgs) {
  const check = spawnSync(bin, probeArgs, { encoding: "utf8" });
  if (check.status !== 0) {
    console.error(`migrate-diff requires ${bin} on PATH (SQLite tools)`);
    process.exit(1);
  }
}

requireBinary("sqlite3", ["--version"]);
requireBinary("sqldiff", ["--help"]);

if (!existsSync(SCHEMA)) {
  console.error(`Missing ${SCHEMA}`);
  process.exit(1);
}

mkdirSync(MIGRATIONS, { recursive: true });

const tmp = mkdtempSync(join(tmpdir(), "release-ops-migrate-"));
const currentDb = join(tmp, "current.db");
const desiredDb = join(tmp, "desired.db");

function runSqlite(db, sql) {
  const r = spawnSync("sqlite3", [db], { input: sql, encoding: "utf8" });
  if (r.status !== 0) {
    console.error(r.stderr || r.stdout);
    throw new Error(`sqlite3 failed on ${db}`);
  }
  return r.stdout;
}

function queryScalar(db, sql) {
  return runSqlite(db, sql).trim();
}

function applyMigrations(db) {
  runSqlite(db, "PRAGMA foreign_keys=ON;");
  if (!existsSync(MIGRATIONS)) return;
  const ups = readdirSync(MIGRATIONS)
    .filter((f) => f.endsWith(".up.sql"))
    .sort();
  for (const f of ups) {
    runSqlite(db, readFileSync(join(MIGRATIONS, f), "utf8"));
  }
}

function applySchema(db) {
  runSqlite(db, readFileSync(SCHEMA, "utf8"));
}

function sqldiff(from, to) {
  const r = spawnSync("sqldiff", [from, to], { encoding: "utf8" });
  if (r.status !== 0) {
    console.error(r.stderr || r.stdout);
    throw new Error("sqldiff failed");
  }
  return r.stdout.trim();
}

function quoteIdent(name) {
  return `"${name.replace(/"/g, '""')}"`;
}

function parseStatements(sql) {
  const statements = [];
  let current = "";
  let depth = 0;
  let inSingle = false;
  let inDouble = false;
  for (let i = 0; i < sql.length; i++) {
    const ch = sql[i];
    const prev = sql[i - 1];
    if (ch === "'" && !inDouble && prev !== "\\") inSingle = !inSingle;
    if (ch === '"' && !inSingle && prev !== "\\") inDouble = !inDouble;
    if (!inSingle && !inDouble) {
      if (ch === "(") depth++;
      if (ch === ")") depth--;
      if (ch === ";" && depth === 0) {
        const stmt = current.trim();
        if (stmt) statements.push(stmt);
        current = "";
        continue;
      }
    }
    current += ch;
  }
  const tail = current.trim();
  if (tail) statements.push(tail);
  return statements;
}

function matchDropTable(stmt) {
  const m = stmt.match(/^DROP TABLE\s+(?:(?:IF EXISTS)\s+)?[`"]?(\w+)[`"]?(?:\s*;|\s+--.*)?$/is);
  return m ? { name: m[1] } : null;
}

function matchCreateTable(stmt) {
  const m = stmt.match(/^CREATE TABLE\s+(?:(?:IF NOT EXISTS)\s+)?[`"]?(\w+)[`"]?\s*\(/is);
  return m ? { name: m[1], sql: stmt } : null;
}

function matchCreateIndex(stmt) {
  const m = stmt.match(
    /^CREATE(?:\s+UNIQUE)?\s+INDEX(?:\s+IF NOT EXISTS)?\s+[`"]?(\w+)[`"]?\s+ON\s+[`"]?(\w+)[`"]?\s*\(/is
  );
  return m ? { index: m[1], table: m[2], sql: stmt } : null;
}

function splitTopLevelComma(body) {
  const parts = [];
  let current = "";
  let depth = 0;
  let inSingle = false;
  let inDouble = false;
  for (let i = 0; i < body.length; i++) {
    const ch = body[i];
    const prev = body[i - 1];
    if (ch === "'" && !inDouble && prev !== "\\") inSingle = !inSingle;
    if (ch === '"' && !inSingle && prev !== "\\") inDouble = !inDouble;
    if (!inSingle && !inDouble) {
      if (ch === "(") depth++;
      if (ch === ")") depth--;
      if (ch === "," && depth === 0) {
        parts.push(current.trim());
        current = "";
        continue;
      }
    }
    current += ch;
  }
  if (current.trim()) parts.push(current.trim());
  return parts;
}

function parseCreateTableBody(createSql) {
  const m = createSql.match(
    /^CREATE TABLE\s+(?:(?:IF NOT EXISTS)\s+)?[`"]?(\w+)[`"]?\s*\(([\s\S]+)\)\s*;?\s*$/i
  );
  if (!m) {
    throw new Error(`migrate-diff: cannot parse CREATE TABLE: ${createSql.slice(0, 120)}`);
  }
  return { table: m[1], parts: splitTopLevelComma(m[2]) };
}

function parseColumnDef(def) {
  if (/^(PRIMARY KEY|UNIQUE|CHECK|FOREIGN KEY|CONSTRAINT)\s/i.test(def)) {
    return { kind: "constraint", raw: def };
  }
  const nameMatch = def.match(/^[`"]?(\w+)[`"]?\s+([\s\S]+)$/);
  if (!nameMatch) return { kind: "unknown", raw: def };
  const name = nameMatch[1];
  const rest = nameMatch[2].trim();
  const typeMatch = rest.match(/^(\S+)/);
  const type = typeMatch ? typeMatch[1].toUpperCase() : "TEXT";
  const notNull = /\bNOT NULL\b/i.test(rest);
  const defaultMatch = rest.match(/\bDEFAULT\s+((?:'(?:''|[^'])*'|\([^)]*\)|\S+))/i);
  const defaultVal = defaultMatch ? defaultMatch[1] : null;
  const checkMatch = rest.match(/\bCHECK\s*(\((?:[^()]*|\([^()]*\))*\))/i);
  const check = checkMatch ? normalizeWhitespace(checkMatch[1]) : null;
  const isPk = /\bPRIMARY KEY\b/i.test(rest);
  const rawAfterName = rest;
  return { kind: "column", name, type, notNull, defaultVal, check, isPk, rawAfterName, raw: def };
}

function normalizeWhitespace(s) {
  return s.replace(/\s+/g, " ").trim();
}

function normalizeDefault(val) {
  if (val == null) return null;
  return normalizeWhitespace(val).replace(/''/g, "'");
}

function isAppendOnlyColumnAdd(oldCols, newCols) {
  const oldNames = oldCols.map((c) => c.name);
  const newNames = newCols.map((c) => c.name);
  const oldSet = new Set(oldNames);
  const addedNames = newNames.filter((n) => !oldSet.has(n));
  if (addedNames.length === 0) return false;

  let oldIdx = 0;
  for (const name of newNames) {
    if (oldIdx < oldNames.length && name === oldNames[oldIdx]) {
      oldIdx++;
    }
  }
  if (oldIdx !== oldNames.length) return false;

  const lastOldPos = Math.max(...oldNames.map((n) => newNames.indexOf(n)));
  const firstAddedPos = Math.min(...addedNames.map((n) => newNames.indexOf(n)));
  return firstAddedPos > lastOldPos;
}

function columnsCompatible(oldCol, newCol) {
  if (oldCol.type !== newCol.type) return false;
  if (oldCol.notNull !== newCol.notNull) return false;
  if (oldCol.isPk !== newCol.isPk) return false;
  if (normalizeDefault(oldCol.defaultVal) !== normalizeDefault(newCol.defaultVal)) return false;
  if (normalizeWhitespace(oldCol.check || "") !== normalizeWhitespace(newCol.check || "")) return false;
  return true;
}

function tableLevelConstraints(parts) {
  return parts
    .filter((p) => parseColumnDef(p).kind === "constraint")
    .map((p) => normalizeWhitespace(p));
}

function getCreateSql(db, table) {
  const escaped = table.replace(/'/g, "''");
  const sql = queryScalar(
    db,
    `SELECT sql FROM sqlite_master WHERE type='table' AND name='${escaped}';`
  );
  if (!sql) {
    throw new Error(`migrate-diff: table ${table} not found in ${db}`);
  }
  return sql;
}

function indexExists(db, indexName) {
  const escaped = indexName.replace(/'/g, "''");
  const count = queryScalar(
    db,
    `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='${escaped}';`
  );
  return Number(count) > 0;
}

function listTableIndexes(db, table) {
  const escaped = table.replace(/'/g, "''");
  const out = runSqlite(
    db,
    `SELECT name, sql FROM sqlite_master WHERE type='index' AND tbl_name='${escaped}' AND sql IS NOT NULL ORDER BY name;`
  ).trim();
  if (!out) return [];
  return out.split("\n").map((line) => {
    const tab = line.indexOf("|");
    return { name: line.slice(0, tab), sql: line.slice(tab + 1) };
  });
}

function hasInboundForeignKeys(db, tableName) {
  const escaped = tableName.replace(/'/g, "''");
  const pattern = `%REFERENCES ${escaped}(%`;
  const count = queryScalar(
    db,
    `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND sql LIKE '${pattern.replace(/'/g, "''")}';`
  );
  return Number(count) > 0;
}

function analyzeTableChange(fromDb, toDb, tableName) {
  const oldSql = getCreateSql(fromDb, tableName);
  const newSql = getCreateSql(toDb, tableName);
  const oldBody = parseCreateTableBody(oldSql);
  const newBody = parseCreateTableBody(newSql);

  const oldCols = oldBody.parts.map(parseColumnDef).filter((c) => c.kind === "column");
  const newCols = newBody.parts.map(parseColumnDef).filter((c) => c.kind === "column");
  const oldByName = new Map(oldCols.map((c) => [c.name, c]));
  const newByName = new Map(newCols.map((c) => [c.name, c]));

  const removed = oldCols.filter((c) => !newByName.has(c.name));
  const added = newCols.filter((c) => !oldByName.has(c.name));
  const modified = oldCols
    .filter((c) => newByName.has(c.name) && !columnsCompatible(c, newByName.get(c.name)))
    .map((c) => c.name);

  const oldConstraints = tableLevelConstraints(oldBody.parts).sort().join("|");
  const newConstraints = tableLevelConstraints(newBody.parts).sort().join("|");

  if (removed.length > 0) {
    return { type: "rebuild", reason: `removed columns: ${removed.map((c) => c.name).join(", ")}` };
  }
  if (modified.length > 0) {
    return { type: "rebuild", reason: `modified columns: ${modified.join(", ")}` };
  }
  if (oldConstraints !== newConstraints) {
    return { type: "rebuild", reason: "table-level constraints changed" };
  }
  if (added.length > 0) {
    if (!isAppendOnlyColumnAdd(oldCols, newCols)) {
      if (hasInboundForeignKeys(fromDb, tableName)) {
        return { type: "add_columns", added, newSql, appendOnly: false };
      }
      return { type: "rebuild", reason: "new columns are not append-only (column order mismatch)" };
    }
    return { type: "add_columns", added, newSql, appendOnly: true };
  }
  return { type: "ambiguous", reason: "DROP/CREATE pair with no detectable column changes" };
}

function emitAddColumns(tableName, addedCols) {
  return addedCols.map(
    (col) => `ALTER TABLE ${quoteIdent(tableName)} ADD COLUMN ${quoteIdent(col.name)} ${col.rawAfterName}`
  );
}

function emitTableRebuild(fromDb, toDb, tableName, newCreateSql) {
  const tmpTable = `${tableName}_new`;
  const newSql = newCreateSql.replace(
    new RegExp(`^CREATE TABLE\\s+(?:(?:IF NOT EXISTS)\\s+)?\`?${tableName}\`?`, "i"),
    `CREATE TABLE ${quoteIdent(tmpTable)}`
  );

  const oldBody = parseCreateTableBody(getCreateSql(fromDb, tableName));
  const newBody = parseCreateTableBody(newCreateSql);
  const oldCols = oldBody.parts.map(parseColumnDef).filter((c) => c.kind === "column").map((c) => c.name);
  const newColSet = new Set(
    newBody.parts.map(parseColumnDef).filter((c) => c.kind === "column").map((c) => c.name)
  );
  const commonCols = oldCols.filter((n) => newColSet.has(n));
  if (commonCols.length === 0) {
    throw new Error(
      `migrate-diff: ambiguous rebuild for ${tableName} — no shared columns between old and new schema`
    );
  }

  const colList = commonCols.map(quoteIdent).join(", ");
  const indexes = listTableIndexes(toDb, tableName);
  const lines = [
    "PRAGMA foreign_keys=OFF;",
    newSql,
    `INSERT INTO ${quoteIdent(tmpTable)} (${colList}) SELECT ${colList} FROM ${quoteIdent(tableName)};`,
    `DROP TABLE ${quoteIdent(tableName)};`,
    `ALTER TABLE ${quoteIdent(tmpTable)} RENAME TO ${quoteIdent(tableName)};`,
    ...indexes.map((idx) => idx.sql),
    "PRAGMA foreign_keys=ON;",
  ];
  return lines;
}

function rewriteTableChange(fromDb, toDb, tableName, createSql) {
  const analysis = analyzeTableChange(fromDb, toDb, tableName);
  if (analysis.type === "add_columns") {
    return emitAddColumns(tableName, analysis.added);
  }
  if (analysis.type === "rebuild") {
    return emitTableRebuild(fromDb, toDb, tableName, createSql);
  }
  throw new Error(`migrate-diff: ambiguous rewrite for table ${tableName}: ${analysis.reason}`);
}

/**
 * Rewrite destructive DROP TABLE + CREATE TABLE pairs into data-safe SQL.
 */
export function postProcessSqldiff(rawSql, fromDb, toDb) {
  if (!rawSql.trim()) return rawSql;

  const statements = parseStatements(rawSql);
  const out = [];
  const skipIndexForTable = new Set();

  for (let i = 0; i < statements.length; i++) {
    const drop = matchDropTable(statements[i]);
    if (drop && i + 1 < statements.length) {
      const create = matchCreateTable(statements[i + 1]);
      if (create && create.name === drop.name) {
        const rewritten = rewriteTableChange(fromDb, toDb, drop.name, create.sql);
        out.push(...rewritten);
        skipIndexForTable.add(drop.name);
        i += 1;
        continue;
      }
    }

    const idx = matchCreateIndex(statements[i]);
    if (idx && skipIndexForTable.has(idx.table) && indexExists(fromDb, idx.index)) {
      continue;
    }

    out.push(statements[i]);
  }

  return out.map((s) => (s.endsWith(";") ? s : `${s};`)).join("\n\n");
}

try {
  applyMigrations(currentDb);
  applySchema(desiredDb);

  const rawUpSql = sqldiff(currentDb, desiredDb);
  const rawDownSql = sqldiff(desiredDb, currentDb);

  const upSql = postProcessSqldiff(rawUpSql, currentDb, desiredDb);
  const downSql = postProcessSqldiff(rawDownSql, desiredDb, currentDb);

  if (!upSql) {
    console.log("No schema drift — migrations/ already matches db/schema.sql");
    process.exit(0);
  }

  const existing = readdirSync(MIGRATIONS)
    .map((f) => /^(\d{6})_/.exec(f))
    .filter(Boolean)
    .map((m) => Number(m[1]));
  const next = String((existing.length ? Math.max(...existing) : 0) + 1).padStart(6, "0");

  const upPath = join(MIGRATIONS, `${next}_${name}.up.sql`);
  const downPath = join(MIGRATIONS, `${next}_${name}.down.sql`);

  writeFileSync(
    upPath,
    `-- Generated by scripts/migrate-diff.mjs — do not edit by hand\n-- Re-generate: make migrate-diff name=${name}\n\nPRAGMA foreign_keys = ON;\n\n${upSql}\n`
  );
  writeFileSync(
    downPath,
    `-- Generated by scripts/migrate-diff.mjs — do not edit by hand\n\n${downSql || "-- no-op\n"}\n`
  );

  console.log(`Wrote ${upPath}`);
  console.log(`Wrote ${downPath}`);
} finally {
  rmSync(tmp, { recursive: true, force: true });
}
