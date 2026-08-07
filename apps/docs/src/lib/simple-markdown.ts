/**
 * Minimal markdown → HTML for roadmap task descriptions (headings, lists, links).
 */
export function simpleMarkdown(md: string | undefined): string {
  if (!md) return "";

  const esc = (s: string) =>
    String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");

  const linkMap = {
    "specs.html": "/spec/",
    "stack.html": "/stack/",
    "schema.html": "/schema/",
  };

  let html = esc(md)
    .replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_, text, href) => {
      let url = href;
      for (const [from, to] of Object.entries(linkMap)) {
        url = url.replace(from, to);
      }
      return `<a href="${url}">${text}</a>`;
    })
    .replace(/^## (.+)$/gm, "<h4>$1</h4>")
    .replace(/^- \[ \] (.+)$/gm, "<li class='ac'>$1</li>")
    .replace(/^- `([^`]+)`$/gm, "<li><code>$1</code></li>")
    .replace(/^- (.+)$/gm, "<li>$1</li>")
    .replace(/\n\n/g, "</p><p>");

  if (!html.startsWith("<")) {
    html = `<p>${html}</p>`;
  }

  return html;
}
