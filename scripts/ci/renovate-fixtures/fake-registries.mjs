#!/usr/bin/env node
/**
 * Local stand-ins for the registries the lookup fixtures resolve against: a Docker registry, an
 * npm registry and a Go module proxy, each on its own port with fixed releases. Nothing here
 * reaches another host.
 *
 * Run: node fake-registries.mjs --ports-file <path> [--requests-file <path>]
 * It writes {"docker": <port>, "npm": <port>, "go": <port>} to the ports file once all three
 * listen, appends "<registry> <path>" to the requests file for every request, and runs until
 * it is killed.
 */
import { appendFileSync, writeFileSync } from "node:fs";
import { createServer } from "node:http";

/** The releases each fake serves; the fixtures' expectations are written against these. */
export const RELEASES = {
  docker: {
    "fixture/base": ["3.0.0", "3.1.0", "3.1.4"],
    "fixture/web": ["1.4.0", "1.5.0", "2.0.0"],
  },
  npm: {
    "fixture-lib": ["1.0.0", "1.0.1"],
    "fixture-dev-lib": ["2.3.0", "2.4.0"],
    "fixture-tool": ["3.2.0", "3.3.0"],
  },
  go: {
    "github.com/fixture-org/golib": ["v1.0.0", "v1.1.0", "v1.1.1"],
  },
};

const RELEASED = "2024-01-15T10:00:00.000Z";

function json(res, status, body) {
  res.writeHead(status, { "content-type": "application/json" });
  res.end(JSON.stringify(body));
}

function notFound(res) {
  json(res, 404, { message: "not found" });
}

/** The Docker registry HTTP API v2, with no authentication. */
export function dockerHandler(req, res) {
  const { pathname } = new URL(req.url, "http://fake");
  if (pathname === "/v2/" || pathname === "/v2") return json(res, 200, {});
  const tags = /^\/v2\/(.+)\/tags\/list$/.exec(pathname);
  if (tags && RELEASES.docker[tags[1]]) {
    return json(res, 200, { name: tags[1], tags: RELEASES.docker[tags[1]] });
  }
  return notFound(res);
}

/** The npm registry's package document route. */
export function npmHandler(req, res) {
  const name = decodeURIComponent(new URL(req.url, "http://fake").pathname.slice(1));
  const versions = RELEASES.npm[name];
  if (!versions) return notFound(res);
  const doc = { name, "dist-tags": { latest: versions.at(-1) }, versions: {}, time: { created: RELEASED, modified: RELEASED } };
  for (const v of versions) {
    doc.versions[v] = { name, version: v, dist: { tarball: `http://fake.invalid/${name}-${v}.tgz` } };
    doc.time[v] = RELEASED;
  }
  return json(res, 200, doc);
}

/** The Go module proxy protocol: @v/list, @v/<version>.info and @latest. */
export function goHandler(req, res) {
  const { pathname } = new URL(req.url, "http://fake");
  const route = /^\/(.+?)\/@(?:v\/(list)|v\/(.+)\.info|(latest))$/.exec(pathname);
  if (!route) return notFound(res);
  const versions = RELEASES.go[route[1]];
  if (!versions) return notFound(res);
  if (route[2]) {
    res.writeHead(200, { "content-type": "text/plain" });
    return res.end(`${versions.join("\n")}\n`);
  }
  const version = route[4] ? versions.at(-1) : route[3];
  if (!versions.includes(version)) return notFound(res);
  return json(res, 200, { Version: version, Time: RELEASED });
}

function listen(name, handler, log) {
  const server = createServer((req, res) => {
    log(`${name} ${req.url}`);
    handler(req, res);
  });
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "0.0.0.0", () => resolve(server.address().port));
  });
}

function arg(name) {
  const i = process.argv.indexOf(name);
  return i >= 0 ? process.argv[i + 1] : undefined;
}

if (process.argv[1] && import.meta.url === new URL(`file://${process.argv[1]}`).href) {
  const portsFile = arg("--ports-file");
  const requestsFile = arg("--requests-file");
  if (!portsFile) {
    console.error("usage: fake-registries.mjs --ports-file <path> [--requests-file <path>]");
    process.exit(2);
  }
  const log = (line) => {
    if (requestsFile) appendFileSync(requestsFile, `${line}\n`);
  };
  const ports = {
    docker: await listen("docker", dockerHandler, log),
    npm: await listen("npm", npmHandler, log),
    go: await listen("go", goHandler, log),
  };
  writeFileSync(portsFile, `${JSON.stringify(ports)}\n`);
}
