# Triage description template

Use when composing the `description` field for MCP `update_task` or `create_task`. Replace `{placeholders}`. Keep `## Report` at the top — verbatim reporter text.

**Title:** set separately via `update_task` `title` field per [summary-patterns.md](summary-patterns.md).

## Full template

```markdown
## Report

{Original issue description — reporter wording, unchanged}

## Classification

{One paragraph: FE-only / BE-only / worker / infra / cross-cutting.}

## {Domain flow or system name}

{How the affected feature works end-to-end — numbered steps, code references by path.}

## Failure modes / gates

| Gate        | Effect        |
| ----------- | ------------- |
| {condition} | {what breaks} |

## Suspects (ranked)

1. **{Top suspect}** — {why; what to check}
2. **{Second suspect}** — {why}

## Recommended triage

1. {Concrete operator/dev check}
2. …

## Key files

- `{path/to/file.ts}` — {one-line role}
```

## Regression section (when creating new task for escaped defect)

```markdown
## Regression

Previously fixed on #<N> (closed <date>). This report suggests the fix did not cover <scenario>.
```

## Anti-patterns

- Posting the same content as a **comment** instead of updating **description**
- Paraphrasing the reporter's `## Report` text
- Transitioning to in-progress / in-review / done during triage
- Committing code fixes without an explicit user request
- Leaving triaged backlog tasks in backlog when user did not opt out of Phasical updates
- Re-opening **done** / **closed** tasks for regressions — create a **new** task with `regression` label instead
