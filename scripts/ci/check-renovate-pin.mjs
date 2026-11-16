#!/usr/bin/env node
/**
 * Fails when the worker stage of the Dockerfile does not name Renovate by an exact version and
 * a digest, or when the WORKER_RENOVATE_TAG default names another version than that tag.
 *
 * Run: node scripts/ci/check-renovate-pin.mjs [Dockerfile]
 * Prints the pinned tag and digest as "<tag> <digest>" on success.
 */
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const IMAGE = /^(?:ghcr\.io\/renovatebot|renovate)\/renovate:(\d+\.\d+\.\d+)@(sha256:[0-9a-f]{64})$/;

/** The problems with the Renovate pin in a Dockerfile's text; none means it is pinned. */
export function checkPin(dockerfile) {
  const problems = [];
  const lines = dockerfile.split("\n").map((l) => l.trim());

  const renovateRefs = lines.filter((l) => /^FROM\s/i.test(l) && /renovate/i.test(l));
  const worker = lines.filter((l) => /^FROM\s+\S+\s+AS\s+worker$/i.test(l));
  if (worker.length !== 1) {
    return [`expected one "FROM <image> AS worker" line, found ${worker.length}`];
  }
  const image = worker[0].split(/\s+/)[1];
  const match = IMAGE.exec(image);
  if (!match) {
    problems.push(
      `the worker stage is built on "${image}"; it must be ghcr.io/renovatebot/renovate:<x.y.z>@sha256:<64 hex digits>, a literal tag and digest`,
    );
  }
  if (renovateRefs.length !== 1) {
    problems.push(`the Dockerfile has ${renovateRefs.length} FROM lines naming Renovate, expected one`);
  }

  const arg = /^ARG WORKER_RENOVATE_TAG=(\S+)$/m.exec(dockerfile);
  if (!arg) {
    problems.push("the Dockerfile has no ARG WORKER_RENOVATE_TAG default");
  } else if (match && arg[1] !== match[1]) {
    problems.push(
      `ARG WORKER_RENOVATE_TAG is ${arg[1]} but the worker stage is pinned to ${match[1]}; set both, and RenovateVersion in internal/buildinfo, to the same version`,
    );
  }
  return problems;
}

/** The pinned tag and digest of a Dockerfile that passes checkPin. */
export function pinned(dockerfile) {
  const line = dockerfile.split("\n").find((l) => /^FROM\s+\S+\s+AS\s+worker\s*$/i.test(l.trim()));
  const match = IMAGE.exec(line.trim().split(/\s+/)[1]);
  return { tag: match[1], digest: match[2] };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const root = join(dirname(fileURLToPath(import.meta.url)), "../..");
  const path = process.argv[2] ?? join(root, "Dockerfile");
  const text = readFileSync(path, "utf8");
  const problems = checkPin(text);
  if (problems.length > 0) {
    for (const p of problems) console.error(`check-renovate-pin: ${p}`);
    process.exit(1);
  }
  const { tag, digest } = pinned(text);
  console.log(`${tag} ${digest}`);
}
