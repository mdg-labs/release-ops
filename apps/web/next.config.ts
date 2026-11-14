import type { NextConfig } from "next";
import createNextIntlPlugin from "next-intl/plugin";

const withNextIntl = createNextIntlPlugin("./i18n/request.ts");

const nextConfig: NextConfig = {
  output: "standalone",
  // GitHub delivers webhooks to /hooks/github (WEBHOOK_URL); the Go API proxy handles them
  // with its webhook-only header list and body cap.
  async rewrites() {
    return [{ source: "/hooks/github", destination: "/api/go/hooks/github" }];
  },
  typescript: {
    ignoreBuildErrors: true,
  },
};

export default withNextIntl(nextConfig);
