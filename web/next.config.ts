import type { NextConfig } from "next";

const configuredAPI = process.env.API_PROXY_URL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://127.0.0.1:8080";
const apiURL = (/^https?:\/\//.test(configuredAPI) ? configuredAPI : `https://${configuredAPI}`).replace(/\/+$/, "");

const nextConfig: NextConfig = {
  experimental: { proxyTimeout: 1_260_000 },
  async rewrites() {
    return [{ source: "/api/:path*", destination: `${apiURL}/api/:path*` }];
  },
};

export default nextConfig;
