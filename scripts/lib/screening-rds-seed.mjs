import { spawnSync } from "node:child_process";
import { chmodSync, mkdtempSync, readFileSync, rmdirSync, unlinkSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";

export const datasets = [
  { id: "gh-pep-executive", file: "gh_pep_executive.json" },
  { id: "gh-pep-parliament", file: "gh_pep_parliament.json" },
  { id: "gh-pep-soe", file: "gh_pep_soe.json" },
  { id: "gh-pep-rca", file: "gh_pep_rca.json" },
  { id: "gh-pep-local-gov", file: "gh_pep_local_gov.json" },
  { id: "gh-pep-parties", file: "gh_pep_parties.json" },
  { id: "gh-pep-judiciary", file: "gh_pep_judiciary.json" },
  { id: "worldbank-debarred", file: "worldbank_fixture.json" },
];

const pgVariables = {
  host: "PGHOST", hostaddr: "PGHOSTADDR", port: "PGPORT", dbname: "PGDATABASE",
  user: "PGUSER", password: "PGPASSWORD", passfile: "PGPASSFILE",
  sslmode: "PGSSLMODE", sslrootcert: "PGSSLROOTCERT", sslcert: "PGSSLCERT",
  sslkey: "PGSSLKEY", sslcrl: "PGSSLCRL", sslcrldir: "PGSSLCRLDIR",
  ssl_min_protocol_version: "PGSSLMINPROTOCOLVERSION",
  ssl_max_protocol_version: "PGSSLMAXPROTOCOLVERSION",
  sslcompression: "PGSSLCOMPRESSION", sslsni: "PGSSLSNI",
  application_name: "PGAPPNAME", connect_timeout: "PGCONNECT_TIMEOUT",
  options: "PGOPTIONS", client_encoding: "PGCLIENTENCODING",
  target_session_attrs: "PGTARGETSESSIONATTRS", gssencmode: "PGGSSENCMODE",
  channel_binding: "PGCHANNELBINDING", requirepeer: "PGREQUIREPEER",
};
const poolParameters = new Set(["pool_max_conns", "pool_min_conns", "pool_max_conn_lifetime",
  "pool_max_conn_lifetime_jitter", "pool_max_conn_idle_time", "pool_health_check_period"]);

function libpqEnvironment(url) {
  const vars = {
    PGHOST: url.hostname.replace(/^\[|\]$/g, ""),
    PGDATABASE: decodeURIComponent(url.pathname.slice(1)),
    PGCONNECT_TIMEOUT: "10",
  };
  if (url.port) vars.PGPORT = url.port;
  if (url.username) vars.PGUSER = decodeURIComponent(url.username);
  if (url.password) vars.PGPASSWORD = decodeURIComponent(url.password);
  const seen = new Set();
  for (const [key, value] of url.searchParams) {
    if (seen.has(key)) throw new Error(`DATABASE_URL contains a duplicate connection parameter: ${key}`);
    seen.add(key);
    if (poolParameters.has(key)) continue;
    if (!Object.hasOwn(pgVariables, key)) throw new Error(`DATABASE_URL has an unsupported connection parameter for the preflight: ${key}`);
    vars[pgVariables[key]] = value;
  }
  if (Object.values(vars).some(value => /[\r\n\0]/.test(value))) {
    throw new Error("DATABASE_URL contains a connection value unsupported by Docker env files.");
  }
  return vars;
}

export function readRdsConfig(envFile, projectOverride) {
  const entries = Object.fromEntries(readFileSync(envFile, "utf8").split(/\r?\n/).flatMap(line => {
    const match = line.match(/^([A-Z][A-Z0-9_]*)=(.*)$/);
    if (!match) return [];
    let value = match[2].trim();
    if (/^(['"]).*\1$/.test(value)) value = value.slice(1, -1).replaceAll("\\'", "'");
    return [[match[1], value]];
  }));
  const project = projectOverride ?? entries.COMPOSE_PROJECT_NAME ?? "new-fraud-system";
  if (!/^[a-z0-9][a-z0-9_-]*$/.test(project)) throw new Error("Invalid Compose project name.");
  const database = entries.DATABASE_URL;
  let url;
  try { url = new URL(database); } catch { throw new Error("The env file must contain a valid DATABASE_URL."); }
  if (!["postgres:", "postgresql:"].includes(url.protocol) || !url.hostname || !url.pathname.slice(1) ||
      url.hostname === "postgres" || /[\r\n\0]/.test(database)) {
    throw new Error("DATABASE_URL must be an explicit PostgreSQL URL for the intended RDS database.");
  }
  return { database, project, pgEnv: libpqEnvironment(url) };
}

function command(args, options = {}, spawn = spawnSync) {
  const result = spawn("docker", args, { encoding: "utf8", ...options });
  if (result.error) throw new Error(`Could not run Docker: ${result.error.message}`);
  if (result.status !== 0) throw new Error(`Docker command failed${result.stderr ? `: ${result.stderr.trim()}` : "."}`);
  return result.stdout ?? "";
}

export function seedRds({ envFile, project: projectOverride, root, spawn = spawnSync }) {
  const { database, project, pgEnv } = readRdsConfig(envFile, projectOverride);
  const snapshots = datasets.map(dataset => {
    const file = path.join(root, dataset.file);
    if (file.includes(",")) throw new Error("Snapshot paths cannot contain commas when using Docker bind mounts.");
    const snapshot = JSON.parse(readFileSync(file, "utf8"));
    if (snapshot.source !== dataset.id || typeof snapshot.parser !== "string" ||
        !snapshot.parser || !Array.isArray(snapshot.records) || snapshot.records.length === 0) {
      throw new Error(`Invalid or empty snapshot: ${dataset.file}`);
    }
    return { ...dataset, path: file, parser: snapshot.parser, count: snapshot.records.length };
  });

  const ids = command(["ps", "--filter", `label=com.docker.compose.project=${project}`,
    "--filter", "label=com.docker.compose.service=screening-service", "--format", "{{.ID}}"], {}, spawn).trim().split(/\s+/).filter(Boolean);
  if (ids.length !== 1) throw new Error(`Expected one running screening-service container for project ${project}; found ${ids.length}.`);
  const container = ids[0];
  const inspect = JSON.parse(command(["inspect", "--format", "{{json .}}", container], {}, spawn));
  const currentDb = inspect.Config?.Env?.find(value => value.startsWith("DATABASE_URL="))?.slice("DATABASE_URL=".length);
  if (currentDb !== database) throw new Error("The env file DATABASE_URL does not match the running screening service. Use its active Compose env file.");
  if (!/^sha256:[a-f0-9]{64}$/.test(inspect.Image ?? "")) throw new Error("Could not identify the running screening-service image.");

  command(["image", "inspect", "postgres:16"], { stdio: ["ignore", "ignore", "pipe"] }, spawn);
  const directory = mkdtempSync(path.join(os.tmpdir(), "screening-rds-"));
  const secretFile = path.join(directory, "database.env");
  const completed = [];
  try {
    const env = { SOURCE_DATABASE_URL: database, ...pgEnv };
    writeFileSync(secretFile, Object.entries(env).map(([key, value]) => `${key}=${value}\n`).join(""), { mode: 0o600 });
    chmodSync(secretFile, 0o600);
    const common = ["run", "--rm", "--pull=never", "--network", `container:${container}`, "--env-file", secretFile];
    const idsSql = snapshots.map(item => `'${item.id}'`).join(",");
    const sql = `SELECT COALESCE(json_agg(json_build_object('id',d.id,'connector',c.connector,'parser',c.config->>'parser','approved',c.rights_state='approved','usable',c.enabled OR c.config->>'intake_mode'='reviewed_snapshot','grants',(SELECT count(*) FROM screening.source_grants g WHERE (g.owner_scope,g.dataset_id)=(d.owner_scope,d.id))) ORDER BY d.id),'[]'::json) FROM screening.source_datasets d LEFT JOIN screening.source_connections c ON (c.owner_scope,c.dataset_id)=(d.owner_scope,d.id) WHERE d.owner_scope='00000000-0000-0000-0000-000000000000' AND d.id IN (${idsSql});`;
    const output = command([...common, "postgres:16", "psql", "-X", "-v", "ON_ERROR_STOP=1", "-A", "-t", "-c", sql], {}, spawn);
    const sources = JSON.parse(output);
    for (const snapshot of snapshots) {
      const source = sources.find(item => item.id === snapshot.id);
      if (!source || !source.approved || !source.usable || source.connector !== snapshot.id || source.parser !== snapshot.parser) {
        throw new Error(`Source ${snapshot.id} is missing, unapproved, or incompatible with its snapshot. Configure it through the screening source administration process.`);
      }
      console.log(`${snapshot.id}: approved, ${source.grants} existing tenant grant(s), ${snapshot.file}`);
    }
    console.log("Publishing operator-supplied snapshots. Existing grants are unchanged; each dataset is published separately.");
    const publications = new Map();
    for (const snapshot of snapshots) {
      console.log(`Importing ${snapshot.id}...`);
      try {
        const output = command([...common, "--mount", `type=bind,src=${snapshot.path},dst=/data/snapshot.json,readonly`,
          "--entrypoint", "/app/source-import", inspect.Image,
          "-file", "/data/snapshot.json", "-dataset", snapshot.id,
          "-snapshot-reference", `Operator-provided snapshot: ${snapshot.file}`],
        {}, spawn);
        const result = JSON.parse(output);
        if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(result.generation ?? "")) {
          throw new Error("Importer did not report a valid generation. The publication outcome is uncertain.");
        }
        publications.set(snapshot.id, result.generation);
        completed.push(snapshot.id);
      } catch (error) {
        throw new Error(`Import failed for ${snapshot.id}; its publication outcome may be uncertain. Previously completed: ${completed.join(", ") || "none"}. ${error.message}`);
      }
    }
    const verifySql = `SELECT COALESCE(json_agg(json_build_object('id',d.id,'generation',d.active_generation,'count',v.record_count) ORDER BY d.id),'[]'::json) FROM screening.source_datasets d LEFT JOIN screening.source_versions v ON (v.owner_scope,v.dataset_id,v.id)=(d.owner_scope,d.id,d.active_generation) WHERE d.owner_scope='00000000-0000-0000-0000-000000000000' AND d.id IN (${idsSql});`;
    const verified = JSON.parse(command([...common, "postgres:16", "psql", "-X", "-v", "ON_ERROR_STOP=1", "-A", "-t", "-c", verifySql], {}, spawn));
    for (const snapshot of snapshots) {
      const active = verified.find(item => item.id === snapshot.id);
      if (!active || active.generation !== publications.get(snapshot.id) || active.count !== snapshot.count) {
        throw new Error(`Verification failed for ${snapshot.id}; inspect active generations before retrying. Completed imports: ${completed.join(", ")}.`);
      }
      console.log(`${snapshot.id}: verified ${active.count} records in active generation ${active.generation}`);
    }
  } finally {
    try { unlinkSync(secretFile); } catch (error) { if (error.code !== "ENOENT") throw error; }
    rmdirSync(directory);
  }
  console.log("All listed snapshot generations were verified.");
}
