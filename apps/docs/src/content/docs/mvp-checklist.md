---
title: "MVP checklist"
description: "Sign-off checklist for all 15 MVP acceptance criteria."
---

# MVP acceptance checklist

Sign-off checklist for Release Ops MVP. Each item maps to [specs.html §12 MVP Acceptance Criteria](/spec/#mvp). Use this document for release validation and operator handoff.

**Spec references:** [§12 MVP](/spec/#mvp) · [§9 Environment](/spec/#env) · [§10 Deployment](/spec/#deployment) · [§11 CI/CD](/spec/#ci)

**Operator guide:** [getting-started.md](/getting-started/)

---

## AC #1 — Admin login

- [ ] Admin can log in with email and password at `/login`
- [ ] Invalid credentials return an error; no public sign-up or registration endpoint
- [ ] Session cookie (`release_ops_session`) is set; protected pages redirect when logged out

**How to verify:** Sign in with bootstrap or `seed-admin` credentials; confirm Dashboard loads.

---

## AC #2 — Integrations (all eight kinds)

- [ ] Admin can create integrations for every `kind`: `github`, `gitlab`, `gitea`, `forgejo`, `codeberg`, `phasical`, `jira`, `linear`
- [ ] API list/detail responses include `hasSecret: true` but **never** the secret value
- [ ] Base URL required where specified (GitLab, Gitea, Forgejo, Phasical, Jira)

**How to verify:** **Integrations** page — create one of each kind; inspect API responses via browser devtools.

---

## AC #3 — Ticket projects

- [ ] Multiple ticket projects per ticket integration (Phasical project, Jira key, Linear team)
- [ ] Each project has its own `status_mapping` (open / done / cancelled / superseded)
- [ ] Each project has `on_open_ticket_policy`: `supersede` (default), `merge`, or `skip_if_open`

**How to verify:** **Ticket Projects** — create two projects under the same integration with different mappings.

---

## AC #4 — Monitored repos

- [ ] Admin can add a repo with any valid source + ticket-project combination
- [ ] Examples: Codeberg + Phasical, Forgejo + Jira, Gitea + Linear, GitHub + any ticket provider
- [ ] Source integration required for GitLab, Gitea, Forgejo; optional for GitHub and Codeberg

**How to verify:** **Repos** — add repos covering at least two source/ticket pairs.

---

## AC #5 — Integration connectivity test

- [ ] **Test connection** on each integration returns success or failure in the UI (toast/feedback)
- [ ] Backend calls provider-specific probe endpoints per [specs.html §6](/spec/#providers)

**How to verify:** **Integrations** — run test on a configured integration (valid and invalid credentials).

---

## AC #6 — First poll: baseline

- [ ] First poll on a new repo stores `last_known_tag` and logs a `baseline` event
- [ ] **No ticket** is created on baseline

**How to verify:** Add a new repo, run manual poll, confirm no ticket in target system and Dashboard shows tag recorded.

---

## AC #7 — Poll without release change: skip

- [ ] When fetched tag equals `last_known_tag`, poll logs `skip` and takes no ticket action

**How to verify:** Run two consecutive polls with no new upstream release; second run shows skip.

---

## AC #8 — New release: create ticket

- [ ] When tag changes and no open ticket (or ticket is done/cancelled), a ticket is created in the target system
- [ ] Title format: `Release: {source_kind} {project_path} {tag}`

**How to verify:** Simulate or wait for a new release tag; confirm ticket appears in Phasical/Jira/Linear.

---

## AC #9 — Supersede policy

- [ ] With `on_open_ticket_policy: supersede`, a new release while a ticket is open:
  - Moves old ticket to superseded status + comment
  - Creates a new ticket
  - Increments `tickets_superseded` on the poll run

**How to verify:** Configure supersede policy; trigger second release while first ticket is still open.

---

## AC #10 — Merge and skip_if_open policies

- [ ] **merge** — updates existing ticket title/description/tag; no second ticket (`merge` event)
- [ ] **skip_if_open** — no new ticket when one is open (`skip_open` event)

**How to verify:** Test each policy on a dedicated ticket project with a controlled release sequence.

---

## AC #11 — Per-project status mapping

- [ ] Status classification uses the linked `ticket_projects.status_mapping`, not a global default
- [ ] Different Phasical projects / Jira keys / Linear teams can use different open/done values

**How to verify:** Two ticket projects with different `open` arrays; confirm live status checks respect each mapping.

---

## AC #12 — Shoutrrr notifications

- [ ] Admin can create notification targets with Shoutrrr URL (encrypted at rest)
- [ ] Events configurable: `create`, `error`, `supersede` (default set)
- [ ] **Test notification** sends a probe message
- [ ] Notifications fire on matching poll events

**How to verify:** **Notifications** — add a target (e.g. `ntfy://` or `slack://`), test, then trigger a create/supersede/error.

---

## AC #13 — Provider mock tests in CI

End-to-end tests per MVP source and ticket provider run in CI via `go test ./...` (mock HTTP servers — no live API calls).

### Source providers (`internal/providers/source/`)

| Provider | Test file | CI job |
|----------|-----------|--------|
| GitHub | `github_test.go` | Go test |
| GitLab | `gitlab_test.go` | Go test |
| Gitea | `gitea_test.go`, `gitea_compatible_test.go` | Go test |
| Forgejo | `forgejo_test.go` | Go test |
| Codeberg | `codeberg_test.go` | Go test |

### Ticket providers (`internal/providers/ticket/`)

| Provider | Test file | CI job |
|----------|-----------|--------|
| Phasical | `phasical_test.go` | Go test |
| Jira | `jira_test.go` | Go test |
| Linear | `linear_test.go` | Go test |

**How to verify locally:**

```bash
go test ./internal/providers/...
```

**How to verify in CI:** `.github/workflows/ci.yml` job `go-test` runs `go test ./...` on every PR and push to `dev` / `main`.

---

## AC #14 — Dashboard, manual poll, settings, single-service Compose

- [ ] Dashboard shows last poll run, repo status table, and errors
- [ ] **Run poll now** triggers manual poll (`POST /api/v1/poll/trigger`)
- [ ] **Settings** saves global `poll_interval_minutes` (min 5)
- [ ] `docker compose up` runs **one** service (`release-ops`) with volume `release-ops-data:/data`

**How to verify:** Follow [getting-started.md](/getting-started/); confirm `docker compose config` shows a single service.

---

## AC #15 — GitHub Actions workflows

CI/CD layout per [specs.html §11](/spec/#ci). All entrypoints call reusable workflows; reusable workflows have no direct `on: push`.

### Workflow files

| File | Type | Trigger / role |
|------|------|----------------|
| `pr.yml` | Entrypoint | `pull_request` → `ci.yml` |
| `dev.yml` | Entrypoint | Push `dev` → `ci.yml` → `build-and-push-image.yml` (`nightly`, `{short_sha}`) |
| `main.yml` | Entrypoint | Push `main` → `ci.yml` → `prepare-release.yml` (draft release on `VERSION` bump) |
| `release.yml` | Entrypoint | `release: published` → `build-and-push-image.yml` (`latest`, `v{X.Y.Z}`) |
| `ci.yml` | Reusable | Lint, test, typecheck, build, `db:check` (Go + web parallel jobs) |
| `prepare-release.yml` | Reusable | `scripts/ci/create-draft-release.mjs` on semver `VERSION` increase |
| `build-and-push-image.yml` | Reusable | Single image `ghcr.io/{owner}/release-ops` → GHCR |

### CI gate jobs (reusable `ci.yml`)

| Job | Command |
|-----|---------|
| Go lint | `golangci-lint run` |
| Go test | `go test ./...` |
| Go vet | `go vet ./...` |
| Go build | `go build -o /dev/null ./cmd/server` |
| DB check | `npm run db:check` |
| Web lint | `npm run lint` (includes i18n) |
| Web test | `npm test` |
| Web typecheck | `npm run typecheck` |
| Web build | `npm run build -w apps/web` |

**How to verify:**

- [ ] PR and push to `dev` / `main` block on CI failure
- [ ] `dev` push produces GHCR image tags `nightly` + 7-char SHA after green CI
- [ ] `main` push with increased root `VERSION` creates draft release + `v*` tag; no bump → no release
- [ ] Published GitHub release pushes image with `latest` + version tag
- [ ] No image build on `main` push alone (production images only on `release: published`)

---

## Sign-off

| Role | Name | Date | Notes |
|------|------|------|-------|
| Operator / QA | | | |
| Engineering | | | |

When all boxes are checked, MVP is ready for production self-hosting.
