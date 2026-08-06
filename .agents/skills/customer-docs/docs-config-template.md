# Customer docs config — {PRODUCT_NAME}

> Supporting file — created by customer-docs (Mode 0 bootstrap). Lives under `.agents/project/customer-docs/` — **not** inside `.agents/skills/` (`npx skills update` wipes skill directories).

## Repository

| Field | Value |
| ----- | ----- |
| Product name | {PRODUCT_NAME} |
| Docs repo | {DOCS_REPO_SAME_OR_SEPARATE} |
| Docs root path | `{DOCS_ROOT_PATH}` |
| App repo path | `{APP_REPO_PATH}` |
| Monorepo workspace | {WORKSPACE_ROOT or same as app} |

## Publishing

| Field | Value |
| ----- | ----- |
| Framework | {FRAMEWORK — Docusaurus, Mintlify, Nextra, VitePress, Starlight, plain Markdown, other} |
| Deploy | {CI pipeline name, manual, or other} |
| Published base URL | `{BASE_URL}` |
| Default language | `en` (English only — baseline) |

## App inventory

| Field | Value |
| ----- | ----- |
| End-user apps | {APP_NAMES — comma-separated} |
| Routes / pages dirs | `{ROUTES_DIRS}` — e.g. `app/`, `src/pages/`, `src/routes/` |
| Navigation source | `{NAV_SOURCE}` — e.g. sidebar config, route manifest, layout component |
| Roles / permissions | {HOW_DETECTED — e.g. middleware, RBAC config path, or "manual per page"} |

## Audience

| Field | Value |
| ----- | ----- |
| Audience type | {technical \| non-technical \| mixed} |
| Core use case | {ONE_SENTENCE_PRODUCT_VALUE} |
| Spec doc index | `.agents/project/orchestrator/doc-index.md` — {present \| absent} |

## Docs structure

| Field | Value |
| ----- | ----- |
| Page docs folder | `{PAGE_DOCS_FOLDER}` — e.g. `docs/features/` |
| Concepts folder | `{CONCEPTS_FOLDER}` — e.g. `docs/concepts/` |
| Getting started path | `{GETTING_STARTED_PATH}` — e.g. `docs/getting-started.md` |
| FAQ path | `{FAQ_PATH}` — e.g. `docs/faq.md` |
| File naming | kebab-case |
| Nav / sidebar file | `{NAV_FILE_PATH}` |
| Frontmatter schema | {FRAMEWORK_FRONTMATTER_NOTES — fields required by the docs framework} |

## Screenshots

| Field | Value |
| ----- | ----- |
| Policy | {PLACEHOLDERS \| NONE} |
| Placeholder format | `{SCREENSHOT: short description}` — skill never captures images |

## Phasical integration

| Field | Value |
| ----- | ----- |
| Enabled | {yes \| no} |
| project.config.md | `.agents/project/orchestrator/project.config.md` — {present \| absent} |
| Commit subject (with task) | `[#N] docs({SCOPE}): {summary}` |
| Commit subject (no task) | `docs({SCOPE}): {summary}` — only when Phasical sync opted out |
| Allowed docs scope | `{DOCS_SCOPE}` — e.g. `docs`, `help` |

## Conventions

- One customer-facing doc page per in-app page (plus Getting Started, concepts, FAQ).
- Terminology: `.agents/project/customer-docs/glossary.md` — one customer term per concept.
- Coverage tracking: `.agents/project/customer-docs/coverage.md`.
- Style: `.agents/skills/customer-docs/style-guide.md` (installed skill — do not copy locally).
