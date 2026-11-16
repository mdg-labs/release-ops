#!/usr/bin/env node
/**
 * Checks what the fake app saw of one worker run (see fake-app.mjs and ../worker-run-smoke.sh).
 *
 * Run: node check-result.mjs <real|deadline> <state dir> <renovate uid>
 *
 * real: the real renovate binary ran against a GitHub API that refuses it; deadline: a stand-in
 * that ignores SIGTERM ran until the job's deadline.
 */
import { readFileSync } from "node:fs";
import { join } from "node:path";

/** The problems with a run; none means it is what the scenario expects. */
export function checkResult(scenario, { result, meta, renovateUid }) {
  const problems = [];
  const run = result.run ?? {};
  if (meta.badAuth !== 0) problems.push(`${meta.badAuth} internal requests carried the wrong worker secret`);
  if (meta.wrongLease !== 0) problems.push(`${meta.wrongLease} requests named a lease the worker does not hold`);
  if (meta.tokenRequests < 1) problems.push("the worker never asked for the job's token");
  if (meta.heartbeats < 1) problems.push("the worker never sent a heartbeat");
  if (meta.tokenInResult !== false) problems.push("the job's token appears in the result the worker posted");
  if (!result.run) problems.push("the result has no run record");

  if (scenario === "real") {
    if (typeof run.exit_code !== "number") problems.push(`renovate did not run to an exit (error_code ${run.error_code ?? "none"})`);
    if (!["ok", "missing"].includes(run.report_status)) problems.push(`report_status is ${run.report_status}`);
    const logged = (run.log_excerpt ?? "").split("\n").some((line) => {
      try {
        return JSON.parse(line).name === "renovate";
      } catch {
        return false;
      }
    });
    if (!logged) problems.push("the log excerpt holds no record of Renovate's JSON log");
    const calls = meta.githubRequests.filter((r) => r.path.startsWith("/api/v3"));
    if (calls.length === 0) problems.push("renovate made no request to the GitHub API endpoint the worker gave it");
    else if (!calls.some((r) => r.authorized)) problems.push("no request to the GitHub API carried the job's token");
  } else if (scenario === "deadline") {
    if (result.outcome !== "failed") problems.push(`outcome is ${result.outcome}, expected failed`);
    if (run.error_code !== "timeout") problems.push(`error_code is ${run.error_code}, expected timeout`);
    if (run.exit_code !== undefined) problems.push(`exit_code is ${run.exit_code}, expected none for a killed process`);
    if (!(run.log_excerpt ?? "").includes(`shim-uid=${renovateUid}`)) {
      problems.push(`the process did not run as the renovate user (uid ${renovateUid})`);
    }
  } else {
    problems.push(`unknown scenario ${scenario}`);
  }
  return problems;
}

if (process.argv[1] && import.meta.url === new URL(`file://${process.argv[1]}`).href) {
  const [scenario, dir, renovateUid] = process.argv.slice(2);
  if (!scenario || !dir || !renovateUid) {
    console.error("usage: check-result.mjs <real|deadline> <state dir> <renovate uid>");
    process.exit(2);
  }
  const read = (name) => JSON.parse(readFileSync(join(dir, name), "utf8"));
  const problems = checkResult(scenario, { result: read("result.json"), meta: read("meta.json"), renovateUid });
  for (const p of problems) console.error(`worker-run-smoke (${scenario}): ${p}`);
  process.exit(problems.length > 0 ? 1 : 0);
}
