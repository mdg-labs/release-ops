# Task title patterns (Kaneo, Release Ops)

Single source of truth for RO task titles. Both `/kaneo-triage` and `/kaneo-intake` follow this table.

## Pattern table

| Work type | Pattern | Examples |
| --------- | ------- | -------- |
| **Bug** | `<Area>: <observed defect>` | `Polling: Gitea pre-releases create duplicate tickets` |
| **Task** | `<Verb> <target>` | `Replace legacy ticket integration with Kaneo` |
| **Story** | `<User-visible outcome>` | `Show last poll time per watched repository` |
| **Epic / parent** | `<Feature name>` — no `(epic)` suffix | `Kaneo ticket integration` |

## Area prefixes

Use for Bug titles, and for other titles when a prefix clarifies scope:

`API` · `Auth` · `Polling` · `Providers` · `Tickets` · `DB` · `Config` · `UI` · `i18n` · `CI` · `Docker` · `Docs`

The prefix must agree with the domain label (`backend`, `web`, `db`, `config`, `ci`, `docs`) — e.g. `UI:` → `web`, `Polling:` → `backend`, `DB:` → `db`.

## Length

≤ 80 characters where possible.

## Rewrite vs keep

| Situation | Action |
| --------- | ------ |
| Vague placeholder ("bug", "fix polling") | Rewrite using the pattern for the work type |
| Typo, wrong area prefix or mis-scoped | Rewrite |
| Already matches the pattern and is accurate | Keep |
| User asked for a specific title | Use the user's wording |

## When to set the title

| Skill | When |
| ----- | ---- |
| `/kaneo-triage` | After investigation, when a rewrite rule applies — in the same `update_task` as the description |
| `/kaneo-intake` | On `create_task`; on enrich when the draft title is vague |
