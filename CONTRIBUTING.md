# Contributing to Release Ops

Thanks for helping improve Release Ops. This guide covers what you need before opening a pull request.

## Contributor License Agreement

All contributions require a signed [Contributor License Agreement](CLA.md). You keep the copyright in your work; the CLA lets the maintainer distribute it under AGPL-3.0-only and under other terms, such as a hosted offering.

To sign, add this sentence to your pull request description:

```text
I have read the CLA and agree to its terms.
```

Pull requests without it can't be merged.

## Branches

| Branch | Role                                                            |
| ------ | --------------------------------------------------------------- |
| `dev`  | Integration branch — **open all pull requests against `dev`**   |
| `main` | Release track — merged from `dev` by the maintainer for release |

## Local setup

Prerequisites: Go 1.25+, Node.js 22+, `sqlite3` and `sqldiff` (for `npm run db:check`), and [golangci-lint](https://golangci-lint.run/) for Go changes.

```bash
npm install
cp .env.example .env   # set SESSION_SECRET and APP_ENCRYPTION_KEY
```

| Command               | Purpose                                      |
| --------------------- | -------------------------------------------- |
| `npm run dev`         | Next.js dev server (`apps/web`, port 3000)   |
| `go run ./cmd/server` | Go API + poll scheduler (port 8080)          |
| `npm run dev:docs`    | Starlight docs site (`apps/docs`, port 4321) |
| `make migrate-up`     | Apply pending migrations locally             |
| `make sqlc-generate`  | Regenerate typed SQL from `queries/`         |

The local Go server expects `SESSION_SECRET`, `APP_ENCRYPTION_KEY` and `APP_DB_PATH` (see [spec §9](https://mdg-labs.github.io/release-ops/spec/#env)). To run the full stack in one container instead, use `docker compose up -d` as described in the [README](README.md#quick-start).

## Checks before you open a PR

Run the checks for the paths you touched. CI runs all of them on every pull request.

| You changed                                                           | Run                                  |
| --------------------------------------------------------------------- | ------------------------------------ |
| `apps/web/**`, root `package.json` / `package-lock.json`              | `npm test && npm run lint`           |
| `db/schema.sql`, `migrations/**`                                      | `npm run db:check`                   |
| `cmd/**`, `internal/**`, `queries/**`, `sqlc.yaml`, `go.mod`/`go.sum` | `go test ./... && golangci-lint run` |
| `Makefile`, `Dockerfile`, `.github/workflows/**`                      | all of the above                     |

Before the PR is ready for review, run the full set, including the type check:

```bash
npm test && npm run lint && npm run typecheck && npm run db:check
go test ./... && golangci-lint run
```

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/) with a scope. Keep the subject at 72 characters or fewer and in the imperative mood.

```text
<type>(<scope>): <imperative summary>
fix(api): apply custom ticket templates at poll time
docs(docs): clarify bootstrap admin setup
```

- **Types:** `feat`, `fix`, `chore`, `refactor`, `docs`, `test`, `ci`, `build`, `perf`
- **Scopes:** `release-ops`, `api`, `db`, `config`, `ci`, `docs`, `deps`
- If your change relates to a GitHub issue, put its number in square brackets after the scope: `fix(api)[#123]: …`
- If the commit resolves that issue, add `fixes #123` to the commit body. The issue then closes when the commit reaches `main`.

## Database migrations

Never write or edit files in `migrations/` by hand. They are generated from the canonical schema:

1. Edit `db/schema.sql`.
2. Run `make migrate-diff name=<change>` (uses SQLite `sqldiff`).
3. Review the generated `.up.sql` / `.down.sql`, then run `make migrate-up`.
4. `npm run db:check` must pass.

## Web UI strings (i18n)

The web UI has **no hardcoded user-facing strings**. Labels, buttons, headings, errors, toasts, placeholders and `aria-label`s live in `apps/web/messages/en.json` and are rendered through next-intl:

```tsx
const t = useTranslations("repos");
return <h1>{t("title")}</h1>;
```

Use `getTranslations` from `next-intl/server` in server components. `npm run lint` enforces this with `i18next/no-literal-string`.

## Specs

Behaviour is defined in [`docs/specs.html`](https://mdg-labs.github.io/release-ops/spec/), with `db/schema.sql` as the canonical schema. If something isn't covered there, open an issue to discuss it before you implement it.
