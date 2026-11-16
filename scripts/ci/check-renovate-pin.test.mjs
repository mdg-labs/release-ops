import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

import { checkPin, pinned } from "./check-renovate-pin.mjs";

const digest = `sha256:${"a".repeat(64)}`;
const good = `ARG WORKER_RENOVATE_TAG=44.239.1

FROM node:22-bookworm-slim AS web-deps

FROM ghcr.io/renovatebot/renovate:44.239.1@${digest} AS worker
ARG WORKER_RENOVATE_TAG
`;

test("the repository's Dockerfile is pinned", () => {
  const root = join(dirname(fileURLToPath(import.meta.url)), "../..");
  assert.deepEqual(checkPin(readFileSync(join(root, "Dockerfile"), "utf8")), []);
});

test("a tag and digest pass and are read back", () => {
  assert.deepEqual(checkPin(good), []);
  assert.deepEqual(pinned(good), { tag: "44.239.1", digest });
});

test("the Docker Hub image is accepted too", () => {
  assert.deepEqual(checkPin(good.replace("ghcr.io/renovatebot/renovate", "renovate/renovate")), []);
});

test("a floating tag fails", () => {
  for (const tag of ["latest", "44", "44.239", "slim", "44.239.1-full"]) {
    const text = good.replace(`renovate:44.239.1@${digest}`, `renovate:${tag}@${digest}`);
    assert.notDeepEqual(checkPin(text), [], tag);
  }
});

test("a missing or malformed digest fails", () => {
  assert.notDeepEqual(checkPin(good.replace(`@${digest}`, "")), []);
  assert.notDeepEqual(checkPin(good.replace(digest, `sha256:${"a".repeat(63)}`)), []);
  assert.notDeepEqual(checkPin(good.replace(digest, `sha256:${"G".repeat(64)}`)), []);
  assert.notDeepEqual(checkPin(good.replace(digest, `sha512:${"a".repeat(64)}`)), []);
});

test("a tag taken from a build argument fails", () => {
  const text = good.replace(`renovate:44.239.1@${digest}`, `renovate:\${WORKER_RENOVATE_TAG}@${digest}`);
  assert.notDeepEqual(checkPin(text), []);
});

test("another registry or image fails", () => {
  assert.notDeepEqual(checkPin(good.replace("ghcr.io/renovatebot/renovate", "example.com/renovate")), []);
});

test("an ARG default that names another version fails", () => {
  const problems = checkPin(good.replace("ARG WORKER_RENOVATE_TAG=44.239.1", "ARG WORKER_RENOVATE_TAG=44.239.0"));
  assert.equal(problems.length, 1);
  assert.match(problems[0], /WORKER_RENOVATE_TAG/);
});

test("a missing ARG default fails", () => {
  assert.notDeepEqual(checkPin(good.replace("ARG WORKER_RENOVATE_TAG=44.239.1\n", "")), []);
});

test("a second Renovate stage fails", () => {
  const text = `${good}\nFROM ghcr.io/renovatebot/renovate:44.239.1@${digest} AS other\n`;
  assert.notDeepEqual(checkPin(text), []);
});

test("no worker stage fails", () => {
  assert.notDeepEqual(checkPin(good.replace("AS worker", "AS other")), []);
});
