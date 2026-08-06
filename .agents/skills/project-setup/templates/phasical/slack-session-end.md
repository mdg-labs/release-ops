# Session-end Slack DM — {PROJECT_NAME}

> Optional local file. Omit if project does not use Slack notifications.

## Default operator (recipient)

| Field | Value |
| ----- | ----- |
| Display name | {OPERATOR_NAME} |
| Email | {OPERATOR_EMAIL} |
| Slack user_id | {OPERATOR_SLACK_USER_ID} |

## Sender

Slack MCP authenticates as the **service account** (`cursor@mdg-labs.dev`). The authenticated user is the **sender**, not the recipient.

## Run overrides

| User says | Effect |
| --------- | ------ |
| `no slack` / `skip slack` | Skip session-end DM |
| `slack to <email>` | Resolve recipient via `slack_search_users` |
| `slack to <user_id>` | Use that user_id as recipient |

## Forbidden

- DM the authenticated MCP service account as the notification recipient
- Resolve recipient via `git config user.email` or GitHub profile email
- Post orchestrator summaries to public channels — **operator DM only**

## MCP

Server: `plugin-slack-slack`  
Tool: `slack_send_message` with `channel_id` = operator **recipient** user_id

## Message template

```markdown
**{PROJECT_NAME} orchestrator — run complete**

- **Mode:** <plan-file | Phasical | chat>
- **Scope:** <e.g. P1-03–P1-05 | #12 epic>
- **Result:** <N passed · M failed · K blocked>

**Tasks**
<one line per task: TASK-ID or #N — PASS | FAIL | blocked>

**Next**
<recommended next batch or "none — scope complete">
```
