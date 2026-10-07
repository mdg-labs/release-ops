# Landing page template — Release Ops

Template for the site front page (`apps/docs/src/content/docs/index.mdx`, hand-written) and, in a longer form, `concepts/product-overview.md`. Voice rules are in [style-guide.md](style-guide.md); paths in [docs-config.md](docs-config.md).

## Rules

| Rule | Detail |
| ---- | ------ |
| Claims | Every sentence traces to `docs/specs.html` §1 (Purpose and MVP non-goals), or to behaviour you verified in `apps/web`. Cross-check each sentence before you propose it. |
| Self-hosting only | Never mention, hint at or compare with a hosted, cloud or managed version. |
| Audience | Small self-hosting dev/ops teams and individuals who want upstream releases of the projects they depend on turned into tickets automatically. |
| Links | Only to pages that exist on the site (Getting started, GitHub). No links to the internal spec, stack, schema, MVP checklist or roadmap. Add links to guide or concept pages only once those pages exist. |
| Base path | Links in `index.mdx` frontmatter and body carry the site base (`/release-ops/getting-started/`). |
| Not allowed | Tooling tag clouds, spec-precedence lists, screenshots, internal identifiers, env var names, pricing or roadmap promises. |
| Language | English only. Short sentences. No hype. |

## `index.mdx` structure

```mdx
---
title: Release Ops
description: <one sentence: self-hosted release monitor that turns new upstream releases into tickets>
template: splash
hero:
  tagline: <one sentence outcome for the reader>
  actions:
    - text: Get started
      link: /release-ops/getting-started/
      icon: right-arrow
      variant: primary
    - text: View on GitHub
      link: https://github.com/mdg-labs/release-ops
      icon: external
      variant: minimal
---

## What Release Ops is
<release sources, ticket systems, notifications, status in the UI; one container you host yourself>

## How it works
<one picture: source → poll → ticket + notification; then 3 numbered steps>

## Who it is for
<the audience sentence plus the problem it removes>

## What it is not
<the MVP non-goals from specs §1, one bullet each: no further sources, no roles, no public sign-up, not multi-service>

## Next steps
<link to Getting started and GitHub>
```

## What to source from the spec

| Section | Source |
| ------- | ------ |
| What it is | `docs/specs.html` §1 Purpose: sources, ticket systems, per-repo combination, several ticket projects per integration, notifications, operational status |
| How it works | §1 Purpose and §5 domain logic: latest stable release, baseline on the first poll, ticket when the tag changes, open-ticket policy (supersede by default, merge, skip) |
| What it is not | §1 MVP non-goals, in customer words |
| Who it is for | `docs-config.md` § Product audience |

## `concepts/product-overview.md`

Same four topics (what it is, what it is not, who it is for, how it works), as a standard concept page with frontmatter from [docs-config.md](docs-config.md), no `splash` template and no hero. Link onward to guide pages once they exist.
