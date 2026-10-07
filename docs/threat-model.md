# Release Ops — Threat Model

**Status: DRAFT.** Written from `docs/specs.html` (Spec v2.8), `db/schema.sql` and the code on `dev` on 2026-10-07. It needs maintainer review before anything relies on it. Statements marked "(proposed — confirm)" are inferences, not things the spec says.

This doc is the yardstick for every security judgement in the project: the `security-audit` skill, the task-verifier's security layer and `cr-review`'s security triage. A finding is measured against the assets (§1), attackers (§2), entry points (§3), invariants (§4), accepted residuals (§5) and severity rubric (§6), and recorded per §7. Code anchors are checked against `dev`; when code moves, the anchor moves in the same change.

---

## 1. Assets

| Asset | Why it matters | Where it lives |
|---|---|---|
| **Forge and ticket-system credentials** | GitHub/GitLab/Gitea/Forgejo/Codeberg tokens and Jira/Linear/Kaneo API keys. They often carry write access to the user's code hosting and issue trackers, far beyond this app. | `integrations.encrypted_payload` (AES-256-GCM, specs §4.8) |
| **Notification URLs** | Shoutrrr URLs embed webhook secrets and bot tokens (Slack, ntfy, …). | `notification_targets.shoutrrr_url_encrypted` |
| **`APP_ENCRYPTION_KEY`** | The only thing between a leaked `app.db` and every credential above. | Container environment only; never in the DB or image |
| **User accounts and sessions** | Every signed-in user has full control of the instance (no RBAC, specs §1, §3). | `users.password_hash` (bcrypt cost 12), `sessions` table, cookie `release_ops_session` |
| **One-time tokens** | Invitation, password-reset and email-change links grant an account or take one over. | `auth_tokens.token_hash` (SHA-256; raw token only in the link) |
| **Ticket integrity** | The app writes tickets into other teams' trackers; forged or spammed tickets mislead people and pollute boards. | Outbound calls in `internal/providers/ticket/`, driven by `internal/poll/` |
| **The SQLite database file** | Holds all of the above plus config; also the target of migrations. | `/data/app.db` (Docker volume `/data`) |

---

## 2. Attackers

Each attacker has a capability, what it is trusted with, and what it must never reach. A finding names the attacker it relies on.

### 2.1 Unauthenticated network client
- **Capability:** sends arbitrary HTTP to the Next.js listener (`0.0.0.0:3000`, `Dockerfile`), and through `/api/go/*` to the Go API. Can guess passwords and tokens subject to the rate limits.
- **Trusted with:** the login page, the public auth pages and the six public API paths in `internal/api/auth/public_paths.go`, plus `/healthz`.
- **Must never reach:** any other API route, any session, any credential, any signal that tells a registered email from an unknown one.

### 2.2 Signed-in user — the trust ceiling
- **Capability:** every protected route. There is no RBAC: any signed-in user can invite and remove users, create and edit integrations, ticket projects, repos, notification targets and settings, and trigger polls (specs §1, §3, §7.0.1).
- **Trusted with:** everything the API exposes. Every user is effectively an admin.
- **Must never reach:** a stored secret's plaintext (the API returns `hasSecret` only), another user's password or session, or a stored credential sent to a host other than the one it was entered for.
- **Not a finding:** anything any user can do by design, e.g. deleting another user or pointing an integration at an internal host (§5).

### 2.3 Malicious or compromised release source
- **Capability:** controls the forge API response for a monitored repo: tag names, release names, release notes (arbitrary Markdown/HTML), URLs, timestamps, response size, status codes and redirects. Includes a self-hosted GitLab/Gitea/Forgejo whose operator is hostile.
- **Trusted with:** being treated as data.
- **Must never reach:** template execution, the web UI's DOM as markup, a credential for another host, or unbounded resource use in the poller.

### 2.4 Malicious ticket system or notification endpoint
- **Capability:** controls Jira/Linear/Kaneo responses (ticket IDs, statuses, metadata lists, errors, redirects) and whatever a Shoutrrr target returns.
- **Trusted with:** being treated as data.
- **Must never reach:** the same as 2.3; in particular its IDs and URLs are not trusted as links or paths without building them server-side.

### 2.5 Holder of the SQLite file or a backup of `/data`
- **Capability:** reads `app.db` offline (stolen volume, backup, misconfigured host share). Does not have the container environment.
- **Must never reach:** any credential or notification URL in plaintext, any reusable password, session-independent login, or a usable one-time token. Note: rows in `sessions` are live session tokens until expiry (proposed — confirm whether that is accepted).

