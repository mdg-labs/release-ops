# Known escapes

Defect patterns that passed a `task-verifier` PASS and were later confirmed
real in review (CodeRabbit on a promotion or unit PR). `task-executor` checks
its change against this list before committing; `task-verifier` checks each
commit against it under layers 6 and 7. `pr-review` appends a line whenever it
fixes a confirmed finding whose pattern is not here yet.

One line per pattern: **category** — what goes wrong — where it was seen.
Keep it to patterns, not individual bugs; merge a new instance into an
existing line by adding its PR number.

## Wiring and missing scope
- **wiring** — a service built but never constructed in `cmd/server` (notifier not passed to the scheduler or the notification test endpoint), so the feature silently does nothing — PR 112
- **wiring** — a value read once at image build instead of at runtime (`APP_TIMEZONE` frozen into a statically rendered layout), so the container ignores the deployment's environment — dev 8190afc
- **scope** — a documented behaviour implemented only for the common case (GitLab base URL path prefix dropped, so instances under a subpath 404 into "no release"; Jira `initialStatus` not applied after create) — PR 112
- **scope** — a filter that matches more or less than its documented rule (`git log --grep` over the whole message where only the subject and a trailer were meant to count; a SemVer tag pattern that rejects a prerelease and build suffix together) — PR 133, PR 153
- **scope** — a provider payload field sent unconditionally or empty where the remote API rejects it (Jira priority when unset, empty ADF text) — PR 112
- **scope** — an eligibility rule the spec states (a paused or removed repository gets no job, no token, and its running job is told to abort) enforced at none of the gates that hand out work (claim, token lookup, heartbeat) — PR 246
- **wiring** — a one-shot start-up action fired on the first pass of one of its inputs even when that pass failed or ran before the other input existed (the PR reconcile triggered before the bot id resolved or the repositories were synced), and never fired again — PR 255
- **scope** — a reconcile asked for on one direction of a change only (repositories added, not removed), so out-of-order or concurrent deliveries leave stale state until the next scheduled pass — PR 255
- **scope** — a validation pattern stricter than the identifier it checks (managed-user GitHub logins with `_`, a git ref starting with `_`), so valid input is refused at sign-in or startup — PR 255
- **scope** — bounded per-pass work that always takes the same first N items, so the rest are starved for good; and a conditional request whose 304 lacks a header the loop needs (`Link`) taken as "no next page" — PR 255

