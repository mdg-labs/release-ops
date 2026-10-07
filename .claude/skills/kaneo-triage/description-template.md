# Triage description template

Shape of a triaged Release Ops task (reference: **RO-108** — read it with `get_task_by_ticket_id` `{ ticketId: "RO-108" }` for a full example). Use for the `description` of `mcp__Kaneo__update_task` or `create_task`. Replace every `<...>` slot; drop optional sections that have nothing to say.

Title is set separately via the `title` field — see [summary-patterns.md](summary-patterns.md).

## Template

```markdown
## Report

<Original reporter text — verbatim, unchanged, including typos and links>

## Classification

<One paragraph: backend-only / web-only / db / config / ci / docs / cross-cutting, and what kind of work it is (defect, refactor, rename, contract gap). Name the layers touched, e.g. "Go ticket provider + DB `kind` CHECK + web UI + i18n + specs". State what is NOT needed when that matters.>

## <Analysis section named after the subject — e.g. "API compatibility", "Polling flow", "Integration create flow">

<How the affected feature works end-to-end, or a comparison table. Code by path, spec by §.>

| <Used by release-ops / step> | <Spec / external API / actual> | Result |
| --- | --- | --- |
| `<call or path>` | <what the source of truth says> | ✅ / ❌ <note> |

## Failure modes / gates

| Gate | Effect |
| --- | --- |
| <condition, constraint or hard rule, e.g. CHECK in `db/schema.sql`, `make db-migration`-only rule> | <what breaks or what it forces> |

## Suspects / work items (ranked)

1. **<Top suspect or work item>** — `<path>` (`<symbol>`); <why, what to check or change>.
2. **<Next>** — `<path>`; <why>.
3. **Out of scope:** <things deliberately excluded>.

## Open questions (need product decision before implementation)

1. **<Question>:** (a) <option>, (b) <option>, (c) <option>.

## Recommended triage

1. <Decision to take, with a recommendation>
2. <Concrete verification step>
3. <Suggested split: one task, or vertical slices via /kaneo-intake that each carry their own wiring>

## Acceptance criteria (proposed)

- [ ] <user-visible or contract outcome>
- [ ] <behaviour covered by updated/added tests>
- [ ] Schema change via `make db-migration` only; `npm run db:check` passes   <!-- schema work only -->
- [ ] Reachable via: <entry point> → <fixed behaviour>   <!-- runtime behaviour only -->
- [ ] `go test ./...`, `golangci-lint run`, `npm test`, `npm run lint`, `npm run typecheck` pass

## Key files

- `internal/<pkg>/<file>.go` — <one-line role>
- `apps/web/<path>` — <one-line role>
- `db/schema.sql` — <table / constraint>
- `docs/specs.html` §<x.y> — <spec contract>

## Out of scope

- <adjacent work> — RO-<n> | none

## Scope hint

~<N> changed lines · Expected files: <N> reviewable (`<paths>`)
```

Omit "Open questions" when there are none. Keep only the gates that apply (Go → `go test ./...` + `golangci-lint run`; web → `npm test` + `npm run lint`; schema → `npm run db:check`).

## Regression section (new task for an escaped defect)

Insert directly after `## Report`:

```markdown
## Regression

Previously fixed in RO-<n> (status: implemented | done). This report shows the fix does not cover <scenario>.
```

## Anti-patterns

- Posting findings as a **comment** instead of updating the **description**
- Paraphrasing or trimming the `## Report` text
- Ranked suspects without file paths
- Acceptance criteria without the gates
- Proposing hand-written `migrations/*.sql`
- Inventing behaviour the spec doesn't define — put it under "Open questions"
- Transitioning to `in-progress` / `in-review` / `implemented` / `done`
- Reopening an `implemented` / `done` task for a regression
