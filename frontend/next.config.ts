import type { NextConfig } from "next";

// On Windows (local dev) send the exported static site to .local/out so it does
// not pollute the source tree. On Linux (Docker build) use defaults so the
// Dockerfile can copy frontend/out as usual.
//
// Note: with output:"export" + a custom distDir, Next uses distDir as the
// EXPORT output dir and force-pins the intermediate build dir to .next
// (see next/dist/build/index.js). So on Windows .next still appears at the
// frontend root; it is throwaway and gitignored. The meaningful output lands
// in .local/out.
const isWindows = process.platform === "win32";

const nextConfig: NextConfig = {
  output: "export",
  ...(isWindows ? { distDir: ".local/out" } : {}),
};

export default nextConfig;
