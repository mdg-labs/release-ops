# Doc index — {PROJECT_NAME}

> Local spec shorthand map. Expand per project.

## Spec documents

| Shorthand | Path | Topics |
| --------- | ---- | ------ |
| `spec` | `{PRIMARY_SPEC_PATH}` | MVP contract, AC |
| `roadmap` | `{PLAN_FILE}` | Phases, task IDs |

## Verification commands

| Scope | Command |
| ----- | ------- |
| Default lint | `{LINT_CMD}` |
| Default typecheck | `{TYPECHECK_CMD}` |
| Default test | `{TEST_CMD}` |

Map committed paths → package filters per `.cursor/rules/06-local-ci-before-commit.mdc` (if present).

## Phase gates (optional)

| Gate | Blocks |
| ---- | ------ |
| {GATE_ID} | {PHASE_LIST} |

## Hot files (never parallelize)

- `{path}` — {reason}
