# Security policy

## Reporting a vulnerability

Report a suspected vulnerability privately through GitHub private vulnerability reporting for this repository:

<https://github.com/mdg-labs/release-ops/security/advisories/new>

Please do not open a public issue, pull request or discussion for a vulnerability yourself, and do not describe it in a commit message or comment. A public report gives everyone running Release Ops the details before a fix exists; for a lower-rated finding the maintainer opens the public issue once it is triaged.

A useful report says which version you tested, what an attacker needs to start with (no account, a signed-in user, access to the container's network or to the `/data` volume), what they gain, and the steps to reproduce it. Do not include real credentials, tokens or private keys; redact them.

Reports are read by the maintainer, who rates each confirmed vulnerability under the [threat model's severity rubric](docs/threat-model.md#6-severity-rubric). One rated Critical or High is fixed in a private advisory, and the advisory is published once the fix has been released. One rated Medium, Low or Info is tracked as a public issue labelled `security`, which the maintainer files after triage; see [disclosure split](docs/threat-model.md#7-disclosure-split).

## Supported versions

Only the latest release receives security fixes, until v2.0.0 ships. Fixes are not backported to earlier releases. Release Ops is released as a container image published to GHCR when a GitHub release is published, and the `latest` tag and the newest version tag are the supported ones; `dev` nightly images are not.

## What counts as a vulnerability

The attackers Release Ops defends against, the invariants it keeps and the limits it accepts are written down in [`docs/threat-model.md`](docs/threat-model.md), and §7 there describes how reports are handled. A report that shows one of those invariants broken is in scope. Behaviour the threat model lists as an accepted residual, such as an admin pointing an integration at an internal address, is not a vulnerability.
