#!/usr/bin/env node
/**
 * A stand-in for the app's internal listener and for the GitHub API, for the worker run smoke
 * test: it hands out one renovate_run job, issues a job token that expires soon, takes the
 * heartbeats and the result, and answers every GitHub API request with 401, or never answers
 * it. The token is random, held in memory and never written to disk.
 *
 * Run: node fake-app.mjs --secret <worker secret> --state-dir <dir> --slot <n> --token-life <s>
 *                        [--github silent]
 * It writes its port to <dir>/port, the job's result body to <dir>/result.json and what it saw
 * to <dir>/meta.json.
 */
import { randomBytes } from "node:crypto";
import { createServer } from "node:http";
import { renameSync, writeFileSync } from "node:fs";
import { join } from "node:path";

function arg(name) {
  const i = process.argv.indexOf(name);
  return i >= 0 ? process.argv[i + 1] : undefined;
}

const secret = arg("--secret");
const stateDir = arg("--state-dir");
const slot = Number(arg("--slot") ?? "0");
const tokenLife = Number(arg("--token-life") ?? "600");
const silentGitHub = arg("--github") === "silent";
if (!secret || !stateDir) {
  console.error("usage: fake-app.mjs --secret <s> --state-dir <dir> [--slot <n>] [--token-life <s>] [--github silent]");
  process.exit(2);
}

const leaseOwner = `lease-${randomBytes(6).toString("hex")}`;
const job = {
  id: "job-smoke",
  kind: "renovate_run",
  repository_id: "repo-smoke",
  repository_full_name: "fixture-org/fixture-repo",
  priority: "normal",
  trigger: "run_now",
  attempt: 1,
  slot,
  lease_owner: leaseOwner,
  lease_expires_at: new Date(Date.now() + 3600e3).toISOString(),
  git_author: "release-ops[bot] <1+release-ops[bot]@users.noreply.github.com>",
};

let token = "";
let issued = false;
const meta = { claims: 0, tokenRequests: 0, heartbeats: 0, badAuth: 0, wrongLease: 0, githubRequests: [], tokenInResult: null };
const save = () => {
  writeFileSync(join(stateDir, "meta.tmp"), JSON.stringify(meta));
  renameSync(join(stateDir, "meta.tmp"), join(stateDir, "meta.json"));
};

function reply(res, status, body) {
  if (body === undefined) {
    res.writeHead(status);
    return res.end();
  }
  res.writeHead(status, { "content-type": "application/json" });
  res.end(JSON.stringify(body));
}

function readBody(req) {
  return new Promise((resolve) => {
    const chunks = [];
    req.on("data", (c) => chunks.push(c));
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf8")));
  });
}

const server = createServer(async (req, res) => {
  const path = new URL(req.url, "http://fake").pathname;

  if (path.startsWith("/api/v3")) {
    const auth = req.headers.authorization ?? "";
    meta.githubRequests.push({ path, authorized: token !== "" && auth.includes(token) });
    save();
    if (silentGitHub) return; // the connection stays open until Renovate is stopped
    return reply(res, 401, { message: "Bad credentials" });
  }

  if (!path.startsWith("/internal/v1/jobs/")) return reply(res, 404, { message: "not found" });
  if (req.headers.authorization !== `Bearer ${secret}`) {
    meta.badAuth++;
    save();
    return reply(res, 401, { message: "unauthorized" });
  }
  const raw = await readBody(req);
  let body = {};
  try {
    body = raw ? JSON.parse(raw) : {};
  } catch {
    return reply(res, 400, { message: "bad json" });
  }

  if (path === "/internal/v1/jobs/claim") {
    meta.claims++;
    save();
    if (!issued) {
      issued = true;
      return reply(res, 200, job);
    }
    setTimeout(() => reply(res, 204), 2000);
    return;
  }

  const route = /^\/internal\/v1\/jobs\/([^/]+)\/(token|heartbeat|result|release)$/.exec(path);
  if (!route || route[1] !== job.id) return reply(res, 404, { message: "not found" });
  if (body.lease_owner !== leaseOwner) {
    meta.wrongLease++;
    save();
    return reply(res, 409, { message: "lease not held" });
  }
  switch (route[2]) {
    case "token":
      meta.tokenRequests++;
      token = `ghs_${randomBytes(20).toString("hex")}`;
      save();
      return reply(res, 200, { token, expires_at: new Date(Date.now() + tokenLife * 1000).toISOString() });
    case "heartbeat":
      meta.heartbeats++;
      save();
      return reply(res, 200, { abort: false });
    case "result":
      meta.tokenInResult = token !== "" && raw.includes(token);
      writeFileSync(join(stateDir, "result.tmp"), raw);
      renameSync(join(stateDir, "result.tmp"), join(stateDir, "result.json"));
      save();
      return reply(res, 204);
    default:
      return reply(res, 204);
  }
});

server.listen(0, "0.0.0.0", () => {
  save();
  writeFileSync(join(stateDir, "port.tmp"), `${server.address().port}\n`);
  renameSync(join(stateDir, "port.tmp"), join(stateDir, "port"));
});
