import { describe, expect, it } from "vitest";
import { integrationDefaultTemplates } from "@/lib/ticket-projects/content-templates";
import type { TemplateField } from "@/lib/ticket-projects/content-templates";
import { renderTemplatePreview } from "@/lib/ticket-projects/template-preview";

function text(template: string, field: TemplateField = "title"): string {
  const result = renderTemplatePreview(template, field);
  if (!result.ok) {
    throw new Error(`no preview: ${result.reason}`);
  }
  return result.text;
}

describe("renderTemplatePreview", () => {
  it("renders the default title from the sample pull request", () => {
    expect(text(integrationDefaultTemplates("kaneo").title)).toBe(
      "Update dependency react to v19 (acme/web)",
    );
  });

  it("loops over the dependencies and prints the update type only when set", () => {
    expect(
      text(
        "{{ range .Dependencies }}- {{ .Name }}: {{ .From }} → {{ .To }}{{ if .UpdateType }} ({{ .UpdateType }}){{ end }}\n{{ end }}",
        "description",
      ),
    ).toBe(
      "- react: 18.3.1 → 19.0.0 (major)\n- typescript: 5.6.3 → 5.7.2 (minor)",
    );
  });

  it("renders the default description with the release notes", () => {
    const rendered = text(
      integrationDefaultTemplates("kaneo").description,
      "description",
    );
    expect(rendered).toContain("**Repository:** acme/web");
    expect(rendered).toContain(
      "[#42 Update dependency react to v19](https://github.com/acme/web/pull/42)",
    );
    expect(rendered).toContain("### Release Notes");
  });

  it("renders the update note and the supersede comment defaults", () => {
    expect(
      text(integrationDefaultTemplates("jira").updateComment, "updateComment"),
    ).toMatch(
      /^Pull request updated:\n- react: 18\.3\.1 → 19\.0\.0 \(major\)\n.*\nhttps:\/\/github\.com\/acme\/web\/pull\/42$/,
    );
    expect(
      text(
        integrationDefaultTemplates("kaneo").supersedeComment,
        "supersedeComment",
      ),
    ).toBe(
      "Superseded by https://github.com/acme/web/pull/43\nNew ticket: https://tracker.example.com/browse/WEB-101",
    );
  });

  it("honours else and trim markers", () => {
    expect(text("{{ if .Repository }}yes{{ else }}no{{ end }}")).toBe("yes");
    expect(text("a  {{- .Repository -}}  b")).toBe("aacme/webb");
  });

  it("gives no preview for a variable the field does not have", () => {
    expect(renderTemplatePreview("{{ .Release.Tag }}", "title")).toEqual({
      ok: false,
      reason: "unknownVariable",
    });
    expect(
      renderTemplatePreview("{{ .Supersede.NewTicketURL }}", "title"),
    ).toEqual({ ok: false, reason: "unknownVariable" });
    expect(
      renderTemplatePreview("{{ .ReleaseNotes }}", "supersedeComment"),
    ).toEqual({ ok: false, reason: "unknownVariable" });
  });

  it("gives no preview for syntax it does not evaluate", () => {
    for (const template of [
      "{{ .Repository | printf }}",
      "{{ yesNo true }}",
      "{{ if .Repository }}open",
      "{{ range .Repository }}x{{ end }}",
      "{{ .Dependencies }}",
      "{{ .Repository",
    ]) {
      expect(renderTemplatePreview(template, "title")).toMatchObject({
        ok: false,
      });
    }
  });
});
