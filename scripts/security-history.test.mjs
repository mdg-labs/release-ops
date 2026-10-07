import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

const script = join(import.meta.dirname, "security-history.sh");

function git(cwd, ...args) {
  const r = spawnSync("git", args, { cwd, encoding: "utf8" });
  if (r.status !== 0) {
    throw new Error(r.stderr || r.stdout || "git failed");
  }
  return r.stdout;
}

function commit(cwd, n, message) {
  writeFileSync(join(cwd, "f.txt"), `${n}\n`);
  git(cwd, "add", "f.txt");
  git(cwd, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", message);
}

test("matches the subject and the Refs trailer, not a passing body mention", () => {
  const dir = mkdtempSync(join(tmpdir(), "security-history-"));
  try {
    git(dir, "init", "-q");
    commit(dir, 1, "fix(deps)[#1]: bump x to 1.2.3 (CVE-2026-1234)");
    commit(dir, 2, "feat(api)[#2]: add widget\n\nNo security impact.");
    commit(dir, 3, "fix(api): tighten checks\n\nRefs: GHSA-abcd-efgh-ijkl");
    commit(dir, 4, "docs: note advisory\n\nSee GHSA-abcd-efgh-ijkl for context.");
    commit(dir, 5, "fix(api): Harden token comparison");
    commit(dir, 6, "chore: add securityContext to chart");

    const r = spawnSync("bash", [script, "f.txt"], { cwd: dir, encoding: "utf8" });
    assert.equal(r.status, 0, r.stderr);
    const subjects = r.stdout
      .trim()
      .split("\n")
      .map((l) => l.slice(l.indexOf(" ") + 1));
    assert.deepEqual(subjects, [
      "fix(api): Harden token comparison",
      "fix(api): tighten checks",
      "fix(deps)[#1]: bump x to 1.2.3 (CVE-2026-1234)",
    ]);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
