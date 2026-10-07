#!/usr/bin/env node
// Parses a security-audit report (.claude/skills/security-audit/SKILL.md, step 6)
// deterministically and refuses one that departs from the format.
//
//   node scripts/audit-report.mjs check <report>        validate only
//   node scripts/audit-report.mjs list <report>         one line per finding
//   node scripts/audit-report.mjs mark <report> <id> <RO-n|GHSA-…>
//                                                       record what a finding was filed as
//
// <report> is a path to report.md or a run id under ~/.local/state/release-ops-audit/.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const FRONT_KEYS = ['run_id', 'dev_sha', 'scope', 'units', 'date'];
const FINDING_KEYS = ['id', 'title', 'severity', 'type', 'label', 'withhold', 'verdict', 'files', 'related', 'invariant'];
const OPTIONAL_KEYS = ['cwe', 'filed'];
const SECTIONS = [
  'Summary',
  'Entry point and attacker',
  'Verified trace',
  'Impact and preconditions',
  'Fix direction',
  'Test to write first',
  'Verifier notes',
];
const TRAILING = ['Refuted candidates', 'Coverage', 'Threat-model gaps'];
const SEVERITIES = ['critical', 'high', 'medium', 'low', 'info'];
const TYPES = ['bug', 'chore', 'docs'];
const LABELS = ['backend', 'web', 'db', 'config', 'ci', 'docs', 'none'];
const VERDICTS = ['CONFIRMED', 'CONFIRMED-WITH-PRECONDITIONS'];

class ReportError extends Error {}

function fail(where, msg) {
  throw new ReportError(`${where}: ${msg}`);
}

function resolveReport(arg) {
  if (!arg) fail('arguments', 'no report given');
  if (fs.existsSync(arg)) return arg;
  const byRun = path.join(os.homedir(), '.local/state/release-ops-audit', arg, 'report.md');
  if (fs.existsSync(byRun)) return byRun;
  fail('arguments', `no report at ${arg} or ${byRun}`);
}

