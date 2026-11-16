import type { TemplateField } from "@/lib/ticket-projects/content-templates";

type Value =
  string | number | boolean | undefined | Value[] | { [key: string]: Value };

type Node =
  | { kind: "text"; text: string }
  | { kind: "print"; path: string[] }
  | { kind: "if"; path: string[]; then: Node[]; otherwise: Node[] }
  | { kind: "range"; path: string[]; body: Node[]; otherwise: Node[] };

/** Why a preview cannot be shown; the server's validation on save stays authoritative. */
export type PreviewResult =
  | { ok: true; text: string }
  | { ok: false; reason: "unsupported" | "unknownVariable" };

class PreviewError extends Error {
  readonly reason: "unsupported" | "unknownVariable";

  constructor(reason: "unsupported" | "unknownVariable") {
    super(reason);
    this.reason = reason;
  }
}

const SAMPLE_PULL_REQUEST = {
  URL: "https://github.com/acme/web/pull/42",
  Number: 42,
  Title: "Update dependency react to v19",
};

/**
 * The sample context a field renders from, shaped like the Go context of
 * internal/tickettemplate/context.go: .ReleaseNotes exists in title, description and update
 * comment, .Supersede in the supersede comment only (docs/specs.html §5.4).
 */
export function sampleContext(field: TemplateField): Value {
  const context: { [key: string]: Value } = {
    PullRequest: SAMPLE_PULL_REQUEST,
    Repository: "acme/web",
    Dependencies: [
      { Name: "react", From: "18.3.1", To: "19.0.0", UpdateType: "major" },
      {
        Name: "typescript",
        From: "5.6.3",
        To: "5.7.2",
        UpdateType: "minor",
      },
    ],
  };
  if (field === "supersedeComment") {
    context.Supersede = {
      NewPullRequest: {
        URL: "https://github.com/acme/web/pull/43",
        Number: 43,
        Title: "Update dependency react to v19.0.1",
      },
      NewTicketURL: "https://tracker.example.com/browse/WEB-101",
    };
  } else {
    context.ReleaseNotes =
      "### Release Notes\n\n- Fixed a hydration warning\n- Improved error messages";
  }
  return context;
}

// A field path (`.A.B`, or `.` for the dot). Pipelines, functions, variables and every other
// action are left to the server.
function parsePath(expression: string): string[] {
  if (expression === ".") {
    return [];
  }
  if (!/^(\.[A-Za-z_][A-Za-z0-9_]*)+$/.test(expression)) {
    throw new PreviewError("unsupported");
  }
  return expression.slice(1).split(".");
}

type Token = { text: string } | { action: string };

function tokenize(source: string): Token[] {
  const tokens: Token[] = [];
  const pattern = /\{\{(-\s)?([\s\S]*?)(\s-)?\}\}/g;
  let last = 0;
  let trimNext = false;
  for (const match of source.matchAll(pattern)) {
    let text = source.slice(last, match.index);
    if (trimNext) {
      text = text.trimStart();
    }
    if (match[1]) {
      text = text.trimEnd();
    }
    if (text) {
      tokens.push({ text });
    }
    tokens.push({ action: match[2].trim() });
    trimNext = Boolean(match[3]);
    last = match.index + match[0].length;
  }
  let rest = source.slice(last);
  if (trimNext) {
    rest = rest.trimStart();
  }
  if (rest.includes("{{") || rest.includes("}}")) {
    throw new PreviewError("unsupported");
  }
  if (rest) {
    tokens.push({ text: rest });
  }
  return tokens;
}

function parse(tokens: Token[]): Node[] {
  let position = 0;

  function parseList(): { nodes: Node[]; ended: "end" | "else" | "eof" } {
    const nodes: Node[] = [];
    while (position < tokens.length) {
      const token = tokens[position++];
      if ("text" in token) {
        nodes.push({ kind: "text", text: token.text });
        continue;
      }
      const action = token.action;
      if (action === "end") {
        return { nodes, ended: "end" };
      }
      if (action === "else") {
        return { nodes, ended: "else" };
      }
      const block = /^(if|range)\s+(\S+)$/.exec(action);
      if (block) {
        const path = parsePath(block[2]);
        const body = parseList();
        let otherwise: Node[] = [];
        if (body.ended === "else") {
          const alternative = parseList();
          if (alternative.ended !== "end") {
            throw new PreviewError("unsupported");
          }
          otherwise = alternative.nodes;
        } else if (body.ended !== "end") {
          throw new PreviewError("unsupported");
        }
        nodes.push(
          block[1] === "if"
            ? { kind: "if", path, then: body.nodes, otherwise }
            : { kind: "range", path, body: body.nodes, otherwise },
        );
        continue;
      }
      nodes.push({ kind: "print", path: parsePath(action) });
    }
    return { nodes, ended: "eof" };
  }

  const root = parseList();
  if (root.ended !== "eof") {
    throw new PreviewError("unsupported");
  }
  return root.nodes;
}

function lookup(dot: Value, path: string[]): Value {
  let value = dot;
  for (const key of path) {
    if (
      typeof value !== "object" ||
      value === null ||
      Array.isArray(value) ||
      !(key in value)
    ) {
      throw new PreviewError("unknownVariable");
    }
    value = value[key];
  }
  return value;
}

function truthy(value: Value): boolean {
  if (Array.isArray(value)) {
    return value.length > 0;
  }
  return Boolean(value);
}

function evaluate(nodes: Node[], dot: Value): string {
  let out = "";
  for (const node of nodes) {
    switch (node.kind) {
      case "text":
        out += node.text;
        break;
      case "print": {
        const value = lookup(dot, node.path);
        if (typeof value === "object") {
          throw new PreviewError("unsupported");
        }
        out += String(value);
        break;
      }
      case "if":
        out += evaluate(
          truthy(lookup(dot, node.path)) ? node.then : node.otherwise,
          dot,
        );
        break;
      case "range": {
        const items = lookup(dot, node.path);
        if (!Array.isArray(items)) {
          throw new PreviewError("unsupported");
        }
        if (items.length === 0) {
          out += evaluate(node.otherwise, dot);
        }
        for (const item of items) {
          out += evaluate(node.body, item);
        }
        break;
      }
    }
  }
  return out;
}

/**
 * Renders a Go text/template of the ticket content with sample data, for the form's preview.
 * It covers the field paths, if/else and range/else the default templates use; anything else
 * (functions, pipelines, variables) gives no preview rather than a wrong one. Saving is
 * always checked by the server.
 */
export function renderTemplatePreview(
  template: string,
  field: TemplateField,
): PreviewResult {
  try {
    const nodes = parse(tokenize(template));
    return {
      ok: true,
      text: evaluate(nodes, sampleContext(field)).replace(/\n+$/, ""),
    };
  } catch (error) {
    if (error instanceof PreviewError) {
      return { ok: false, reason: error.reason };
    }
    throw error;
  }
}
