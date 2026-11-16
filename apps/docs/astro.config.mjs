import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";

// https://mdg-labs.github.io/release-ops/
const site = "https://mdg-labs.github.io";
const base = "/release-ops";

export default defineConfig({
  site,
  base,
  integrations: [
    starlight({
      title: "Release Ops",
      description:
        "Self-hosted dependency-update bot — a GitHub App runs Renovate on your repositories, and every update PR becomes a ticket in Kaneo, Jira or Linear.",
      social: [
        {
          icon: "github",
          label: "GitHub",
          href: "https://github.com/mdg-labs/release-ops",
        },
      ],
      customCss: ["./src/styles/custom.css"],
      sidebar: [
        {
          label: "Start here",
          items: [{ label: "Getting started", slug: "getting-started" }],
        },
        {
          label: "Concepts",
          items: [
            {
              label: "How runs are triggered",
              slug: "concepts/how-runs-are-triggered",
            },
            {
              label: "Update PRs and tickets",
              slug: "concepts/update-prs-and-tickets",
            },
            { label: "Status mapping", slug: "concepts/status-mapping" },
            { label: "Roles", slug: "concepts/roles" },
            {
              label: "The global Renovate config",
              slug: "concepts/global-renovate-config",
            },
          ],
        },
        {
          label: "User guide",
          items: [
            {
              label: "Migrating from Renovate",
              slug: "guide/migrating-from-renovate",
            },
          ],
        },
      ],
      head: [
        {
          tag: "link",
          attrs: {
            rel: "preconnect",
            href: "https://fonts.googleapis.com",
          },
        },
        {
          tag: "link",
          attrs: {
            rel: "preconnect",
            href: "https://fonts.gstatic.com",
            crossorigin: true,
          },
        },
        {
          tag: "link",
          attrs: {
            rel: "stylesheet",
            href: "https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500;700&display=swap",
          },
        },
      ],
      editLink: {
        baseUrl: "https://github.com/mdg-labs/release-ops/edit/dev/apps/docs/",
      },
    }),
  ],
});
