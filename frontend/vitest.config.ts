import { resolve } from "node:path";

import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

const isWindows = process.platform === "win32";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": resolve(__dirname, "."),
    },
  },
  // Keep the Vite/Vitest cache under .local/ on Windows; default on Linux.
  ...(isWindows ? { cacheDir: ".local/vitest" } : {}),
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./vitest.setup.ts"],
    include: ["tests/unit/**/*.{test,spec}.{ts,tsx}"],
    // Keep generated coverage under .local/ on Windows; default on Linux.
    ...(isWindows
      ? { coverage: { reportsDirectory: ".local/coverage" } }
      : {}),
  },
});
