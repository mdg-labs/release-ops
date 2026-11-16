#!/usr/bin/env node
/**
 * Checks what Renovate reported for each fixture repository against expectations.json.
 *
 * Run: node assert.mjs --results <dir> --renovate-version <x.y.z> [--self-check]
 *
 * <dir> holds <fixture>/extract and <fixture>/lookup (the latter only for a fixture that has
 * lookup expectations), each with report.json (Renovate's reportType=file report), log.ndjson
 * (its JSON log) and exit_code.
 *
 * With --self-check, every expectation is also made deliberately wrong, one way at a time, and
 * the check must then fail: it proves the assertions are not vacuous.
 */
import { existsSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));

export function loadExpectations(path = join(here, "expectations.json")) {
  return JSON.parse(readFileSync(path, "utf8"));
}

/** Reads one run's files; a missing file is null, so the check reports it instead of throwing. */
export function loadRun(dir) {
  const read = (name) => (existsSync(join(dir, name)) ? readFileSync(join(dir, name), "utf8") : null);
  return { report: read("report.json"), log: read("log.ndjson"), exitCode: read("exit_code") };
}

/**
 * The repository's report entry: the one repository the run reported. With needsPackageFiles
 * it must also hold the ingest contract of the report, branches and packageFiles.
 */
function repositoryOf(reportText, problems, needsPackageFiles = true) {
  let report;
  try {
    report = JSON.parse(reportText);
  } catch (err) {
    problems.push(`report.json is not valid JSON: ${err.message}`);
    return null;
  }
  const names = Object.keys(report?.repositories ?? {});
  if (names.length !== 1) {
    problems.push(`report.repositories has ${names.length} entries, expected one`);
    return null;
  }
  const repo = report.repositories[names[0]];
  if (!needsPackageFiles) return repo;
  if (!Array.isArray(repo.branches)) problems.push("report.repositories.<repo>.branches is not an array");
  if (typeof repo.packageFiles !== "object" || repo.packageFiles === null) {
    problems.push("report.repositories.<repo>.packageFiles is not an object");
    return null;
  }
  return repo;
}

/** The log is one JSON object per line; check the records the run is read from. */
function checkLog(logText, renovateVersion, problems) {
  const records = [];
  for (const line of logText.split("\n")) {
    if (!line.trim().startsWith("{")) continue;
    try {
      records.push(JSON.parse(line));
    } catch {
      problems.push(`a log line is not valid JSON: ${line.slice(0, 80)}`);
    }
  }
  const started = records.find((r) => r.msg === "Repository started");
  if (!started) problems.push('the log has no "Repository started" record');
  else if (started.renovateVersion !== renovateVersion) {
    problems.push(`the log says Renovate ${started.renovateVersion}, expected the pinned ${renovateVersion}`);
  }
  if (!records.some((r) => r.msg === "Repository finished")) problems.push('the log has no "Repository finished" record');
  for (const r of records) {
    if (typeof r.level !== "number" || typeof r.msg !== "string") {
      problems.push("a log record has no numeric level and string msg");
      break;
    }
  }
}

function packageFileOf(repo, expected, problems) {
  const files = repo.packageFiles[expected.manager];
  if (!Array.isArray(files)) {
    problems.push(`no packageFiles for manager ${expected.manager}; the report has ${Object.keys(repo.packageFiles).join(", ") || "none"}`);
    return null;
  }
  const file = files.find((f) => f.packageFile === expected.packageFile);
  if (!file) {
    problems.push(`${expected.manager} has no package file ${expected.packageFile}; it has ${files.map((f) => f.packageFile).join(", ")}`);
    return null;
  }
  return file;
}

/**
 * The dependencies a fixture is checked against. A skipped one is left out unless the fixture
 * lists its skip reason in allowSkipReasons: the GitHub Actions fixture runs without a GitHub
 * token, so Renovate extracts its actions but skips them with github-token-required.
 */
