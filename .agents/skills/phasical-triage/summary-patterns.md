# Issue title patterns (Phasical)

Single source of truth for **task title** conventions. Both phasical-triage and phasical-intake must follow this table.

## Pattern table

| Work type       | Pattern                               | Examples                                               |
| --------------- | ------------------------------------- | ------------------------------------------------------ |
| **Bug**         | `{Area}: {observed defect}`           | `Sync: Coolify 401 does not mark run as failed`        |
| **Task**        | `{Verb} {target}`                     | `Add Redis lock helper for spawn scheduler`             |
| **Story**       | `{User-visible outcome}`              | `Show routed ETA in dispatch vehicle picker`           |
| **Epic/parent** | `{Feature name}` — no `(epic)` suffix | `Routed return legs`                                   |

## Area prefixes

Use for **Bug** titles and when a prefix clarifies scope. Customize per project in `project.config.md` § Area prefixes.

Examples: `API` · `Auth` · `UI` · `Worker` · `DB` · `CI` · `Infra` · `Sync` · `i18n` · `Agent skills`

Cross-check domain labels — area prefix and label should not contradict.

## Length

≤ **80 characters** where possible.

## Rewrite vs keep

| Situation                                    | Action                                           |
| -------------------------------------------- | ------------------------------------------------ |
| Vague placeholder                            | Rewrite using the pattern row for that work type |
| Typo, wrong area prefix, or mis-scoped title | Rewrite                                          |
| Already matches the pattern and is accurate  | Keep unchanged                                   |
| User explicitly asked for a specific title   | Use user wording                                 |

## When to update title

| Skill            | When                                                                                           |
| ---------------- | ---------------------------------------------------------------------------------------------- |
| **phasical-triage** | After investigation, when rewrite rules apply — include in `update_task` alongside description |
| **phasical-intake** | On `create_task`; on enrich when draft title is vague                                          |
