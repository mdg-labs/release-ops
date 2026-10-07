# Release Ops

Self-hosted release monitor: polls GitHub, GitLab, Gitea, Forgejo and Codeberg for new releases and creates tickets in configured integrations. Single Docker container — Go API + poller (`cmd/server`, `internal/`) and Next.js + COSS UI (`apps/web`). Sibling repo: `../release-ops-cloud` (control plane, landing page, billing, provisioning).

## Where things live

| What | Path |
| ---- | ---- |
| Project rules (auto-loaded; `paths:` frontmatter = file-scoped) | `.claude/rules/*.md` |
| Skills (`/orchestrator`, `/kaneo-intake`, `/kaneo-triage`, `/dependabot-triage`, `/customer-docs`, `/coss`) | `.claude/skills/` — project-owned, edit in place. Only `coss` and `coss-particles` are third-party (`cosscom/coss`, pinned in `skills-lock.json`); don't edit those |
| Project sub-agents | `.claude/agents/*.md` |
| Orchestrator config (Kaneo IDs + lookup rules, doc index, prompt templates) | `.agents/project/orchestrator/` |
| Workspace notes (tracked) / session memory (gitignored) | `.agents/project/workspace-notes.md` / `.agents/project/agent-memory/` |
| Roadmap plan file (generated from `docs/roadmap.json`) | `docs/roadmap.html` — `docs/index.html` is the doc hub |
| Third-party skill lock (coss only) | `skills-lock.json` |

Start with `.claude/rules/00-project.md` (identity, doc precedence, hard rules) and `.claude/rules/01-git-workflow.md`.

## Commands

```bash
npm test && npm run lint && npm run db:check   # scoped gate before every task commit
npm run typecheck                              # + full gate before push (only when asked to push)
go test ./... && golangci-lint run             # Go
make migrate-diff name=<change>                # schema change — edit db/schema.sql first; never hand-write migrations/
```

## MCP servers

The harness expects these Claude Code MCP server names (tool prefix `mcp__<name>__`):

- `Kaneo` — Kaneo board, project Release Ops (`RO-<n>`); source of truth for tasks and status. Agents never set `done`: it comes from the Kaneo ↔ GitHub sync when a `fixes #N` commit lands on `main` (or the user sets it).
- `github` — read-only for issues (resolve the `#N` for commit subjects). Agents never comment on, label, close or create GitHub issues via API. Task commits carry `[#N]` in the subject and a mandatory `fixes #N` trailer in the body — the only way an issue closes (`07-commit-linking.md`).
