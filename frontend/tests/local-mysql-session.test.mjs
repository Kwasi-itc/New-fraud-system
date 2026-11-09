import assert from "node:assert/strict";
import { test } from "node:test";
import { localMySQLSession } from "../src/lib/server/local-mysql-session.ts";

const config = { LOCAL_MYSQL_NO_AUTH: "1", APP_ORIGIN: "http://localhost:3001", NEXT_PUBLIC_DATA_MODEL_TENANT_ID: "11111111-1111-4111-8111-111111111111" };
test("authentication remains enabled unless explicitly disabled for local MySQL", () => {
  assert.equal(localMySQLSession({}), null);
  assert.throws(() => localMySQLSession({ ...config, LOCAL_MYSQL_NO_AUTH: "true" }));
});
test("local identity is fixed to configured tenant and excludes case/screening roles", () => {
  const local = localMySQLSession(config);
  assert.ok(local);
  assert.equal(local.session.tenant_id, config.NEXT_PUBLIC_DATA_MODEL_TENANT_ID);
  assert.equal(local.access_token, "local-mysql-development-only");
  assert.equal(local.session.roles.includes("case_investigator"), false);
});
test("local mode refuses remote origins and missing tenant configuration", () => {
  for (const APP_ORIGIN of ["https://example.com", "http://0.0.0.0:3001", "http://localhost.example.com", "http://user@localhost"]) {
    assert.throws(() => localMySQLSession({ ...config, APP_ORIGIN }));
  }
  assert.throws(() => localMySQLSession({ ...config, NEXT_PUBLIC_DATA_MODEL_TENANT_ID: "" }));
});
