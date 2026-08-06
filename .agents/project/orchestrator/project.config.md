# Project config — Release Ops

> Supporting file — created by project-setup (layer **phasical**). Lives under `.agents/project/` — **not** inside `.agents/skills/` (`npx skills update` wipes skill directories).

## Repository

| Field | Value |
| ----- | ----- |
| Project name | Release Ops |
| Repo path | `/home/mdguggenbichler/projects/release-ops` |
| GitHub repo | `mdg-labs/release-ops` |
| Integration branch | `dev` |
| Production branch | `main` — agents must not push here |
| Task branch (Lane P) | `orchestrator/<TASK-ID>` |
| Worktree (Lane P) | `../release-ops-wt/<TASK-ID>` |
| Plan file | `docs/roadmap.html` |
| Spec doc glob | `docs/specs.html`, `db/schema.sql`, `docs/stack.html` |

## Phasical

| Field | Value |
| ----- | ----- |
| MCP server | `user-phasical` |
| Workspace | MDG-Labs (`X3VbytvC7pKgazK2dAsOQIFtdGYRzdGH`) |
| Project | Release Ops (`tv679ggt5ier9r5dx70w8ks6`) |
| Ready status slug | `ready` |
| GitHub MCP (read) | `user-github` |

**Commits:** use GitHub `[#N]` from `externalLinks.externalId`. Never Phasical task IDs in git.

## Domain labels

| Label | Scope |
| ----- | ----- |
| `backend` | Go API, polling, providers (same container as Next.js) |
| `web` | Next.js UI, COSS, Go API proxy |
| `db` | SQLite schema, migrations (`app.db` — app + auth) |
| `config` | App settings in DB, env bootstrap only |
| `ci` | GitHub Actions, Docker, GHCR |
| `docs` | Spec and roadmap HTML docs |

## Area prefixes (titles)

- `E01`–`E11` — consolidated roadmap epics (one orchestrator session per epic)
- `G1` / `G2` — Grundlagen modules (legacy)
- `EL-` — epic-level orchestrator batches (when used)

## Commit conventions

- Phasical/GitHub tasks: `[#N]` in subject
- Roadmap-only (no Phasical mirror): `[E*-*]` or `[G*]` in subject
- Body: `fixes #N` when project rules require it (see `.cursor/rules/`)

## Optional

| Field | Value |
| ----- | ----- |
| Multi-repo workspace | none |
| Slack session-end | not configured |
| Phase gates | see `doc-index.md` § Phase gates |
