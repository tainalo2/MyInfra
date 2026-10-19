import { defineConfig, devices } from "@playwright/test";

// The tests run against a MyInfra already started, empty, on E2E_URL.
export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [["github"], ["list"]] : "list",
  use: {
    baseURL: process.env.E2E_URL || "http://127.0.0.1:8080",
    trace: "retain-on-failure",
    // The texts checked below are the French ones; one test switches language.
    locale: "fr-FR",
  },
  // E2E_CHANNEL=chrome uses the Chrome already installed on the machine.
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"], channel: process.env.E2E_CHANNEL || undefined } }],
});
