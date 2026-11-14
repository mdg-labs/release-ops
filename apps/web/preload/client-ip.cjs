"use strict";

const net = require("node:net");

const HEADER = "x-forwarded-for";
const REQUEST_EVENTS = new Set([
  "request",
  "upgrade",
  "checkContinue",
  "checkExpectation",
]);

function normalizeAddress(value) {
  if (typeof value !== "string") {
    return null;
  }
  let address = value.trim();
  const zone = address.indexOf("%");
  if (zone !== -1) {
    address = address.slice(0, zone);
  }
  const mapped = /^::ffff:(\d{1,3}(?:\.\d{1,3}){3})$/i.exec(address);
  if (mapped) {
    address = mapped[1];
  }
  return net.isIP(address) ? address : null;
}

function isTrusted(trusted, address) {
  return trusted.check(address, net.isIPv4(address) ? "ipv4" : "ipv6");
}

// Returns null when unset or blank (the TCP peer is the client) and throws on any
// entry that is not an IP address or CIDR range, so a typo never widens or silently
// drops the trust list.
function parseTrustedProxies(raw) {
  if (raw === undefined || raw.trim() === "") {
    return null;
  }
  const list = new net.BlockList();
  for (const part of raw.split(",")) {
    const entry = part.trim();
    const [address, prefixText, ...extra] = entry.split("/");
    const family = net.isIP(address);
    const maxPrefix = family === 4 ? 32 : 128;
    const prefix = prefixText === undefined ? maxPrefix : Number(prefixText);
    if (
      family === 0 ||
      extra.length > 0 ||
      (prefixText !== undefined && !/^\d{1,3}$/.test(prefixText)) ||
      prefix > maxPrefix
    ) {
      throw new Error(
        `TRUSTED_PROXIES: invalid entry "${entry}"; expected a comma-separated list of IP addresses or CIDR ranges`,
      );
    }
    list.addSubnet(address, prefix, family === 4 ? "ipv4" : "ipv6");
  }
  return list;
}

// The client address for a connection, or null when the socket has no usable address.
// Without a trusted list, or when the peer is not in it, the peer is the client. Behind a
// trusted peer it is the right-most X-Forwarded-For hop not in the list; a hop that is not
// an IP address, or a chain of only trusted hops, resolves to the peer.
function resolveClientIP(peerAddress, forwardedFor, trusted) {
  const peer = normalizeAddress(peerAddress);
  if (peer === null || trusted === null || !isTrusted(trusted, peer)) {
    return peer;
  }
  if (typeof forwardedFor !== "string") {
    return peer;
  }
  const hops = forwardedFor.split(",");
  for (let i = hops.length - 1; i >= 0; i--) {
    const hop = normalizeAddress(hops[i]);
    if (hop === null) {
      return peer;
    }
    if (!isTrusted(trusted, hop)) {
      return hop;
    }
  }
  return peer;
}

function overwriteForwardedFor(req, trusted) {
  const headers = req.headers;
  const client = resolveClientIP(
    req.socket && req.socket.remoteAddress,
    headers[HEADER],
    trusted,
  );

  if (client === null) {
    delete headers[HEADER];
  } else {
    headers[HEADER] = client;
  }

  const raw = req.rawHeaders;
  const kept = [];
  for (let i = 0; i < raw.length; i += 2) {
    if (raw[i].toLowerCase() !== HEADER) {
      kept.push(raw[i], raw[i + 1]);
    }
  }
  if (client !== null) {
    kept.push("X-Forwarded-For", client);
  }
  req.rawHeaders = kept;
}

// Wraps emit on a server prototype so every inbound request, upgrade and expectation
// handshake carries the connection's own address in X-Forwarded-For before any listener,
// Next.js included, reads the headers.
function install(serverPrototype, trusted) {
  const originalEmit = serverPrototype.emit;
  serverPrototype.emit = function emit(event, req, ...rest) {
    if (REQUEST_EVENTS.has(event) && req && req.headers) {
      overwriteForwardedFor(req, trusted);
    }
    return originalEmit.call(this, event, req, ...rest);
  };
}

module.exports = { parseTrustedProxies, resolveClientIP, install };
