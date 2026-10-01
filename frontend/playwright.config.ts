import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests/cases",
  fullyParallel: false,
  workers: 1,
  use: { baseURL: "http://127.0.0.1:3100", headless: true, trace: "retain-on-failure",
    launchOptions: process.env.PLAYWRIGHT_BROWSER_PATH ? { executablePath: process.env.PLAYWRIGHT_BROWSER_PATH } : {},
  },
  webServer: [
    { command: "node tests/cases/service-fixture.mjs", url: "http://127.0.0.1:3199/health", reuseExistingServer: false },
    { command: "npm run start -- --hostname 127.0.0.1 --port 3100", url: "http://127.0.0.1:3100/login", reuseExistingServer: false,
      env: { CASE_MANAGER_SERVICE_URL: "http://127.0.0.1:3199", CASE_SESSION_COOKIE: "case_test_session", APP_ORIGIN: "http://127.0.0.1:3100", DECISION_ENGINE_SERVICE_URL: "http://127.0.0.1:3199", SCREENING_SERVICE_URL: "http://127.0.0.1:3199", DECISION_ENGINE_AUTH_TOKEN: "fixture-owner-token", SCREENING_AUTH_TOKEN: "fixture-owner-token" },
    },
  ],
});
