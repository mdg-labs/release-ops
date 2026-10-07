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
        "Self-hosted release monitor — poll GitHub, GitLab, Gitea, Forgejo, and Codeberg; create tickets when releases ship.",
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
          items: [
            { label: "Getting started", slug: "getting-started" },
            { label: "FAQ", slug: "faq" },
          ],
        },
        {
          label: "User guide",
          items: [
            { label: "First steps", slug: "guide/first-steps" },
            { label: "Dashboard", slug: "guide/dashboard" },
            { label: "Integrations", slug: "guide/integrations" },
            { label: "Ticket projects", slug: "guide/ticket-projects" },
            { label: "Repos", slug: "guide/repos" },
            { label: "Poll runs", slug: "guide/poll-runs" },
            { label: "Notifications", slug: "guide/notifications" },
            { label: "Settings", slug: "guide/settings" },
            { label: "Users", slug: "guide/users" },
            { label: "Profile", slug: "guide/profile" },
            { label: "Signing in", slug: "guide/signing-in" },
          ],
        },
        {
          label: "Concepts",
          items: [
            { label: "Product overview", slug: "concepts/product-overview" },
            {
              label: "How release detection works",
              slug: "concepts/release-detection",
            },
            { label: "Status mapping", slug: "concepts/status-mapping" },
            {
              label: "Open-ticket policy",
              slug: "concepts/open-ticket-policy",
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
