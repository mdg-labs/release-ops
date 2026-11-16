import assert from "node:assert/strict";
import { test } from "node:test";

import { checkResult } from "./check-result.mjs";

const meta = (over = {}) => ({
  claims: 3,
  tokenRequests: 1,
  heartbeats: 2,
  badAuth: 0,
  wrongLease: 0,
  tokenInResult: false,
  githubRequests: [{ path: "/api/v3/repos/fixture-org/fixture-repo", authorized: true }],
  ...over,
});

const realResult = (run = {}) => ({
  outcome: "failed",
  run: {
    exit_code: 1,
    report_status: "missing",
    log_excerpt: `${JSON.stringify({ name: "renovate", level: 30, msg: "Repository started" })}\n`,
    ...run,
  },
});

const deadlineResult = (run = {}) => ({
  outcome: "failed",
  run: { error_code: "timeout", error_message: "renovate exceeded the job deadline and was stopped", log_excerpt: "shim-uid=12031\n", ...run },
});

test("a run that matches its scenario passes", () => {
  assert.deepEqual(checkResult("real", { result: realResult(), meta: meta(), renovateUid: "12031" }), []);
  assert.deepEqual(checkResult("deadline", { result: deadlineResult(), meta: meta({ githubRequests: [] }), renovateUid: "12031" }), []);
});

test("a real run that never reached the GitHub endpoint or never ran to an exit fails", () => {
  assert.notDeepEqual(checkResult("real", { result: realResult(), meta: meta({ githubRequests: [] }), renovateUid: "12031" }), []);
  assert.notDeepEqual(
    checkResult("real", { result: realResult(), meta: meta({ githubRequests: [{ path: "/api/v3/user", authorized: false }] }), renovateUid: "12031" }),
    [],
  );
  assert.notDeepEqual(checkResult("real", { result: realResult({ exit_code: undefined, error_code: "timeout" }), meta: meta(), renovateUid: "12031" }), []);
  assert.notDeepEqual(checkResult("real", { result: realResult({ log_excerpt: "plain text" }), meta: meta(), renovateUid: "12031" }), []);
});

test("a deadline run that was not stopped by the deadline, or ran as another user, fails", () => {
  const m = meta({ githubRequests: [] });
  assert.notDeepEqual(checkResult("deadline", { result: deadlineResult({ error_code: "renovate_exit" }), meta: m, renovateUid: "12031" }), []);
  assert.notDeepEqual(checkResult("deadline", { result: deadlineResult({ exit_code: 0 }), meta: m, renovateUid: "12031" }), []);
  assert.notDeepEqual(checkResult("deadline", { result: deadlineResult({ log_excerpt: "shim-uid=12032\n" }), meta: m, renovateUid: "12031" }), []);
});

test("a token in the result, a wrong secret or a missing heartbeat fails", () => {
  const ok = { result: realResult(), renovateUid: "12031" };
  assert.notDeepEqual(checkResult("real", { ...ok, meta: meta({ tokenInResult: true }) }), []);
  assert.notDeepEqual(checkResult("real", { ...ok, meta: meta({ badAuth: 1 }) }), []);
  assert.notDeepEqual(checkResult("real", { ...ok, meta: meta({ heartbeats: 0 }) }), []);
});
