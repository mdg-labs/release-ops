# Known escapes

Defect patterns that passed review and were later confirmed real by CodeRabbit
on a pull request. `task-executor` checks its change against this list before
committing; `task-verifier` checks each commit against it under layers 6 and 7.
`cr-review` appends a line whenever it fixes a confirmed finding whose pattern
is not here yet.

One line per pattern: **category** — what goes wrong — where it was seen. Keep
it to patterns, not individual bugs; merge a new instance into an existing line
by adding its PR number.

## Wiring and missing scope
- **wiring** — a service built but never constructed in `cmd/server` (notifier not passed to the scheduler or the notification test endpoint), so the feature silently does nothing — PR 112
- **wiring** — a value read once at image build instead of at runtime (`APP_TIMEZONE` frozen into a statically rendered layout), so the container ignores the deployment's environment — dev 8190afc
- **scope** — a documented behaviour implemented only for the common case (GitLab base URL path prefix dropped, so instances under a subpath 404 into "no release"; Jira `initialStatus` not applied after create) — PR 112
- **scope** — a filter that matches more or less than its documented rule (`git log --grep` over the whole message where only the subject and a trailer were meant to count; a SemVer tag pattern that rejects a prerelease and build suffix together) — PR 133, PR 153
- **scope** — a provider payload field sent unconditionally or empty where the remote API rejects it (Jira priority when unset, empty ADF text) — PR 112

## Partial failure and atomicity
- **partial-failure** — the new external object (ticket) is not persisted right after it is created, so a later failing step orphans it and the next poll creates a duplicate — PR 112
- **partial-failure** — a migration or startup step fails half-way on existing data and leaves a dirty `schema_migrations` row instead of refusing up front with an actionable message — PR 112
- **atomicity** — a one-time token checked and then consumed in two steps, so two concurrent redemptions both succeed; consume first with a `used_at IS NULL` guard — PR 112
- **stale-state** — changing a parent setting (a repo's ticket project or source) leaves derived state (open ticket, baseline tag) pointing at the old one — PR 112
- **partial-failure** — a file written before the validation meant to guard it, so an edit the tool reports as refused is already on disk — PR 133
- **data-safety** — a generated migration that drops and recreates a table loses existing rows; additive changes must come out as `ALTER TABLE … ADD COLUMN` — #96

## Fail-open and error handling
- **fail-open** — a failed fetch or ticket call recorded as a successful run (engine error actions not counted), so the run status hides the failure — PR 112
- **fail-open** — a security option silently downgraded when the peer doesn't support it (SMTP STARTTLS not offered → mail sent in plaintext) instead of failing the send — PR 112
- **fail-open** — a public placeholder secret from `.env.example` accepted at startup — PR 112

## Security
- **auth** — session token not renewed on login, invitation accept, password reset or email change (session fixation); a password reset that leaves other sessions alive — PR 112
- **auth** — an open redirect through a `?redirect=` parameter that is not restricted to same-origin paths — PR 112
- **auth** — responses or timing that reveal whether an account exists (forgot-password returning SMTP errors or answering slower for real users) — PR 112
- **credential** — changing an integration's base URL without a new secret, so the stored credential is sent to a new host — PR 112
- **rate-limit** — `X-Forwarded-For` trusted from any peer, or not forwarded by the Next.js proxy, so every user shares one bucket or an attacker picks their own — PR 112, PR 133
- **input** — email addresses compared case-sensitively, or display-name forms (`Name <a@b>`) accepted where a bare address is expected — PR 112

## Docs
- **docs** — a worked example or table row that contradicts the rule it illustrates, or states an invariant without the gap the same doc records elsewhere (or the partial-failure path the code takes) — PR 133, PR 140, PR 153, PR 167
- **docs** — a page that declares itself a mirror of a canonical file (`db/schema.sql`, `docs/specs.html`) left stale when that file changes, because only the canonical file was edited — PR 140

## UI states
- **ui** — rate-limited (429) and server (5xx) responses shown as "invalid credentials" — PR 112
- **ui** — a "hidden while loading or failed" state gated on `data` alone; TanStack Query keeps the last data after a failed refetch, so check `isError` too — PR 153
- **ui** — an async action's result applied to whatever dialog is open when it settles, so a dismissed dialog's late failure (or success) lands on the next one; guard each attempt and invalidate it on close — PR 167

## Release and CI
- **ci** — a release workflow that builds from the branch head at publish time instead of the release tag, so the tagged image and the tag differ — PR 112
- **ci** — a retry of a release job refused because an earlier attempt already pushed the tag — PR 112
- **runtime** — a container entrypoint that keeps running when one of its two processes (Go, Next.js) dies, with no health check to notice — PR 112
