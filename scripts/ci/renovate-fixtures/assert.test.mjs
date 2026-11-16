import assert from "node:assert/strict";
import { test } from "node:test";

import { loadExpectations, mutations, verifyFixture } from "./assert.mjs";

const VERSION = "44.239.1";
const expectations = loadExpectations();

const logLines = (version = VERSION) =>
  [
    { level: 30, msg: "Repository started", renovateVersion: version },
    { level: 30, msg: "Extracted dependencies" },
    { level: 30, msg: "Repository finished", result: "done" },
  ]
    .map((r) => JSON.stringify(r))
    .join("\n");

/** What Renovate's report holds for a fixture, built from its expectations. */
function runFor(expected, phase, { exitCode = "0", log = logLines(), mutate = (r) => r } = {}) {
  const deps = expected[phase].map((want) => {
    const dep = { depName: want.depName, currentValue: want.currentValue, datasource: want.datasource ?? "npm" };
    if (want.skipReason) dep.skipReason = want.skipReason;
    if (phase === "extract") return dep;
    const source = expected.extract.find((e) => e.depName === want.depName);
    return { ...dep, ...source, currentVersion: want.currentVersion, updates: want.updates.map((u) => ({ ...u, bucket: "x" })) };
  });
  const report = {
    problems: [],
    repositories: {
      local: {
        problems: [],
        branches: [],
        packageFiles: { [expected.manager]: [{ packageFile: expected.packageFile, deps }] },
      },
    },
  };
  return { report: JSON.stringify(mutate(report)), log, exitCode };
}

function runsFor(expected, options) {
  return {
    extract: runFor(expected, "extract", options),
    lookup: expected.lookup ? runFor(expected, "lookup", options) : null,
  };
}

const first = (report) => Object.values(report.repositories)[0].packageFiles;

test("reports that match the expectations pass", () => {
  for (const [name, expected] of Object.entries(expectations)) {
    assert.deepEqual(verifyFixture(expected, runsFor(expected), VERSION), [], name);
  }
});

test("every deliberately wrong expectation fails against a matching report", () => {
  for (const [name, expected] of Object.entries(expectations)) {
    for (const [label, wrong] of mutations(expected)) {
      assert.notDeepEqual(verifyFixture(wrong, runsFor(expected), VERSION), [], `${name}: ${label}`);
    }
  }
});

test("a changed report fails against the real expectations", () => {
  const expected = expectations.npm;
  const change = (mutate) => verifyFixture(expected, { ...runsFor(expected), lookup: runFor(expected, "lookup", { mutate }) }, VERSION);
  assert.notDeepEqual(
    change((r) => {
      first(r).npm[0].deps[0].updates[0].updateType = "major";
      return r;
    }),
    [],
  );
  assert.notDeepEqual(
    change((r) => {
      first(r).npm[0].deps[0].updates = [];
      return r;
    }),
    [],
  );
  assert.notDeepEqual(
    change((r) => {
      first(r).npm[0].deps.push({ depName: "extra", datasource: "npm" });
      return r;
    }),
    [],
  );
});

test("a dependency with a skip reason or an ignored datasource is not unexpected", () => {
  const expected = expectations["github-actions"];
  const run = runFor(expected, "extract", {
    mutate: (r) => {
      const deps = first(r)["github-actions"][0].deps;
      deps.push({ depName: "ubuntu", datasource: "github-runners", currentValue: "latest" });
      deps.push({ depName: "local/action", datasource: "github-tags", skipReason: "local-dependency" });
      return r;
    },
  });
  assert.deepEqual(verifyFixture(expected, { extract: run, lookup: null }, VERSION), []);
});

test("a dependency skipped for a reason the fixture expects is still checked", () => {
  const expected = expectations["github-actions"];
  assert.ok(expected.allowSkipReasons?.length, "the github-actions fixture expects a skip reason");
  const check = (mutate) => verifyFixture(expected, { extract: runFor(expected, "extract", { mutate }), lookup: null }, VERSION);
  assert.deepEqual(
    check((r) => r),
    [],
  );
  assert.notDeepEqual(
    check((r) => {
      first(r)["github-actions"][0].deps.shift();
      return r;
    }),
    [],
    "a missing dependency with an allowed skip reason fails",
  );
  assert.notDeepEqual(
    check((r) => {
      first(r)["github-actions"][0].deps[0].skipReason = "local-dependency";
      return r;
    }),
    [],
    "a dependency skipped for a reason the fixture does not list is not counted, so it fails as missing",
  );
  assert.notDeepEqual(
    check((r) => {
      delete first(r)["github-actions"][0].deps[0].skipReason;
      return r;
    }),
    [],
    "a dependency without the expected skip reason fails",
  );
});

test("a nonzero exit, a missing file or another Renovate version fails", () => {
  const expected = expectations.dockerfile;
  const runs = runsFor(expected);
  assert.notDeepEqual(verifyFixture(expected, { ...runs, extract: { ...runs.extract, exitCode: "1" } }, VERSION), []);
  assert.notDeepEqual(verifyFixture(expected, { ...runs, extract: { ...runs.extract, report: null } }, VERSION), []);
  assert.notDeepEqual(verifyFixture(expected, runsFor(expected, { log: logLines("44.0.0") }), VERSION), []);
  assert.notDeepEqual(verifyFixture(expected, runsFor(expected, { log: "not json" }), VERSION), []);
});

test("a report without the branches array the worker reads fails", () => {
  const expected = expectations.dockerfile;
  const run = runFor(expected, "extract", {
    mutate: (r) => {
      delete Object.values(r.repositories)[0].branches;
      return r;
    },
  });
  assert.notDeepEqual(verifyFixture(expected, { extract: run, lookup: runFor(expected, "lookup") }, VERSION), []);
});
