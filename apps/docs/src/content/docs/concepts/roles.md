---
title: "Roles"
description: "The two roles, admin and viewer, how GitHub decides which one you get, and which repositories a viewer sees."
---

Release Ops has no local accounts and no passwords. You sign in with GitHub through the instance's own GitHub App, and GitHub decides your role. There are two roles: admin and viewer. Roles are never edited in Release Ops. The **Settings** page shows the mapping read-only, under **Role mapping**.

## Who gets which role

| App installed on | Admin | Viewer | No access |
| ---------------- | ----- | ------ | --------- |
| An organization | The organization's owners, the active members of the admin team when one is set, and the bootstrap admin. | Every other active member of the organization. When a viewer team is set, only the active members of that team. Admins are never excluded by it. | Everyone else, including people with a pending invitation. |
| A personal account | The account owner and the bootstrap admin. | There are no viewers. | Everyone else. |

- The admin team and the viewer team are set by the operator in the instance's environment as `org/team-slug`. They must name a team of the organization the App is installed on. A team of another organization grants nothing, and the **Settings** page reports it as a configuration problem. On a personal account both are ignored.
- Members of child teams count as members of the team.
- The bootstrap admin is a GitHub account the operator pins when installing. It is always an admin, whatever its organization or team membership. Before the App is installed, only the bootstrap admin can sign in.

## What each role can do

| | Viewer | Admin |
| --- | ------ | ----- |
| See the **Dashboard** and the update PRs, with their ticket state | Their repositories only | All repositories |
| See **About** | Yes | Yes |
| Manage **Integrations** and **Ticket projects** | No | Yes |
| Open **Settings**: the instance settings, the GitHub App and installation, the global Renovate config | No | Yes |
| Start a run by hand (run now), and pause or enable a repository | No | Yes |
| Change a role | No | No, roles come from GitHub |

The server checks the role on every request. Hiding a control in the page is a convenience, not the check. A viewer who calls an admin function is refused.

## Which repositories a viewer sees

A viewer sees only the repositories they can access on GitHub. At sign-in, Release Ops reads the repositories your GitHub account can reach through the App's installation, keeps those that are also installed, and stores the list with your session. It does not store your GitHub token. The list is not refreshed during the session, and a session ends 8 hours after sign-in, so the list never outlives that.

A repository outside your list does not appear. Release Ops does not reveal that it exists.

Admins see everything the installation covers.

## How long a session lasts

- A session ends 8 hours after you sign in. Signing in again starts a new one.
- Release Ops checks your role with GitHub every 15 minutes. If you have lost your role, your session ends. If it changed, the new role applies.
- When GitHub cannot be reached for that check, Release Ops refuses the request instead of serving a role that may be out of date.

## Related pages

- [Getting started](../../getting-started/#8-sign-in) for the first sign-in and the bootstrap admin
- [Update PRs and tickets](../update-prs-and-tickets/)
