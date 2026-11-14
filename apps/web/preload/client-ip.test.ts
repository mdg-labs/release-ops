// @vitest-environment node
import { spawnSync } from "node:child_process";
import http from "node:http";
import type { AddressInfo } from "node:net";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { install, parseTrustedProxies, resolveClientIP } from "./client-ip.cjs";

const PRELOAD = path.join(__dirname, "preload.cjs");

describe("parseTrustedProxies", () => {
  it("returns null when unset or blank", () => {
    expect(parseTrustedProxies(undefined)).toBeNull();
    expect(parseTrustedProxies("")).toBeNull();
    expect(parseTrustedProxies("  ")).toBeNull();
  });

  it("accepts IPs and CIDR ranges of both families", () => {
    expect(
      parseTrustedProxies("10.0.0.0/8, 172.16.0.1,fd00::/8 , ::1"),
    ).not.toBeNull();
  });

  it.each([
    "nonsense",
    "10.0.0.0/33",
    "::1/129",
    "10.0.0.0/8/1",
    "10.0.0.0/",
    "10.0.0.0/x",
    "10.0.0.1,",
    "10.0.0.1,,10.0.0.2",
    "10.0.0.256",
  ])("throws on %j", (value) => {
    expect(() => parseTrustedProxies(value)).toThrow(/TRUSTED_PROXIES/);
  });
});

describe("resolveClientIP", () => {
  const trusted = parseTrustedProxies("10.0.0.0/8, 192.168.1.1");

  it("uses the peer and ignores the header without a trusted list", () => {
    expect(resolveClientIP("203.0.113.7", "198.51.100.1", null)).toBe(
      "203.0.113.7",
    );
  });

  it("unwraps an IPv4-mapped peer", () => {
    expect(resolveClientIP("::ffff:203.0.113.7", undefined, null)).toBe(
      "203.0.113.7",
    );
  });

  it("returns null for a missing or unparseable peer address", () => {
    expect(resolveClientIP(undefined, "198.51.100.1", trusted)).toBeNull();
    expect(resolveClientIP("not-an-ip", "198.51.100.1", null)).toBeNull();
  });

  it("uses a peer outside the trusted list as-is", () => {
    expect(resolveClientIP("203.0.113.7", "198.51.100.1", trusted)).toBe(
      "203.0.113.7",
    );
  });

  it("takes the right-most hop not in the list behind a trusted peer", () => {
    expect(
      resolveClientIP(
        "10.0.0.5",
        "6.6.6.6, 198.51.100.1, 192.168.1.1, 10.1.2.3",
        trusted,
      ),
    ).toBe("198.51.100.1");
  });

  it("falls back to the peer when the chain has no untrusted hop or no header", () => {
    expect(resolveClientIP("10.0.0.5", "192.168.1.1, 10.1.2.3", trusted)).toBe(
      "10.0.0.5",
    );
    expect(resolveClientIP("10.0.0.5", undefined, trusted)).toBe("10.0.0.5");
  });

  it("falls back to the peer on a hop that is not an IP address", () => {
    expect(resolveClientIP("10.0.0.5", "198.51.100.1, garbage", trusted)).toBe(
      "10.0.0.5",
    );
  });
});

describe("install on a real HTTP server", () => {
  let server: http.Server | undefined;

  afterEach(async () => {
    await new Promise((resolve) => {
      if (server) {
        server.close(resolve);
        server.closeAllConnections();
      } else {
        resolve(undefined);
      }
    });
    server = undefined;
  });

  async function observe(
    trusted: ReturnType<typeof parseTrustedProxies>,
    sent: string[],
  ): Promise<{ header: unknown; raw: string[] }> {
    class TestServer extends http.Server {}
    install(TestServer.prototype, trusted);

    server = new TestServer((req, res) => {
      const raw = req.rawHeaders.filter(
        (_, i) => i % 2 === 0 && _.toLowerCase() === "x-forwarded-for",
      );
      res.setHeader("content-type", "application/json");
      res.end(JSON.stringify({ header: req.headers["x-forwarded-for"], raw }));
    });
    await new Promise<void>((resolve) =>
      server!.listen(0, "127.0.0.1", resolve),
    );
    const { port } = server.address() as AddressInfo;

    return new Promise((resolve, reject) => {
      const req = http.request(
        { host: "127.0.0.1", port, path: "/", agent: false },
        (res) => {
          let body = "";
          res.on("data", (chunk) => (body += chunk));
          res.on("end", () => resolve(JSON.parse(body)));
        },
      );
      req.on("error", reject);
      for (const value of sent) {
        req.setHeader("x-forwarded-for", value);
      }
      req.end();
    });
  }

  it("replaces a spoofed header with the socket address before the listener runs", async () => {
    const seen = await observe(null, ["198.51.100.99, 6.6.6.6"]);
    expect(seen.header).toBe("127.0.0.1");
    expect(seen.raw).toEqual(["X-Forwarded-For"]);
  });

  it("sets the header when the client sent none", async () => {
    const seen = await observe(null, []);
    expect(seen.header).toBe("127.0.0.1");
  });

  it("takes the right hop when the peer is a trusted proxy", async () => {
    const seen = await observe(parseTrustedProxies("127.0.0.0/8"), [
      "198.51.100.7",
    ]);
    expect(seen.header).toBe("198.51.100.7");
  });

  it("uses the peer as-is when it is outside the trusted list", async () => {
    const seen = await observe(parseTrustedProxies("10.0.0.0/8"), [
      "198.51.100.7",
    ]);
    expect(seen.header).toBe("127.0.0.1");
  });
});

describe("the preload module", () => {
  const probe = `
    const http = require("node:http");
    const server = http.createServer((req, res) => {
      res.end(String(req.headers["x-forwarded-for"]));
    });
    server.listen(0, "127.0.0.1", async () => {
      const { port } = server.address();
      const res = await fetch("http://127.0.0.1:" + port + "/", {
        headers: { "x-forwarded-for": "198.51.100.99" },
      });
      process.stdout.write(await res.text());
      server.close();
      server.closeAllConnections();
    });
  `;

  function run(trustedProxies: string | undefined) {
    const env = { ...process.env };
    delete env.TRUSTED_PROXIES;
    if (trustedProxies !== undefined) {
      env.TRUSTED_PROXIES = trustedProxies;
    }
    return spawnSync(process.execPath, ["--require", PRELOAD, "-e", probe], {
      env,
      encoding: "utf8",
      timeout: 20_000,
    });
  }

  it("overwrites the header on http.createServer when loaded with --require", () => {
    const result = run(undefined);
    expect(result.status).toBe(0);
    expect(result.stdout).toBe("127.0.0.1");
  });

  it("honours TRUSTED_PROXIES from the environment", () => {
    const result = run("127.0.0.1");
    expect(result.status).toBe(0);
    expect(result.stdout).toBe("198.51.100.99");
  });

  it("stops the process at startup on a malformed TRUSTED_PROXIES", () => {
    const result = run("10.0.0.0/99");
    expect(result.status).toBe(1);
    expect(result.stderr).toContain("TRUSTED_PROXIES");
    expect(result.stdout).toBe("");
  });
});
