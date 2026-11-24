import { generateKeyPairSync } from "node:crypto";
import { mkdtempSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { defineConfig } from "@playwright/test";

const fixtureKeys=generateKeyPairSync("rsa",{modulusLength:2048,privateKeyEncoding:{type:"pkcs8",format:"pem"},publicKeyEncoding:{type:"spki",format:"pem"}});
const fixtureDirectory=mkdtempSync(path.join(os.tmpdir(),"fraud-browser-identity-"));
const fixtureKey=path.join(fixtureDirectory,"bff.pem");writeFileSync(fixtureKey,fixtureKeys.privateKey,{mode:0o600});
export default defineConfig({
  testDir: "./tests/cases",
  fullyParallel: false,
  workers: 1,
  use: { baseURL: "http://127.0.0.1:3100", headless: true, trace: "retain-on-failure",
    launchOptions: process.env.PLAYWRIGHT_BROWSER_PATH ? { executablePath: process.env.PLAYWRIGHT_BROWSER_PATH } : {},
  },
  webServer: [
    { command: "node tests/cases/service-fixture.mjs", url: "http://127.0.0.1:3199/health", reuseExistingServer: false, env: { TEST_BFF_PUBLIC_KEY: fixtureKeys.publicKey } },
    { command: "npm run start -- --hostname 127.0.0.1 --port 3100", url: "http://127.0.0.1:3100/login", reuseExistingServer: false,
      env: {
        PLATFORM_AUTH_SIGNING_KEY_FILE: fixtureKey, PLATFORM_AUTH_SIGNING_KEY_ID:"fixture-bff", PLATFORM_AUTH_SERVICE_ID:"frontend-bff",
        DATA_MODEL_SERVICE_URL:"http://127.0.0.1:3199", AUTH_SERVICE_URL:"http://127.0.0.1:3199", CASE_MANAGER_SERVICE_URL:"http://127.0.0.1:3199", CASE_SESSION_COOKIE:"case_test_session", AUTH_COOKIE_SECURE:"false", AUTH_MANAGE_SIGN_IN_URL:"http://127.0.0.1:3199/provider-account", APP_ORIGIN:"http://127.0.0.1:3100", DECISION_ENGINE_SERVICE_URL:"http://127.0.0.1:3199", SCREENING_SERVICE_URL:"http://127.0.0.1:3199",
      },
    },
  ],
});
