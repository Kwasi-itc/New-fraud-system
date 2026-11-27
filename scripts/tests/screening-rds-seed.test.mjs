import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { datasets, readRdsConfig, seedRds } from "../lib/screening-rds-seed.mjs";

const database = "postgres://operator:secret@rds.example:5432/fraud?sslmode=require";
const image = `sha256:${"a".repeat(64)}`;
const generation = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";

function fixture(t) {
  const root = mkdtempSync(path.join(os.tmpdir(), "screening-seed-test-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const envFile = path.join(root, "compose.env");
  writeFileSync(envFile, `DATABASE_URL='${database}'\nCOMPOSE_PROJECT_NAME='my-project'\n`);
  for (const dataset of datasets) {
    writeFileSync(path.join(root, dataset.file), JSON.stringify({
      source: dataset.id, parser: "example-v1", records: [{ id: "one" }],
    }));
  }
  return { root, envFile };
}

function dockerMock({ currentDb = database, unapproved, failImport, badVerification } = {}) {
  const calls = [];
  let secretFile;
  const spawn = (executable, args) => {
    assert.equal(executable, "docker");
    calls.push(args);
    if (args[0] === "ps") return { status: 0, stdout: "container-id\n" };
    if (args[0] === "inspect") return { status: 0, stdout: JSON.stringify({ Image: image, Config: { Env: [`DATABASE_URL=${currentDb}`] } }) };
    if (args[0] === "image") return { status: 0 };
    assert.equal(args[0], "run");
    secretFile = args[args.indexOf("--env-file") + 1];
    assert.equal(statSync(secretFile).mode & 0o777, 0o600);
    const env = readFileSync(secretFile, "utf8");
    assert.match(env, new RegExp(`SOURCE_DATABASE_URL=${database.replace(/[?]/g, "\\?")}`));
    assert.match(env, /PGHOST=rds\.example\n/);
    assert.match(env, /PGDATABASE=fraud\n/);
    assert.match(env, /PGUSER=operator\n/);
    assert.match(env, /PGPASSWORD=secret\n/);
    assert.match(env, /PGSSLMODE=require\n/);
    assert.ok(!args.some(arg => arg.includes(database)));
    if (args.includes("psql")) {
      const verification = args.at(-1).includes("source_versions");
      return { status: 0, stdout: JSON.stringify(datasets.map(dataset => verification
        ? { id: dataset.id, generation, count: dataset.id === badVerification ? 0 : 1 }
        : { id: dataset.id, connector: dataset.id, parser: "example-v1",
          approved: dataset.id !== unapproved, usable: true, grants: 1 })) };
    }
    if (args.includes(failImport)) return { status: 1, stderr: "publication refused" };
    return { status: 0, stdout: JSON.stringify({ generation, unchanged: false }) };
  };
  return { spawn, calls, getSecretFile: () => secretFile };
}

test("RDS publication uses the active project, approved sources and a private temporary env file", t => {
  const { root, envFile } = fixture(t);
  const mock = dockerMock();
  seedRds({ root, envFile, spawn: mock.spawn });
  assert.ok(mock.calls[0].includes("label=com.docker.compose.project=my-project"));
  const imports = mock.calls.filter(args => args.includes("/app/source-import"));
  assert.equal(imports.length, datasets.length);
  assert.ok(imports.every(args => args.includes(image) && args.includes("--pull=never")));
  assert.ok(imports.every(args => args.some(arg => arg.endsWith(",dst=/data/snapshot.json,readonly"))));
  assert.throws(() => statSync(mock.getSecretFile()), { code: "ENOENT" });
});

test("RDS publication stops before writes for mismatched DB, unapproved sources or missing snapshots", t => {
  const { root, envFile } = fixture(t);
  const mismatch = dockerMock({ currentDb: "postgres://another/database" });
  assert.throws(() => seedRds({ root, envFile, spawn: mismatch.spawn }), /does not match/);
  assert.equal(mismatch.calls.filter(args => args[0] === "run").length, 0);

  const unapproved = dockerMock({ unapproved: datasets[1].id });
  assert.throws(() => seedRds({ root, envFile, spawn: unapproved.spawn }), /missing, unapproved, or incompatible/);
  assert.equal(unapproved.calls.filter(args => args.includes("/app/source-import")).length, 0);
  assert.throws(() => statSync(unapproved.getSecretFile()), { code: "ENOENT" });

  writeFileSync(path.join(root, datasets[0].file), JSON.stringify({ source: datasets[0].id, parser: "example-v1", records: [] }));
  const missing = dockerMock();
  assert.throws(() => seedRds({ root, envFile, spawn: missing.spawn }), /Invalid or empty snapshot/);
  assert.equal(missing.calls.length, 0);
});

test("RDS publication reports partial completion and removes credentials on import failure", t => {
  const { root, envFile } = fixture(t);
  const mock = dockerMock({ failImport: datasets[1].id });
  assert.throws(() => seedRds({ root, envFile, spawn: mock.spawn }), /Import failed for gh-pep-parliament.*Previously completed: gh-pep-executive/);
  assert.throws(() => statSync(mock.getSecretFile()), { code: "ENOENT" });
});

test("RDS publication fails if the active generation or count cannot be verified", t => {
  const { root, envFile } = fixture(t);
  const mock = dockerMock({ badVerification: datasets[0].id });
  assert.throws(() => seedRds({ root, envFile, spawn: mock.spawn }), /Verification failed for gh-pep-executive/);
  assert.throws(() => statSync(mock.getSecretFile()), { code: "ENOENT" });
});

test("RDS configuration rejects local database and invalid project names", t => {
  const { envFile } = fixture(t);
  assert.equal(readRdsConfig(envFile, "other-project").project, "other-project");
  assert.throws(() => readRdsConfig(envFile, "../bad"), /Invalid Compose project/);
  writeFileSync(envFile, "DATABASE_URL=postgres://user:pass@postgres:5432/fraud\n");
  assert.throws(() => readRdsConfig(envFile), /explicit PostgreSQL URL/);
  writeFileSync(envFile, `DATABASE_URL='${database}&sslmode=disable'\n`);
  assert.throws(() => readRdsConfig(envFile), /duplicate connection parameter/);
  writeFileSync(envFile, `DATABASE_URL='${database}&unsupported=value'\n`);
  assert.throws(() => readRdsConfig(envFile), /unsupported connection parameter/);
  writeFileSync(envFile, "DATABASE_URL='postgres://user:pa%40ss@rds.example:5432/fraud?sslmode=verify-full&pool_max_conns=5'\n");
  const { pgEnv } = readRdsConfig(envFile);
  assert.equal(pgEnv.PGPASSWORD, "pa@ss");
  assert.equal(pgEnv.PGSSLMODE, "verify-full");
});

test("RDS mode requires explicit write confirmation and refuses crawling", () => {
  const script = path.resolve("scripts/seed-all-screening-data.mjs");
  for (const args of [
    ["--env-file", "file"],
    ["--env-file", "file", "--confirm-write", "--crawl"],
    ["--project", "test"],
    ["--env-file", "file", "--confirm-write", "unexpected"],
  ]) {
    const result = spawnSync(process.execPath, ["--", script, ...args], { encoding: "utf8" });
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /Usage:/);
  }
});