### 2.6 Network observer between browser and container
- **Capability:** sees traffic when the operator exposes port 3000 without TLS, or between the app and SMTP.
- **Must never reach:** session cookies or one-time tokens when the operator configured HTTPS (`APP_PUBLIC_URL=https://…`) and `SMTP_TLS=true`.

### 2.7 Co-tenant in the container's network namespace or on the Docker host
- **Capability:** a sidecar sharing the container's network namespace can reach the Go API on `127.0.0.1:8080` directly and is treated as the trusted loopback proxy for `X-Forwarded-For`. A host user in the `docker` group is root-equivalent and out of scope.
- **Must never reach:** a protected route without a session (the Go API enforces sessions itself, not the proxy).

### 2.8 Compromised dependency or CI step
- **Capability:** a Go module, npm package, base image (`node:22-bookworm-slim`, `golang:1.25-bookworm`) or GitHub Action (`.github/workflows/*.yml`, pinned by tag, not SHA) runs attacker code at build or run time.
- **Must never reach:** a published GHCR image the project did not build, or a runtime secret. The image carries no secrets (T13).

---

## 3. Entry points, mapped to code

| Entry point | Owner paths | Reachable by |
|---|---|---|
| **Next.js listener and pages** (`:3000`) | `apps/web/app/`, `apps/web/middleware.ts` (redirect to login — UX only, not a guard) | 2.1, 2.2, 2.6 |
| **Proxy `/api/go/*` → Go API** | `apps/web/app/api/go/[...path]/route.ts` (forwards `cookie`, `x-forwarded-for`, `Set-Cookie`) | 2.1, 2.2, 2.6 |
| **Go API listener** (`127.0.0.1:8080` only) | `internal/api/router.go` (`Serve`, `newBaseRouter`), `internal/config/config.go` (`GoListenAddr`), `cmd/server/main.go` | 2.7 (directly); everyone else via the proxy |
| **Public auth routes** — login, session, accept-invitation, forgot/reset password, confirm-email-change | `internal/api/routes.go` (`RegisterAll`), `internal/api/auth/` (`handlers.go`, `password_reset.go`, `tokens.go`, `session.go`, `session_user.go`, `session_store.go`, `public_paths.go`) | 2.1 |
| **Rate limiter** | `internal/api/middleware/ratelimit.go` | 2.1, 2.7 |
| **Session check on protected routes** | `internal/api/middleware/auth.go` (`RequireSession`), `internal/api/routes.go` (protected group) | 2.1, 2.2 |
| **Protected REST handlers** | `internal/api/handlers/` (`integrations.go`, `integration_metadata.go`, `ticket_projects.go`, `repos.go`, `notifications.go`, `settings.go`, `status.go`, `poll.go`, `users.go`, `invitations.go`) | 2.2 |
| **Login redirect** (`?redirect=`) | `apps/web/lib/auth/safe-redirect.ts`, `apps/web/app/login/login-form.tsx` | 2.1 |
| **Poller fetching releases** | `internal/poll/` (`scheduler.go`, `engine.go`, `run.go`), `internal/providers/source/` | 2.3 |
| **Ticket creation and metadata calls** | `internal/providers/ticket/` (`jira.go`, `linear.go`, `kaneo.go`, `content.go`, `metadata/`), `internal/api/handlers/status_ticket_provider.go` | 2.4 |
| **Connection tests and notifications** | `internal/providers/integrationtester/tester.go`, `internal/poll/notify.go` (Shoutrrr) | 2.2, 2.4 |
| **Ticket content templates** | `internal/tickettemplate/` (`render.go`, `context.go`, `funcs.go`) | 2.2 (template source), 2.3 (data) |
| **Outbound email** | `internal/mail/` (`smtp.go`, `templates.go`, `templates/`) | 2.6 |
| **Secrets at rest** | `internal/crypto/aesgcm.go`, `internal/store/integrations.go`, `internal/store/notifications.go` | 2.5 |
| **Startup, migrations, bootstrap** | `docker/entrypoint.sh`, `tools/migrate/` (`main.go`, `preflight.go`), `migrations/`, `db/schema.sql`, `internal/api/auth/bootstrap.go`, `cmd/seed-admin/main.go` | 2.5, 2.8 |
| **Build and release** | `Dockerfile`, `.github/workflows/` (`build-and-push-image.yml`, `release.yml`, `ci.yml`, …), `go.mod`, `package-lock.json` | 2.8 |

