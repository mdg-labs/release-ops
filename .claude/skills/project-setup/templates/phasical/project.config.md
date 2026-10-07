# Project config — {PROJECT_NAME}

> Supporting file — created by project-setup (layer **phasical**). Lives under `.agents/project/` — **not** inside `.claude/skills/` (`npx skills update` wipes skill directories).

## Repository

| Field | Value |
| ----- | ----- |
| Project name | {PROJECT_NAME} |
| Repo path | {REPO_PATH} |
| GitHub repo | `{GITHUB_OWNER}/{GITHUB_REPO}` |
| Integration branch | `{INTEGRATION_BRANCH}` |
| Production branch | `{PRODUCTION_BRANCH}` — agents must not push here |
| Task branch (Lane P) | `orchestrator/<TASK-ID>` |
| Worktree (Lane P) | Managed by Claude Code (Agent `isolation: "worktree"` → `.claude/worktrees/…`); agent switches to the task branch |
| Plan file | `{PLAN_FILE}` |
| Spec doc glob | `{SPEC_GLOB}` |

## Phasical

| Field | Value |
| ----- | ----- |
| MCP server | `phasical` — Claude Code tools `mcp__phasical__<tool>` (register with `claude mcp add phasical …`; name must match) |
| Workspace | {WORKSPACE_NAME} (`{WORKSPACE_ID}`) |
| Project | {PHASICAL_PROJECT_NAME} (`{PROJECT_ID}`) |
| Ready status slug | `to-do` |
| GitHub MCP (read) | `github` — tools `mcp__github__<tool>` |

**Commits:** use GitHub `[#N]` from `externalLinks.externalId`. Never Phasical task IDs in git.

## Domain labels

| Label | Scope |
| ----- | ----- |
| `{label}` | {scope description} |

## Area prefixes (titles)

{AREA_PREFIX_LIST}

## Commit conventions

- Phasical/GitHub tasks: `[#N]` in subject
- Roadmap-only (no Phasical mirror): `[P*-*]` in subject
- Body: `fixes #N` when project rules require it (see `.claude/rules/`)

## Optional

| Field | Value |
| ----- | ----- |
| Multi-repo workspace | {WORKSPACE_FILE or none} |
| Slack session-end | see `slack-session-end.md` |
| Phase gates | see `doc-index.md` § Phase gates |
