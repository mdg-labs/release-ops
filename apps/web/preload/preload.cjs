"use strict";

const http = require("node:http");
const { install, parseTrustedProxies } = require("./client-ip.cjs");

let trusted;
try {
  trusted = parseTrustedProxies(process.env.TRUSTED_PROXIES);
} catch (error) {
  console.error(`release-ops: ${error.message}`);
  process.exit(1);
}

install(http.Server.prototype, trusted);