function depsOf(file, expected) {
  const ignored = new Set(expected.ignoreDatasources ?? []);
  const allowed = new Set(expected.allowSkipReasons ?? []);
  return (file.deps ?? []).filter((d) => (!d.skipReason || allowed.has(d.skipReason)) && !ignored.has(d.datasource));
}

/**
 * A repository with no config under requireConfig=required: exactly what the worker reads
 * (skippedForNoConfig in internal/renovate/handler.go) to record a run as skipped for a
 * missing config. Renovate exits 0 and the report entry for the repository has status
 * "disabled". Nothing else about the run is expected: it ends before it extracts anything.
 */
function verifyNoConfigRun(phase, run, renovateVersion) {
  const problems = [];
  if (run.exitCode === null || run.report === null || run.log === null) {
    return [`${phase}: the run left no ${[run.exitCode === null && "exit_code", run.report === null && "report.json", run.log === null && "log.ndjson"].filter(Boolean).join(", ")}`];
  }
  if (run.exitCode.trim() !== "0") problems.push(`renovate exited with ${run.exitCode.trim()}`);
  const started = run.log
    .split("\n")
    .map((line) => {
      try {
        return JSON.parse(line);
      } catch {
        return null;
      }
    })
    .find((r) => r?.msg === "Repository started");
  if (!started) problems.push('the log has no "Repository started" record');
  else if (started.renovateVersion !== renovateVersion) {
    problems.push(`the log says Renovate ${started.renovateVersion}, expected the pinned ${renovateVersion}`);
  }
  const repo = repositoryOf(run.report, problems, false);
  if (repo && repo.status !== "disabled") {
    problems.push(`the repository reports status ${JSON.stringify(repo.status)}, expected "disabled" (no config with requireConfig=required)`);
  }
  return problems.map((p) => `${phase}: ${p}`);
}

const updateKey = (u) => `${u.newVersion}|${u.newValue}|${u.updateType}`;

/**
 * The problems with one run of one fixture; none means it matches.
 * phase is "extract" or "lookup"; wanted is expected.extract or expected.lookup.
 */
export function verifyRun(phase, expected, run, renovateVersion) {
  const problems = [];
  const wanted = expected[phase];
  if (expected.noConfig) return verifyNoConfigRun(phase, run, renovateVersion);
  if (run.exitCode === null || run.report === null || run.log === null) {
    return [`${phase}: the run left no ${[run.exitCode === null && "exit_code", run.report === null && "report.json", run.log === null && "log.ndjson"].filter(Boolean).join(", ")}`];
  }
  if (run.exitCode.trim() !== "0") problems.push(`renovate exited with ${run.exitCode.trim()}`);
  checkLog(run.log, renovateVersion, problems);
  const repo = repositoryOf(run.report, problems);
  if (!repo) return problems.map((p) => `${phase}: ${p}`);
  if (repo.status === "disabled") problems.push('the repository reports status "disabled", the status of a repository with no config');
  const file = packageFileOf(repo, expected, problems);
  if (file) {
    const deps = depsOf(file, expected);
    const byName = new Map(deps.map((d) => [d.depName, d]));
    for (const want of wanted) {
      const dep = byName.get(want.depName);
      if (!dep) {
        problems.push(`dependency ${want.depName} was not reported`);
        continue;
      }
      for (const [field, value] of Object.entries(want)) {
        if (field === "updates") {
          const got = (dep.updates ?? []).map(updateKey).sort();
          const exp = value.map(updateKey).sort();
          if (JSON.stringify(got) !== JSON.stringify(exp)) {
            problems.push(`${want.depName} updates are [${got.join(", ")}], expected [${exp.join(", ")}] (newVersion|newValue|updateType)`);
          }
        } else if (dep[field] !== value) {
          problems.push(`${want.depName} ${field} is ${JSON.stringify(dep[field])}, expected ${JSON.stringify(value)}`);
        }
      }
    }
    const wantedNames = new Set(wanted.map((w) => w.depName));
    for (const d of deps) {
      if (!wantedNames.has(d.depName)) problems.push(`unexpected dependency ${d.depName} (${d.datasource})`);
    }
  }
  return problems.map((p) => `${phase}: ${p}`);
}

