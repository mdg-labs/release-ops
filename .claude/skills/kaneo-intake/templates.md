# Kaneo intake description templates

Use in **Phase 1** to draft descriptions for the written proposal; after approval, put the filled template into the `description` of `mcp__Kaneo__create_task` / `update_task`. Replace every `<...>` slot. Reference tasks by RO key (`RO-<n>`); link with the Kaneo task URL:

```text
https://cloud.kaneo.app/dashboard/workspace/ffM0nW62CAq0BeWqTEQuoqsDySEVyxxj/project/z4janvyjsbbb0esishvd9gb8/task/<taskCuid>
```

Titles are set separately — follow [summary-patterns.md](../kaneo-triage/summary-patterns.md).

## Parent task

```markdown
## Epic: <Feature name>

**Background:** <current behaviour and why it changes>. Related: RO-<n> (implemented).

**Roadmap ID:** <E*> (when mirroring docs/roadmap.json)

---

### Subtasks

| Task | Label | Scope |
| ---- | ----- | ----- |
| RO-<n1> | backend | <one line> |
| RO-<n2> | web | <one line> |

---

### Goal

<One paragraph: what a Release Ops user can do when this epic is implemented.>

---

### Product rules (epic-level)

- <rule, citing docs/specs.html §x.y>

---

### Suggested implementation order

1. RO-<n1> (+ RO-<n2> in parallel if independent)
2. RO-<n3>

---

See child tasks for acceptance criteria, files and tests.
```

## Leaf task

```markdown
## <Title>

**Parent:** RO-<n> — <epic title>

**Depends on:** RO-<n> — <reason> | —

**Blocks:** RO-<n> — <reason> | —

**Roadmap ID:** <E*-*> (when mirroring docs/roadmap.json)

**Spec:** docs/specs.html §<x.y> (db/schema.sql for schema; docs/stack.html for tooling)

---

### Acceptance criteria

- [ ] <concrete, testable outcome>
- [ ] <schema work only> Schema change in `db/schema.sql`; migration generated with `make migrate-diff name=<change>` (no hand-written `migrations/*.sql`); `npm run db:check` passes
- [ ] <web work only> All new UI strings via next-intl keys in `apps/web/messages/en.json` (no literals)
- [ ] `go test ./...` and `golangci-lint run` pass (Go changes)
- [ ] `npm test` and `npm run lint` pass
- [ ] `docs/specs.html` updated if the contract changed

---

### Files

- `internal/<pkg>/<file>.go` — <role>
- `apps/web/<path>` — <role>
- `db/schema.sql` — <table/column>

---

### Tests

- `internal/<pkg>/<file>_test.go` — <scenario>
- `apps/web/<path>.test.ts` — <scenario>

---

### Implementation notes

- <non-obvious constraints for the execution agent>
```

Keep only the gate lines that apply to the touched area: Go (`cmd/`, `internal/`) → `go test ./...` + `golangci-lint run`; web/Node → `npm test` + `npm run lint`; `db/schema.sql` / `migrations/` → `make migrate-diff` + `npm run db:check`. Docs-only tasks may drop the gates.

## Bug task (small, no investigation needed)

For bugs that need code investigation use `/kaneo-triage` and its [description-template.md](../kaneo-triage/description-template.md).

```markdown
## Report

<reporter text — verbatim>

## Classification

<backend / web / db / config / ci / docs / cross-cutting>

## Acceptance criteria

- [ ] <expected fixed behaviour>
- [ ] Regression test added (`go test ./...` or `npm test`)
- [ ] `go test ./...`, `golangci-lint run`, `npm test`, `npm run lint` pass

## Key files

- `<path>` — <suspected area>
```

## Dependency rules

Every `Depends on:` line uses real RO keys once the tasks exist:

| Forbidden | Required |
| --------- | -------- |
| `Depends on: E03-02` | `Depends on: RO-42 (E03-02)` |
| `Depends on: —` when deps exist | `Depends on: RO-38, RO-42` |
| `Depends on: <cuid>` | `Depends on: RO-42` |

Also wire `create_task_relation` with `relationType: blocks` (source = blocker, target = blocked) so dependencies are machine-readable.
