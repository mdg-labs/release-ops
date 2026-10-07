# Release Ops

Self-hosted release monitor: polls GitHub, GitLab, Gitea, Forgejo and Codeberg for new releases and creates tickets in configured integrations. Single Docker container — Go API + poller (`cmd/server`, `internal/`) and Next.js + COSS UI (`apps/web`). Sibling repo: `../release-ops-cloud` (control plane, landing page, billing, provisioning).

## Where things live

| What | Path |
| ---- | ---- |
| Project rules (auto-loaded; `paths:` frontmatter = file-scoped) | `.claude/rules/*.md` |
| Skills (`/orchestrator`, `/kaneo-intake`, `/kaneo-triage`, `/dependabot-triage`, `/customer-docs`, `/coss`, …) | `.claude/skills/` — installed by `npx skills` from `mdg-labs/skills` / `cosscom/coss`; do not hand-edit, change upstream. **Exception:** `orchestrator`, `dependabot-triage`, `customer-docs`, `kaneo-intake`, `kaneo-triage` are locally migrated Phasical → Kaneo until upstreamed — don't run `npx skills update` on them |
| Project sub-agents | `.claude/agents/*.md` |
| Orchestrator config (project constants, doc index, prompt templates) | `.agents/project/orchestrator/` |
| Workspace notes / session memory (gitignored) | `.agents/project/workspace-notes.md`, `.agents/project/agent-memory/` |
| Rule + skill manifests | `rules-manifest.json`, `skills-lock.json` |

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

- `Kaneo` — Kaneo board (source of truth for tasks; syncs to GitHub issues). Workflow: backlog → ready → in-progress → in-review → implemented (agents stop here) → done (GitHub, when the `fixes #N` commit lands on `main`)
- `github` — GitHub read access
- Slack — only for the optional orchestrator session-end DM
