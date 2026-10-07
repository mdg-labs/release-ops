## 🔍 Verification — {{PASS|FAIL}}

**Attempt:** {{N}} of {{MAX}}
**Reviewed commit:** `{{SHA}}` — `{{subject line}}` (scratch workspace, not yet landed on `dev`)
**Closes on main via:** `fixes #{{ISSUE_NUMBER}}` — `done` follows when the commit reaches `main`
{{IF SECURITY_SENSITIVE:}}**Security-sensitive:** yes — full security and data-safety review; the task's test confirmed to fail before and pass after the change{{END IF}}

| Layer | Result (❌ = blocking finding, ⚠️ = notes only) |
|---|---|
| Correctness / checks | {{✅ ⚠️ or ❌}} — {{one line}} |
| Scope and commit format | {{✅ ⚠️ or ❌}} — {{one line}} |
| Spec conformance | {{✅ ⚠️ or ❌}} — {{one line}} |
| Security | {{✅ ⚠️ or ❌}} — {{one line}} |
| Data safety | {{✅ ⚠️ ❌ or ➖ not applicable}} — {{one line}} |
| Best practice / obvious bugs | {{✅ ⚠️ or ❌}} — {{one line}} |
| Reachability (production caller for everything new) | {{✅ ❌ or ➖ nothing new to reach}} — {{one line: the entry point traced}} |

### Checks run
{{the exact commands executed and their outcome, and any applicable check that
could not run on this machine, with why}}

{{ — only on a fix round, otherwise omit this whole section: }}
### Previous blocking findings
1. **{{short title}}** — {{closed ✅ | still open ❌}} — {{the evidence}}

{{ — only if FAIL, otherwise omit this whole section: }}
### Blocking findings — must be closed before the next attempt
1. **{{short title}}** — `{{file:line}}` — {{what's wrong, the concrete
   scenario that shows it, and what closing it requires}}

{{ — only if there are any, otherwise omit this whole section: }}
### Notes — non-blocking, no action required
- {{one line each; never filed as tasks, never required of a fix round}}

{{ — only if there are any, otherwise omit this whole section: }}
### Findings outside this task
Real defects with a concrete scenario, outside this task's scope — the
orchestrator files or routes each one.
1. **{{short title}}** — `{{file:line}}` or {{the command that shows it}} —
   {{what's wrong, the scenario, and why it isn't in this task's scope}}

---
*Verified by `task-verifier` via the `orchestrate` skill. This comment does not
move the task to `done` — only the `fixes #N` commit reaching `main` does that.*