function stripComment(v) {
  if (v.startsWith('"')) return v;
  const i = v.search(/\s#/);
  return (i === -1 ? v : v.slice(0, i)).trim();
}

function parseScalar(raw, where) {
  const v = stripComment(raw.trim());
  if (v.startsWith('"')) {
    const m = v.match(/^"((?:[^"\\]|\\.)*)"\s*(#.*)?$/);
    if (!m) fail(where, `unterminated or malformed quoted string: ${v}`);
    return m[1].replace(/\\(["\\])/g, '$1');
  }
  if (v.startsWith('[')) {
    if (!v.endsWith(']')) fail(where, `malformed flow list: ${v}`);
    const inner = v.slice(1, -1).trim();
    return inner === '' ? [] : inner.split(',').map((s) => s.trim().replace(/^"(.*)"$/, '$1'));
  }
  if (v === 'true') return true;
  if (v === 'false') return false;
  return v;
}

function parseKeyBlock(lines, where) {
  const out = [];
  for (const line of lines) {
    if (line.trim() === '') continue;
    const m = line.match(/^([a-z_]+):\s?(.*)$/);
    if (!m) fail(where, `not a "key: value" line: ${line}`);
    out.push([m[1], parseScalar(m[2], `${where} key ${m[1]}`)]);
  }
  return out;
}

export function parseReport(text) {
  const lines = text.split('\n');
  let i = 0;
  if (lines[0] !== '---') fail('front matter', 'report must start with ---');
  const fmEnd = lines.indexOf('---', 1);
  if (fmEnd === -1) fail('front matter', 'no closing ---');
  const front = parseKeyBlock(lines.slice(1, fmEnd), 'front matter');
  const frontKeys = front.map(([k]) => k);
  if (frontKeys.join(',') !== FRONT_KEYS.join(',')) {
    fail('front matter', `keys must be exactly ${FRONT_KEYS.join(', ')} in order; got ${frontKeys.join(', ')}`);
  }
  const fm = Object.fromEntries(front);
  if (!/^[0-9a-f]{40}$/.test(fm.dev_sha)) fail('front matter', 'dev_sha must be a full 40-character SHA');
  i = fmEnd + 1;

  const h2 = [];
  for (let j = i; j < lines.length; j++) if (lines[j].startsWith('## ')) h2.push(j);
  if (h2.length === 0 || lines[h2[0]] !== '## Summary') fail('## Summary', 'first ## section must be "## Summary"');

  const findings = [];
  const trailingSeen = [];
  for (let n = 1; n < h2.length; n++) {
    const start = h2[n];
    const end = n + 1 < h2.length ? h2[n + 1] : lines.length;
    const heading = lines[start].slice(3);
    if (heading.startsWith('SA-')) {
      if (trailingSeen.length) fail(heading, 'finding section after a trailing section');
      findings.push(parseFinding(heading, lines.slice(start + 1, end), start + 1));
    } else if (TRAILING.includes(heading)) {
      trailingSeen.push(heading);
    } else {
      fail(`line ${start + 1}`, `unexpected ## section "${heading}"`);
    }
  }
  if (trailingSeen.join(',') !== TRAILING.join(',')) {
    fail('trailing sections', `must be exactly ${TRAILING.map((t) => `## ${t}`).join(', ')} in order`);
  }

  const summaryBody = lines.slice(h2[0] + 1, h2[1] ?? lines.length);
  const rows = summaryBody.filter((l) => /^\| SA-/.test(l));
  const ids = rows.map((r) => r.split('|')[1].trim());
  if (ids.join(',') !== findings.map((f) => f.id).join(',')) {
    fail('## Summary', 'table rows must list every finding id, in finding order');
  }
  if (!summaryBody.some((l) => /^\d+ findings?:/.test(l))) fail('## Summary', 'missing the totals line');

  return { front: fm, findings };
}

function parseFinding(heading, body, lineNo) {
  const where = heading.split(' — ')[0];
  const firstContent = body.findIndex((l) => l.trim() !== '');
  if (firstContent === -1 || body[firstContent] !== '```yaml') fail(where, 'heading must be followed by exactly one ```yaml block');
  const close = body.indexOf('```', firstContent + 1);
  if (close === -1) fail(where, 'unclosed yaml block');
  const kv = parseKeyBlock(body.slice(firstContent + 1, close), where);
  const keys = kv.map(([k]) => k);
  const expected = FINDING_KEYS.join(',');
  if (keys.slice(0, FINDING_KEYS.length).join(',') !== expected) {
    fail(where, `yaml keys must start with ${FINDING_KEYS.join(', ')} in order`);
  }
  const extra = keys.slice(FINDING_KEYS.length);
  const allowed = OPTIONAL_KEYS.filter((k) => extra.includes(k));
  if (extra.join(',') !== allowed.join(',')) fail(where, `only ${OPTIONAL_KEYS.join(', ')} may follow, in that order`);
  const f = Object.fromEntries(kv);
  if (!heading.startsWith(`${f.id} — `)) fail(where, 'heading id does not match the yaml id');
  if (!SEVERITIES.includes(f.severity)) fail(where, `bad severity ${f.severity}`);
  if (!TYPES.includes(f.type)) fail(where, `bad type ${f.type}`);
  if (!LABELS.includes(f.label)) fail(where, `bad label ${f.label}`);
  if (typeof f.withhold !== 'boolean') fail(where, 'withhold must be true or false');
  if (['critical', 'high'].includes(f.severity) && !f.withhold) fail(where, 'critical and high findings must be withhold: true');
  if (!VERDICTS.includes(f.verdict)) fail(where, `bad verdict ${f.verdict}`);
  for (const k of ['files', 'related']) if (!Array.isArray(f[k])) fail(where, `${k} must be a flow list`);
  if (f.cwe !== undefined && !Array.isArray(f.cwe)) fail(where, 'cwe must be a flow list');
  const h3 = body.slice(close + 1).filter((l) => l.startsWith('### ')).map((l) => l.slice(4));
  if (h3.join('|') !== SECTIONS.join('|')) fail(where, `### sections must be exactly: ${SECTIONS.join(', ')}`);
  f.line = lineNo;
  return f;
}

function main(argv) {
  const [cmd, ...rest] = argv;
  if (!['check', 'list', 'mark'].includes(cmd)) {
    console.error('usage: audit-report.mjs check|list <report> | mark <report> <id> <RO-n|GHSA-…>');
    return 2;
  }
  const file = resolveReport(rest[0]);
  const text = fs.readFileSync(file, 'utf8');
  const report = parseReport(text);

  if (cmd === 'check') {
    console.log(`ok: ${report.findings.length} findings in ${file}`);
    return 0;
  }
  if (cmd === 'list') {
    for (const f of report.findings) {
      const filed = f.filed ?? '-';
      console.log(`${f.id}\t${f.severity}\t${f.withhold ? 'withheld' : 'public'}\t${filed}\t${f.title}`);
    }
    return 0;
  }
  const [, id, value] = rest;
  if (!id || !value) fail('arguments', 'mark needs <id> and <value>');
  if (!/^(RO-\d+|GHSA-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4})$/.test(value)) fail('arguments', `bad filed value ${value}`);
  const f = report.findings.find((x) => x.id === id);
  if (!f) fail('arguments', `no finding ${id}`);
  if (f.withhold && value.startsWith('RO-')) fail(id, 'a withheld finding is never filed as a Kaneo task');
  if (!f.withhold && value.startsWith('GHSA-')) fail(id, 'a public finding is filed as a Kaneo task');
  const lines = text.split('\n');
  let j = f.line;
  while (lines[j] !== '```yaml') j++;
  let k = j + 1;
  while (lines[k] !== '```') k++;
  const filedLine = `filed: ${value.startsWith('RO-') ? `"${value}"` : value}`;
  const existing = lines.slice(j + 1, k).findIndex((l) => l.startsWith('filed:'));
  if (existing !== -1) lines[j + 1 + existing] = filedLine;
  else lines.splice(k, 0, filedLine);
  fs.writeFileSync(file, lines.join('\n'));
  parseReport(lines.join('\n'));
  console.log(`${id} filed as ${value}`);
  return 0;
}

try {
  process.exitCode = main(process.argv.slice(2));
} catch (e) {
  if (e instanceof ReportError) {
    console.error(`refused: ${e.message}`);
    process.exitCode = 1;
  } else {
    throw e;
  }
}