---

## 4. Security invariants

Findings and verifier verdicts cite these by number (`violates T8`). An invariant the code does not fully hold yet is still an invariant; the gap is listed in the Open questions.

| # | Invariant | Anchor and current guard |
|---|---|---|
| **T1** | Credentials and notification URLs are encrypted at rest with AES-256-GCM under `APP_ENCRYPTION_KEY`, random 12-byte nonce per write. | `internal/crypto/aesgcm.go` (`Encrypt`/`Decrypt`); called from `internal/store/integrations.go:53,107` and `internal/store/notifications.go:63,121` |
| **T2** | No API response carries a secret's plaintext; lists return `hasSecret`. | `internal/api/handlers/integrations.go` (list item `HasSecret`, line 43), `internal/api/handlers/notifications.go` (line 35) |
| **T3** | No secret, token or password is logged. The request log carries method, path, status, bytes and duration only; one-time tokens travel in POST bodies, not URLs logged by Go. | `internal/api/router.go` (`requestLogger`) |
| **T4** | Every `/api/v1/*` route outside `PublicAuthPaths` requires a session, enforced in Go; the Go API binds to `127.0.0.1` only, so the proxy is not a security boundary. | `internal/api/routes.go` (`RegisterAll` protected group), `internal/api/middleware/auth.go` (`RequireSession`), `internal/api/router.go` (`Serve` refuses non-127.0.0.1) |
| **T5** | The session token is renewed on every login, invitation accept, password reset, email-change confirm and password change; the cookie is `HttpOnly`, `SameSite=Lax`, and `Secure` when `APP_PUBLIC_URL` is HTTPS. | `internal/api/auth/session_user.go` (`StartUserSession` → `RenewToken`), `internal/api/handlers/users.go:250`, `internal/api/auth/session.go` (`NewSessionManager`), `internal/config/config.go` (`SecureCookies`) |
| **T6** | Sessions are revoked on credential change: password reset → all of the user's sessions; email-change confirm and password change → all others; user delete → all, before the row goes. | `internal/api/auth/password_reset.go:168`, `internal/api/auth/handlers.go:249`, `internal/api/handlers/users.go:243` and `:94`; `queries/sessions.sql` |
| **T7** | One-time tokens are random, stored only as SHA-256, checked for kind, expiry and `used_at`, and consumed with `used_at IS NULL` **before** any side effect, so only one concurrent redemption wins. | `internal/api/auth/tokens.go` (`HashToken`, `Validate`, `Consume`), `queries/auth_tokens.sql` (`MarkAuthTokenUsed`), `internal/api/auth/handlers.go:153`, `internal/api/auth/password_reset.go:136` |
| **T8** | No public response tells a registered email from an unknown one. Forgot-password always returns the same 200 and sends mail in the background. Login returns the same message for both cases, but not the same timing (gap — see Open questions). | `internal/api/auth/password_reset.go:100`; `internal/api/auth/handlers.go` (`Login`, lines 55–68) |
| **T9** | Post-login redirects are same-origin paths only. | `apps/web/lib/auth/safe-redirect.ts` (`safeRedirectPath`), used by `apps/web/app/login/login-form.tsx` |
| **T10** | A stored credential is only ever sent to the host it was entered for: changing an integration's `baseUrl` requires a new secret, and redirects must not carry a credential to another host (gap for GitLab's `PRIVATE-TOKEN` — see Open questions). Outbound calls time out (30 s) and read error bodies through a size limit. | `internal/api/handlers/integrations.go:158-163`; `internal/poll/scheduler.go:75`, `internal/providers/integrationtester/tester.go:36`, `internal/providers/ticket/metadata/metadata.go:43`; `io.LimitReader` in `internal/providers/source/*.go` |
| **T11** | Public auth mutations are rate-limited per client IP; `X-Forwarded-For` is honoured only when the peer is loopback (the bundled proxy), so a direct caller cannot choose its bucket. | `internal/api/middleware/ratelimit.go` (`clientIP`, `limit`, `sweep`), `internal/api/routes.go` (`rateLimiter.*`) |
| **T12** | No SQL is built from input: every query is a sqlc-generated statement with bound parameters. | `queries/*.sql` → `internal/store/db/*.sql.go`; no `ExecContext`/`QueryContext` outside `internal/store/db/` |
| **T13** | Secrets are never baked into the image; they arrive only as runtime env. The container runs as `node`, data lives in the `/data` volume. | `Dockerfile` (no `.env` copied, `USER node`), `.env.example` |
| **T14** | Startup refuses a missing, short or `.env.example` placeholder `SESSION_SECRET` or `APP_ENCRYPTION_KEY`; `SMTP_TLS=true` never falls back to plaintext. | `internal/config/config.go` (`validate`), `internal/mail/smtp.go:95-100` |
| **T15** | Forge and ticket content is data, never template source or markup: the user-authored ticket template is parsed with `text/template` and receives a plain-string context with no methods; the web UI renders through React escaping (no `dangerouslySetInnerHTML` in `apps/web`); link URLs are built server-side with the tag path-escaped; emails use `html/template`. | `internal/tickettemplate/render.go` (`render`), `internal/tickettemplate/context.go`, `internal/providers/source/provider.go` (`BuildReleaseWebURL`), `apps/web/components/dashboard/repo-status-table.tsx`, `internal/mail/templates.go` |
| **T16** | Schema changes are sqldiff-generated from `db/schema.sql`, never hand-written, and a migration that would fail or lose data is refused by a preflight that leaves the DB untouched. | `.claude/rules/11-db-migrations.md`, `tools/migrate/preflight.go` (`checkPhasicalIntegrations`), `docker/entrypoint.sh` (migrate before server) |
| **T17** | An admin can be bootstrapped only while `users` is empty; there is no public sign-up. | `internal/api/auth/bootstrap.go` (`createAdminIfEmpty`, `SeedAdminUser` → `ErrUsersExist`), `internal/api/routes.go` (no register route) |

