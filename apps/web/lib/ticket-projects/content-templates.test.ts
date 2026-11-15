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
    specHtml.indexOf("<h4>v2 <code>content_templates</code> JSON shape"),
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

function specVariables(): { path: string; worksIn: string }[] {
  const section = specHtml.slice(
    specHtml.indexOf("<h4>Template variables</h4>"),
  );
  const table = section.slice(0, section.indexOf("</table>"));
  const rows: { path: string; worksIn: string }[] = [];
  for (const row of table.matchAll(
    /<tr><td>([\s\S]*?)<\/td><td>[\s\S]*?<\/td><td>([\s\S]*?)<\/td><\/tr>/g,
  )) {
    const codes = [...row[1].matchAll(/<code>(\.[A-Za-z.]+)<\/code>/g)].map(
      (match) => match[1],
    );
    // A row may list siblings relative to its first path: .Supersede.NewPullRequest.URL, .Number
    const parent = /^(\.Supersede\.NewPullRequest)\./.exec(codes[0])?.[1] ?? "";
    codes.forEach((code, index) => {
      rows.push({
        path: index > 0 && parent ? parent + code : code,
        worksIn: row[2].replace(/<\/?code>/g, ""),
      });
    });
  }
  return rows;
}

describe("integrationDefaultTemplates", () => {
  it("mirrors the Go default templates", () => {
    const markdown = integrationDefaultTemplates("kaneo");
    expect(markdown.title).toBe(goConst("defaultTitle"));
    expect(markdown.description).toBe(goConst("defaultDescriptionMarkdown"));
    expect(markdown.updateComment).toBe(goConst("defaultUpdateComment"));
    expect(markdown.supersedeComment).toBe(goConst("defaultSupersedeComment"));

    const plain = integrationDefaultTemplates("jira");
    expect(plain.title).toBe(goConst("defaultTitle"));
    expect(plain.description).toBe(goConst("defaultDescriptionPlain"));
    expect(plain.updateComment).toBe(goConst("defaultUpdateComment"));
    expect(plain.supersedeComment).toBe(goConst("defaultSupersedeComment"));
  });

  it("matches the defaults documented in specs.html §5.4", () => {
    const spec = specContentTemplates();
    expect(Object.keys(spec).sort()).toEqual([
      "description",
      "supersedeComment",
      "title",
      "updateComment",
    ]);
    const markdown = integrationDefaultTemplates("linear");
    expect(markdown.title).toBe(spec.title);
    expect(markdown.description).toBe(spec.description);
    expect(markdown.updateComment).toBe(spec.updateComment);
    expect(markdown.supersedeComment).toBe(spec.supersedeComment);
  });

  it("uses the plain-text description only for Jira", () => {
    expect(integrationDefaultTemplates("jira").description).not.toContain("**");
    expect(integrationDefaultTemplates("kaneo").description).toContain("**");
    expect(integrationDefaultTemplates("linear").description).toContain("**");
    expect(integrationDefaultTemplates(undefined).description).toContain("**");
  });

  it("uses only variables of the update-PR contract", () => {
    for (const kind of ["kaneo", "jira"] as const) {
      const defaults = integrationDefaultTemplates(kind);
      for (const text of Object.values(defaults)) {
        expect(text).not.toMatch(/\.(Release|Repo|Previous)\b/);
        expect(text).not.toMatch(/OldTag|NewTag/);
      }
    }
  });
});

describe("TEMPLATE_VARIABLES", () => {
  it("lists every variable of the specs.html §5.4 table", () => {
    expect(TEMPLATE_VARIABLES.map((variable) => variable.path)).toEqual(
      specVariables().map((variable) => variable.path),
    );
  });

  it("says where each variable works as the specs.html §5.4 table does", () => {
    const labels: Record<string, string> = {
      title: "title",
      description: "description",
      updateComment: "updateComment",
      supersedeComment: "supersedeComment",
    };
    const fromSpec = (worksIn: string): string[] =>
      worksIn === "all keys"
        ? ["title", "description", "updateComment", "supersedeComment"]
        : worksIn === "supersedeComment only"
          ? ["supersedeComment"]
          : worksIn.split(", ").map((key) => labels[key]);
    const spec = specVariables();
    for (const variable of TEMPLATE_VARIABLES) {
      const row = spec.find((candidate) => candidate.path === variable.path);
      expect(row, variable.path).toBeDefined();
      expect([...variable.worksIn], variable.path).toEqual(
        fromSpec((row as { worksIn: string }).worksIn),
      );
    }
  });

  it("builds the insertable syntax with braces", () => {
    expect(templateVariableSyntax(".Repository")).toBe("{{ .Repository }}");
  });
});
