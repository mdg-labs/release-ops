# Release Ops

Self-hosted release monitor: polls GitHub, GitLab, Gitea, Forgejo and Codeberg for new releases and creates tickets in configured integrations. Single Docker container — Go API + poller (`cmd/server`, `internal/`) and Next.js + COSS UI (`apps/web`). Sibling repo: `../release-ops-cloud` (control plane, landing page, billing, provisioning).

## Where things live

| What | Path |
| ---- | ---- |
| Project rules (auto-loaded; `paths:` frontmatter = file-scoped) | `.claude/rules/*.md` |
| Skills (`/orchestrate`, `/dev-diff`, `/open-pr`, `/cr-review`, `/security-audit`, `/kaneo-intake`, `/kaneo-triage`, `/dependabot-triage`, `/customer-docs`, `/coss`) | `.claude/skills/` — project-owned, edit in place. Only `coss` and `coss-particles` are third-party (`cosscom/coss`, pinned in `skills-lock.json`); don't edit those |
| Project sub-agents | `.claude/agents/*.md` |
| Orchestrate prompt templates + known escapes | `.claude/skills/orchestrate/templates/` |
| Project constants (Kaneo IDs + lookup rules, area → paths, doc index) | `.agents/project/orchestrator/` |
| Threat model (security yardstick) | `docs/threat-model.md` |
| Workspace notes (tracked) / session memory (gitignored) | `.agents/project/workspace-notes.md` / `.agents/project/agent-memory/` |
| Historical roadmap (generated from `docs/roadmap.json`; Kaneo is the live plan) | `docs/roadmap.html` — `docs/index.html` is the doc hub |
| Third-party skill lock (coss only) | `skills-lock.json` |

Start with `.claude/rules/00-project.md` (identity, doc precedence, hard rules) and `.claude/rules/01-git-workflow.md`.

## Commands

```bash
npm test && npm run lint && npm run typecheck  # web / root JS — before every commit that touches them
npm run db:check                               # db/schema.sql, migrations/
go test ./... && golangci-lint run             # Go
.claude/skills/dev-diff/dev-diff.sh            # dev→main size as CodeRabbit counts it (cap 100)
make migrate-diff name=<change>                # schema change — edit db/schema.sql first; never hand-write migrations/
```

## MCP servers

The harness expects these Claude Code MCP server names (tool prefix `mcp__<name>__`):

- `Kaneo` — Kaneo board, project Release Ops (`RO-<n>`); source of truth for tasks and status. Through the claude.ai connector the same tools appear as `mcp__claude_ai_Kaneo__<tool>`; skills and agents use whichever the session lists. Agents never set `done`: it comes from the Kaneo ↔ GitHub sync when a `fixes #N` commit lands on `main` (or the user sets it).
- `github` — read-only for issues (resolve the `#N` for commit subjects); without it, `gh issue view` / `gh issue list` do the same reads. Agents never comment on, label, close or create GitHub issues via API. Task commits carry `[#N]` in the subject and a mandatory `fixes #N` trailer in the body — the only way an issue closes (`07-commit-linking.md`).

## Workflow

Task → `/kaneo-intake` / `/kaneo-triage` / `/dependabot-triage` (stop at `ready`) → `/orchestrate RO-<n>` (scratch-clone executor, independent verifier, land on `dev` after PASS, push `dev`) → `/open-pr` (dev→main PR) → `/cr-review <PR>` → the maintainer merges; the `fixes #N` trailers close the issues and the Kaneo ↔ GitHub sync moves tasks to `done`. `/security-audit` reviews the code against `docs/threat-model.md`.