---

## 5. Accepted residuals

Not findings. Items without "(proposed)" are stated in `docs/specs.html`; the rest are inferred and need confirmation.

1. **No RBAC.** Every signed-in user can invite and remove users, edit every integration, and change settings (specs §1, §3, §7.0.1). Only self-delete and last-user-delete are refused (`internal/api/handlers/users.go:71,81`).
2. **Any user can make the server call any URL** they configure — integration base URLs, Shoutrrr targets, connection tests — including private and loopback addresses. The SSRF reach equals the user's trust (proposed).
3. **The rate limiter is in memory and per container**, so it resets on restart (specs §7.0).
4. **TLS is the operator's job.** The container serves plain HTTP; cookies are `Secure` only when `APP_PUBLIC_URL` is HTTPS (specs §9).
5. **Release notes go into Kaneo/Linear tickets as Markdown.** Rendering them safely is the ticket system's job; a hostile release can put links or images into a ticket (proposed).
6. **Invitation links are shown to the inviting user** for copy-link flows (specs §3); that user could accept the invite themselves.
7. **Holder of `app.db` plus the container env has everything.** `APP_ENCRYPTION_KEY` protects the DB only when stored apart from it (proposed).

---

## 6. Severity rubric

The rating is the lowest level whose definition the finding meets after the anti-inflation rules.

| Level | Definition |
|---|---|
| **Critical** | An unauthenticated network client (2.1) gets a session, an account, or a stored credential's plaintext, or makes the server act as a signed-in user, against the production image with default config. |
| **High** | A signed-in user (2.2) or a hostile source/ticket system (2.3, 2.4) obtains a credential's plaintext or sends it to a host it was not entered for; a hostile forge response executes code, template logic or script in a user's browser; a DB-file holder (2.5) recovers a credential without the key; any break of T4, T7 or T10. |
| **Medium** | A bounded loss that needs a precondition the default setup does not give, or stops short of credentials: account enumeration, a session weakness needing an adjacent position (2.6, 2.7), rate-limit bypass, unbounded resource use by a hostile source, an invariant violation with no working path to a High outcome. |
| **Low** | Needs an already privileged or compromised position, or a defence-in-depth gap with no attack path today. |
| **Info** | No exploitable path: missing hardening, doc drift, a non-default configuration the docs already warn about. |

**Anti-inflation rules.** A finding is rated only after all of these hold:

