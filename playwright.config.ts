import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "tests/browser",
  fullyParallel: false,
  workers: 1,
  timeout: 60000,
  use: {
    baseURL: process.env.PP_BASE_URL ?? "http://localhost:5173",
    channel: process.env.PP_BROWSER_CHANNEL,
    trace: "off",
    screenshot: "only-on-failure",
  },
  reporter: [["list"], ["json", { outputFile: "test-results/browser.json" }]],
});
