import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";
import {
  TEMPLATE_VARIABLES,
  integrationDefaultTemplates,
  templateVariableSyntax,
} from "@/lib/ticket-projects/content-templates";

const REPO_ROOT = path.resolve(__dirname, "../../../..");

const goSource = readFileSync(
  path.join(REPO_ROOT, "internal/tickettemplate/templates.go"),
  "utf8",
);
const specHtml = readFileSync(path.join(REPO_ROOT, "docs/specs.html"), "utf8");

// Joins the string literals of one Go const (a `"…" +` chain) into its value.
function goConst(name: string): string {
  const match = new RegExp(
    `^\\t${name} = ([\\s\\S]*?)(?:\\n\\n|\\n\\)$)`,
    "m",
  ).exec(goSource);
  if (!match) {
    throw new Error(`const ${name} not found in templates.go`);
  }
  const literals = match[1].match(/"(?:[^"\\]|\\.)*"/g);
  if (!literals) {
    throw new Error(`const ${name} has no string literals`);
  }
  return literals.map((literal) => JSON.parse(literal) as string).join("");
}

function specContentTemplates(): Record<string, string> {
  const section = specHtml.slice(
    specHtml.indexOf("<h4><code>content_templates</code> JSON shape"),
  );
  const block = /<pre><code>([\s\S]*?)<\/code><\/pre>/.exec(section);
  if (!block) {
    throw new Error("content_templates JSON block not found in specs.html");
  }
  return JSON.parse(
    block[1]
      .replaceAll("&lt;", "<")
      .replaceAll("&gt;", ">")
      .replaceAll("&amp;", "&"),
  ) as Record<string, string>;
}

describe("integrationDefaultTemplates", () => {
  it("mirrors the Go default templates", () => {
    const markdown = integrationDefaultTemplates("kaneo");
    expect(markdown.title).toBe(goConst("defaultTitleMarkdown"));
    expect(markdown.description).toBe(goConst("defaultDescriptionMarkdown"));
    expect(markdown.supersedeComment).toBe(
      goConst("defaultSupersedeCommentMarkdown"),
    );

    const plain = integrationDefaultTemplates("jira");
    expect(plain.title).toBe(goConst("defaultTitleMarkdown"));
    expect(plain.description).toBe(goConst("defaultDescriptionPlain"));
    expect(plain.supersedeComment).toBe(
      goConst("defaultSupersedeCommentPlain"),
    );
  });

  it("matches the defaults documented in specs.html §5.4", () => {
    const spec = specContentTemplates();
    const markdown = integrationDefaultTemplates("linear");
    expect(markdown.title).toBe(spec.title);
    expect(markdown.description).toBe(spec.description);
    expect(markdown.supersedeComment).toBe(spec.supersedeComment);
  });

  it("uses the plain-text description only for Jira", () => {
    expect(integrationDefaultTemplates("jira").description).not.toContain("**");
    expect(integrationDefaultTemplates("kaneo").description).toContain("**");
    expect(integrationDefaultTemplates("linear").description).toContain("**");
    expect(integrationDefaultTemplates(undefined).description).toContain("**");
  });
});

describe("TEMPLATE_VARIABLES", () => {
  it("lists every variable of the specs.html §5.4 table", () => {
    const table = specHtml.slice(
      specHtml.indexOf("<h4>MVP template variables</h4>"),
    );
    const specPaths = [
      ...table
        .slice(0, table.indexOf("</table>"))
        .matchAll(/<tr><td><code>(\.[A-Za-z.]+)<\/code>/g),
    ].map((match) => match[1]);
    expect(TEMPLATE_VARIABLES.map((variable) => variable.path)).toEqual(
      specPaths,
    );
  });

  it("limits the Supersede variables to the supersede comment", () => {
    for (const variable of TEMPLATE_VARIABLES) {
      expect(variable.worksIn).toEqual(
        variable.path.startsWith(".Supersede.")
          ? ["supersedeComment"]
          : ["title", "description", "supersedeComment"],
      );
    }
  });

  it("builds the insertable syntax with braces", () => {
    expect(templateVariableSyntax(".Repo.SourceKind")).toBe(
      "{{ .Repo.SourceKind }}",
    );
  });
});