/** Every run of a fixture: extract always, lookup when the fixture has lookup expectations. */
export function verifyFixture(expected, runs, renovateVersion) {
  const problems = verifyRun("extract", expected, runs.extract, renovateVersion);
  if (expected.lookup) {
    problems.push(...verifyRun("lookup", expected, runs.lookup, renovateVersion));
  } else if (runs.lookup) {
    problems.push("lookup: ran although the fixture has no lookup expectations");
  }
  return problems;
}

/**
 * Deliberately wrong variants of a fixture's expectations; each must make verifyFixture fail.
 * Returns [label, expected] pairs.
 */
export function mutations(expected) {
  const out = [];
  const copy = () => structuredClone(expected);
  if (expected.noConfig) {
    const configured = { manager: "npm", packageFile: "package.json", extract: [{ depName: "fixture-lib", currentValue: "1.0.0", datasource: "npm" }] };
    out.push(["a fixture that has a config", configured]);
    return out;
  }
  out.push(["a fixture that has no config", { noConfig: true }]);
  for (const phase of ["extract", "lookup"]) {
    if (!expected[phase]) continue;
    let e = copy();
    e[phase][0].depName = `${e[phase][0].depName}-wrong`;
    out.push([`${phase}: a dependency that is not there`, e]);
    e = copy();
    e[phase][0].currentValue = "0.0.0-wrong";
    if (phase === "lookup") e[phase][0].currentVersion = "0.0.0-wrong";
    out.push([`${phase}: a wrong current version`, e]);
    e = copy();
    e[phase].pop();
    out.push([`${phase}: a reported dependency left out`, e]);
  }
  let e = copy();
  e.extract[0].datasource = "wrong-datasource";
  out.push(["extract: a wrong datasource", e]);
  e = copy();
  e.manager = "wrong-manager";
  out.push(["a wrong manager", e]);
  if (expected.lookup) {
    e = copy();
    e.lookup[0].updates[0].newVersion = "99.0.0";
    out.push(["lookup: a wrong new version", e]);
    e = copy();
    e.lookup[0].updates[0].updateType = "wrong";
    out.push(["lookup: a wrong update type", e]);
    e = copy();
    e.lookup[0].updates.pop();
    out.push(["lookup: an update left out", e]);
  }
  return out;
}

function arg(name) {
  const i = process.argv.indexOf(name);
  return i >= 0 ? process.argv[i + 1] : undefined;
}

if (process.argv[1] && import.meta.url === new URL(`file://${process.argv[1]}`).href) {
  const results = arg("--results");
  const version = arg("--renovate-version");
  if (!results || !version) {
    console.error("usage: assert.mjs --results <dir> --renovate-version <x.y.z> [--self-check]");
    process.exit(2);
  }
  const selfCheck = process.argv.includes("--self-check");
  const expectations = loadExpectations();
  let failed = false;
  for (const [name, expected] of Object.entries(expectations)) {
    const runs = {
      extract: loadRun(join(results, name, "extract")),
      lookup: expected.lookup ? loadRun(join(results, name, "lookup")) : null,
    };
    if (selfCheck) {
      for (const [label, wrong] of mutations(expected)) {
        if (verifyFixture(wrong, runs, version).length === 0) {
          console.error(`${name}: the check passed with ${label}`);
          failed = true;
        }
      }
      continue;
    }
    const problems = verifyFixture(expected, runs, version);
    for (const p of problems) console.error(`${name}: ${p}`);
    if (problems.length > 0) failed = true;
    else console.log(`${name}: ok`);
  }
  if (selfCheck && !failed) console.log("self-check: every deliberately wrong expectation failed the check");
  process.exit(failed ? 1 : 0);
}
