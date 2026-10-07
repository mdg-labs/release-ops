## Execution report — {{UNIT_ID}}

**Workspace:** `{{WORKSPACE_PATH}}`
**Tasks in this dispatch:** {{RO-<n> (#<N>), … in the order you worked them}}
**Dependencies installed:** {{"npm ci ok" | the error}}

{{FOR EACH TASK — one block per task in this dispatch, including any you had
to report blocked or already-resolved:}}

### RO-{{RO_NUMBER}} (#{{ISSUE_NUMBER}}) — {{TASK_TITLE}}

**Status:** {{done|blocked|already-resolved}}
**Kaneo:** {{in-progress ✓ / in-review ✓ — or which call failed}}
**Commit:** `{{SHA}}` — `{{subject line}}` {{or "none — blocked" / "none — already-resolved"}}
**Trailer:** {{`fixes #<N>` | `Refs: GHSA-…`}}

**Files touched:**
- {{path}}

**Summary:** {{2-5 sentences: what was implemented and how}}

**Checks run:** {{every check actually run inside WORKSPACE for this task, with
its result — and every applicable check that could NOT run on this machine,
with why (not installed)}}

**Generated output changed:** {{migrations/, internal/store/db/, package-lock.json changes and why — or "none"}}

**Security-relevant code touched:** {{each auth, token, credential, outbound-URL
or schema-data path changed, and the test covering it — or "none"}}

**Spec references:** {{spec sections implemented — or "none"}}

**Deviations from the task text:** {{where and why, including any behaviour the
spec does not define — or "none"}}

**Left undone / blocked:** {{what and why; for already-resolved, the evidence
that each criterion already holds — or "none"}}

{{END FOR}}

### Findings outside these tasks
{{only real problems none of the tasks above cover and you did not fix: a
pre-existing defect with a concrete scenario, a spec statement that is untrue,
or work a planned feature cannot do without. One line each — what, where
(`file:line` or the command that shows it), the scenario, and why it isn't
yours. Not findings: style, hardening you would like, ideas, and tooling missing
on this machine (the orchestrator already knows). The orchestrator files or
routes each one; you never create a task. "none" is the normal answer.}}