1. **A §3 entry point**, named with its owner path, that is reachable in the production image (`Dockerfile`, `docker/entrypoint.sh`) with default config.
2. **A named §2 attacker**, using only that attacker's capability. Needs-a-signed-in-user is never Critical, and is High only when it crosses a "must never reach" line of 2.2.
3. **Dev-only paths do not count** at face value: test helpers (`*_test.go`, `internal/mail/smtp_test_helpers.go`, `NewRateLimiterForTest`), `internal/providers/source/mock.go`, local dev servers. Rate them by the precondition.
4. **§5 and "any user can do it by design" are not findings.**
5. **Theory is checked.** The report gives the exact sequence from entry point to outcome; a verifier re-derives it before it is rated above Low.

### Worked examples

- **Critical (constructed).** A new handler is registered outside the `RequireSession` group in `internal/api/routes.go` and returns integrations with decrypted payloads. Any unauthenticated client (2.1) reads every token. Violates T2 and T4.
- **High (constructed).** The poller follows a redirect from a hostile self-hosted GitLab (2.3) to an attacker host and the request carries the `PRIVATE-TOKEN` header. The token leaks to a host it was not entered for (T10). Not Critical: needs a configured integration pointing at the hostile host.
- **Medium (constructed).** A client sends its own `X-Forwarded-For` to the proxy, the header reaches Go unchanged, and the client rotates buckets to brute-force login past the limit (T11, 2.1). Bounded by bcrypt cost and password strength.
- **Low (constructed).** Login skips bcrypt for unknown emails, so response timing reveals which emails are registered (T8). Enumeration only, no account access.
- **Not a finding.** A signed-in user deletes another user, or points a Shoutrrr target at `http://127.0.0.1:8080` — by design (§5.1, §5.2).
- **Info (constructed).** The spec says password change keeps other sessions; the code revokes them. Stricter code, doc drift only.

---

## 7. Disclosure split

- **Critical and High** → a private GitHub repository security advisory on `mdg-labs/release-ops`. Never a Kaneo task, never a public GitHub issue, never a commit message or PR text that describes the flaw.
- **Medium, Low and Info** → a normal Kaneo task (project Release Ops) titled `Security: <short description>`, created through the usual intake flow.
- A public finding that shares a root cause with a withheld one is withheld too.
- **Fixing an advisory:** run `orchestrate --advisory GHSA-…`. The commit uses a neutral subject (no vulnerability wording) and its body ends with `Refs: GHSA-…`. The maintainer publishes the advisory once the fix reaches `main`.

---

## Open questions for the maintainer

Each with a recommended default.

1. **`X-Forwarded-For` passthrough.** `route.ts` forwards the browser-supplied header as-is; if Next.js does not overwrite a client-sent value, the first hop is attacker-chosen and T11 is bypassable. *Default:* have the proxy set the header from the socket address, ignoring any client value (or use the last hop).
2. **Login timing.** `Login` returns before bcrypt for unknown emails. *Default:* compare against a fixed dummy hash on the not-found path.
3. **Redirects on outbound calls.** No client sets `CheckRedirect`; Go strips `Authorization` on cross-host redirects but not GitLab's `PRIVATE-TOKEN`. *Default:* refuse cross-host redirects for all credentialed provider clients.
4. **`baseUrl` scheme.** `validateBaseURL` checks presence only. *Default:* require `http`/`https` and a host, for integrations and links built from them.
5. **`SESSION_SECRET` is unused.** `NewSessionManager` discards it (`_ = sessionSecret`); scs tokens are random and stored server-side. *Default:* keep the startup check, document that it is reserved, or drop it from the spec.
6. **Spec drift on password change.** Spec says no revocation; code revokes other sessions (T6). *Default:* update the spec to match the code.
7. **CSRF.** Protection rests on `SameSite=Lax`; Go does not check `Origin` or `Content-Type`. Same-site sibling apps could forge POSTs. *Default:* reject state-changing requests whose `Origin` does not match `APP_PUBLIC_URL`.
8. **Unbounded success bodies.** Source providers decode release JSON straight from `resp.Body`. *Default:* cap with `io.LimitReader` (e.g. 5 MiB).
9. **SSRF residual (§5.2).** *Default:* accept, given no RBAC; revisit if roles are added.
10. **Session rows in a DB copy (2.5).** *Default:* accept, given the 7-day lifetime; consider storing session tokens hashed.
11. **No `SECURITY.md`.** §7 assumes private advisories. *Default:* add one that points reporters to GitHub private vulnerability reporting.
12. **Actions pinned by tag.** *Default:* pin third-party actions by commit SHA.
