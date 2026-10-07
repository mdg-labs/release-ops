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
            { label: "Introduction", slug: "index" },
            { label: "Getting started", slug: "getting-started" },
            { label: "MVP checklist", slug: "mvp-checklist" },
          ],
        },
        {
          label: "Reference",
          items: [
            { label: "Product specification", slug: "spec" },
            { label: "Tech stack", slug: "stack" },
            { label: "Database schema", slug: "schema" },
          ],
        },
        {
          label: "Project",
          items: [{ label: "Roadmap", link: "/roadmap/" }],
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
