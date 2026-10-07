---
description: {PROJECT_NAME} project identity, doc precedence, and current-phase awareness
---

# {PROJECT_NAME}

{PROJECT_TAGLINE}

## Tech stack

{TECH_STACK_BULLETS}

## Repository state

{REPO_STATE_NOTE}

## Spec doc precedence (when docs conflict)

{DOC_PRECEDENCE_LIST}

## Hard rules

- If behaviour is not defined in a spec doc, **ask before guessing**.
- Never invent fields, endpoints, or IDs not in the spec.
- Player/user-facing strings use i18n keys when the project uses i18n — no hardcoded copy in UI code.

## Agent config

- Orchestrator: `.claude/skills/orchestrator/SKILL.md` (from `mdg-labs/skills`)
- Sub-agent monitoring: `.claude/skills/orchestrator/references/sub-agent-monitoring.md` — mandatory when dispatching sub-agents via the Agent tool
- Project constants: `.agents/project/orchestrator/project.config.md`
- Plan file: `{PLAN_FILE}`