## Partial failure and atomicity
- **partial-failure** — the new external object (ticket) is not persisted right after it is created, so a later failing step orphans it and the next poll creates a duplicate — PR 112
- **partial-failure** — a migration or startup step fails half-way on existing data and leaves a dirty `schema_migrations` row instead of refusing up front with an actionable message — PR 112
- **atomicity** — a one-time token checked and then consumed in two steps, so two concurrent redemptions both succeed; consume first with a `used_at IS NULL` guard — PR 112
- **stale-state** — changing a parent setting (a repo's ticket project or source) leaves derived state (open ticket, baseline tag) pointing at the old one — PR 112
- **partial-failure** — a file written before the validation meant to guard it, so an edit the tool reports as refused is already on disk — PR 133
- **data-safety** — a generated migration that drops and recreates a table loses existing rows; additive changes must come out as `ALTER TABLE … ADD COLUMN` — #96
- **data-safety** — a cleanup `DELETE` whose `ON DELETE CASCADE` erases history the spec keeps (pausing a repository deleted a retry-waiting job and its `job_runs`); end such rows in a terminal state instead — PR 255
- **atomicity** — values that must belong together read in separate queries (an integration's host and its credential; a PR row, its dependencies and its ticket), so a write between the reads pairs two revisions; read them from one row or one read transaction — PR 271
- **atomicity** — a conditional write or claim decided from a read or a clock taken before the lock or transaction it runs in (a gone PR closed after a webhook updated it; a state's expiry checked before the claim lock); re-check the version, or read the clock, inside it — PR 271
- **atomicity** — a step that runs after a gate without checking the gate's verdict (dependencies and release notes recorded for a payload the upsert rejected) — PR 271

## Fail-open and error handling
- **fail-open** — a failed fetch or ticket call recorded as a successful run (engine error actions not counted), so the run status hides the failure — PR 112
- **fail-open** — a security option silently downgraded when the peer doesn't support it (SMTP STARTTLS not offered → mail sent in plaintext) instead of failing the send — PR 112
- **fail-open** — a public placeholder secret from `.env.example` accepted at startup — PR 112
- **fail-open** — a stand-in handler registered in production that reports success for work it never did (a no-op `renovate_run` handler); leave the kind unregistered so the attempt fails visibly — PR 246
- **fail-open** — a read error inside a handler whose transaction marks the work as done (a webhook delivery GUID) treated as "nothing to do", so the delivery commits as handled and its redelivery is deduplicated away — PR 255
- **limit** — a body-size cap sized from the raw field caps without the encoding's worst-case expansion (JSON `\u003c` is six bytes), so a valid maximal request is refused — PR 255
- **rate-limit** — an upstream rate-limit answer whose delay is dropped or lifted early: a 429 that still lists remaining requests, a `Retry-After` on a non-429 status branch ignored, a cooldown replaced by a later window's reset, a huge header value that overflows on conversion to a duration — PR 271
- **scope** — a rule enforced for one shape of its input only (a report entry without a commit taken as the current head; a template validator that skips `with` scopes, parenthesised chains, variables and invoked templates; a JSON `null` label decoded to `""`) — PR 271

## Security
- **auth** — session token not renewed on login, invitation accept, password reset or email change (session fixation); a password reset that leaves other sessions alive — PR 112
- **auth** — an open redirect through a `?redirect=` parameter that is not restricted to same-origin paths — PR 112
- **auth** — responses or timing that reveal whether an account exists (forgot-password returning SMTP errors or answering slower for real users) — PR 112
- **credential** — changing an integration's base URL without a new secret, so the stored credential is sent to a new host — PR 112
- **rate-limit** — `X-Forwarded-For` trusted from any peer, or not forwarded by the Next.js proxy, so every user shares one bucket or an attacker picks their own — PR 112, PR 133
- **input** — email addresses compared case-sensitively, or display-name forms (`Name <a@b>`) accepted where a bare address is expected — PR 112
- **credential** — a token cache keyed on less than what decides the token's scope, so a narrower fallback token minted for one request is served to another it does not cover — PR 246
- **redaction** — a secret shape or tail cut that stops at a delimiter the secret may contain or that misses an encoding it travels in (`@`/`#` in URL user-info, quoted `NAME="…"`, JSON `\/`, a truncated line with no newline) — PR 255
- **redaction** — a redaction path that covers attribute keys but not group names, a registry renewal that keeps the first deadline, or an unauthenticated value registered before it is validated (spending the bounded budget) — PR 271
- **untrusted-path** — a file the untrusted side can create opened with a blocking `open` (a FIFO at the report path hangs the worker slot); open non-blocking and check the type first — PR 255
- **owned-config** — settings the app owns removed from the top level of a user-supplied config but not from a nested override that wins over them (Renovate `force`) — PR 255

## Docs
- **docs** — a worked example or table row that contradicts the rule it illustrates, or states an invariant without the gap the same doc records elsewhere (or the partial-failure path the code takes) — PR 133, PR 140, PR 153, PR 167, PR 246
- **docs** — a page that declares itself a mirror of a canonical file (`db/schema.sql`, `docs/specs.html`) left stale when that file changes, because only the canonical file was edited — PR 140
- **docs** — a name or command in prose that differs from the canonical file it describes (Compose service names vs `docker-compose.yml`), or two policy documents that state contradicting rules (SECURITY.md vs threat-model §7) — PR 246
- **docs** — an operational procedure that discards data it should keep (a restore step that deletes a volume snapshot's WAL) — PR 246
- **docs** — a README or overview that presents planned features as available — PR 246

## UI states
- **ui** — rate-limited (429) and server (5xx) responses shown as "invalid credentials" — PR 112
- **ui** — a "hidden while loading or failed" state gated on `data` alone; TanStack Query keeps the last data after a failed refetch, so check `isError` too — PR 153
- **ui** — an async action's result applied to whatever dialog is open when it settles, so a dismissed dialog's late failure (or success) lands on the next one; guard each attempt and invalidate it on close — PR 167
- **ui** — state set just before navigating away (a hand-off flag) never cleared when the page comes back from the back-forward cache, leaving the form disabled; also an error message whose recovery step cannot work (re-enter a single-use token) — PR 246
- **ui** — a "cannot check right now" answer (503 from the session check) treated as "no session", sending a signed-in user to the login page — PR 255
- **ui** — a form control left enabled while its save is pending, so the success message describes a value that was not saved; a late async result (a slower file read, an in-flight GET) overwriting a newer one — PR 271
- **ui** — an unedited field re-parsed through a lossy text form on save (labels joined with `, ` and split on `,`), so an unrelated edit rewrites stored data — PR 271

## Tests
- **test** — a rejection test whose input fails an earlier validation, so it passes with the guard it claims to cover removed; a test that uses a listener started in a goroutine before waiting for it to answer — PR 246
- **test** — a test that depends on the row order of random ids or on variables leaking in from the process environment, or a filtered-deletion test with no similar-but-distinct record asserted to survive; a privileged-only fixture step run on the unprivileged path — PR 255
- **test** — a test whose failure fires before the code path it claims to cover (an error injected before the publication callback is registered); a helper that calls `t.Fatal` from a goroutine, or a held handler never released when the test fails; a leak check missing the encoded forms a secret is logged in; a third-party log fixture guessed instead of taken from its serializer (Renovate's `err.options.url`); an input guard without a rejection case for each malformed shape — PR 271

## Release and CI
- **ci** — a release workflow that builds from the branch head at publish time instead of the release tag, so the tagged image and the tag differ — PR 112
- **ci** — a retry of a release job refused because an earlier attempt already pushed the tag — PR 112
- **runtime** — a container entrypoint that keeps running when one of its two processes (Go, Next.js) dies, with no health check to notice — PR 112
