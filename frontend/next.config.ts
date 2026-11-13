import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: "standalone",
  turbopack: { root: process.cwd() },
  experimental: { cpus: 2 },
};

export default nextConfig;
