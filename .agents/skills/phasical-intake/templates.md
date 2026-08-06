# Phasical description templates

Use during **Phase 1** to draft description outlines in the written proposal. After approval, copy filled templates into MCP `description` fields. Replace `{placeholders}`. Use GitHub issue URLs once sync completes: `https://github.com/{owner}/{repo}/issues/{N}`.

**Titles** are set on `create_task` / `update_task` separately — follow [summary-patterns.md](../phasical-triage/summary-patterns.md).

GitHub repo and owner come from `.agents/project/orchestrator/project.config.md`.

## Parent task template

```markdown
## Epic: {Feature name}

**Background:** {Current behaviour + why we're changing it}. {Link to related Done issues, e.g. #12 ✅}.

---

### Subtasks

| Issue | Domain | Description |
| ----- | ------ | ----------- |
| [#XX](https://github.com/{owner}/{repo}/issues/XX) | Backend | {one line} |
| [#YY](https://github.com/{owner}/{repo}/issues/YY) | Frontend | {one line} |

---

### Goal

{One paragraph: what users can do when this epic is done.}

---

### Product rules (epic-level)

- {Rule 1}
- {Rule 2}

---

### Suggested implementation order

1. **#XX** + **#YY** (parallel)
2. **#ZZ**

---

See child task descriptions for acceptance criteria, file paths, and tests.
```

## Leaf subtask template

```markdown
## {Title}

**Parent:** [#XX {Epic title}](https://github.com/{owner}/{repo}/issues/XX)

**Depends on:** [#YY](https://github.com/{owner}/{repo}/issues/YY) — {reason}

**Blocks:** [#ZZ](https://github.com/{owner}/{repo}/issues/ZZ) — {reason}

**Roadmap ID:** P*-* (when mirroring plan file)

---

### Acceptance criteria

- [ ] {Concrete, testable outcome}
- [ ] {Migration via project CLI only — no hand-written SQL}
- [ ] Update spec doc if contract changed

---

### Files

- `{path/to/file}` — {role}

---

### Tests

- {test command or scenario}

---

### Implementation notes

- {Non-obvious constraints for execution agent}
```

## Bug template

```markdown
## Report

{Reporter text — verbatim}

## Classification

{FE / BE / infra / cross-cutting}

## Acceptance criteria

- [ ] {Expected fixed behaviour}
- [ ] Regression test or manual repro steps documented

## Key files

- `{path}` — {suspected area}
```

## Dependency rules (GitHub intake pattern)

Every leaf **Depends on:** line must use real GitHub issue numbers once created:

| Forbidden | Required |
| --------- | -------- |
| `Depends on: P4.4` | `Depends on: #42 (P4.4)` |
| `Depends on: —` when deps exist | `Depends on: #38, #42` |

Also wire `create_task_relation` with `relationType: blocks` for machine-readable deps.
