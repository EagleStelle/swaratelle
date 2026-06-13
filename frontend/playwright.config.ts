import { defineConfig, devices } from "@playwright/test";

const isWindows = process.platform === "win32";

export default defineConfig({
  testDir: "./tests/e2e",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: isWindows
    ? [["html", { outputFolder: ".local/playwright-report", open: "never" }]]
    : "html",
  // Keep traces/screenshots/artifacts under .local/ on Windows; defaults on Linux.
  ...(isWindows ? { outputDir: ".local/playwright-results" } : {}),
  use: {
    baseURL: process.env.PLAYWRIGHT_BASE_URL ?? "http://localhost:3000",
    trace: "on-first-retry",
  },
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
  webServer: {
    command: "npm run dev",
    url: "http://localhost:3000",
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
});
