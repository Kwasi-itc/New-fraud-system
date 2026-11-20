#!/usr/bin/env node
/**
 * Master Seeding Script for Ghana PEPs & Watchlists
 * 
 * Usage:
 *   node scripts/seed-all-screening-data.mjs           # Uses/generates fixtures & seeds database
 *   node scripts/seed-all-screening-data.mjs --crawl   # Runs live web crawlers (parliament.gh + wikidata) first
 */

import { execSync } from "child_process";
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const ROOT_DIR = path.resolve(__dirname, "..");

const args = process.argv.slice(2);
const shouldCrawl = args.includes("--crawl");

function run(cmd, desc) {
  console.log(`\n==> [${desc}] Running: ${cmd}`);
  try {
    const out = execSync(cmd, { cwd: ROOT_DIR, encoding: "utf8", stdio: "inherit" });
    return out;
  } catch (err) {
    console.error(`ERROR in [${desc}]:`, err.message);
    process.exit(1);
  }
}

async function main() {
  console.log("================================================================================");
  console.log("          SCREENING MODULE - AUTOMATED PEP SEEDING & INGESTION PIPELINE         ");
  console.log("================================================================================");

  // 1. Build Canonical Base JSON Fixtures
  console.log("\n[Step 1] Building Canonical JSON Fixtures...");
  run("node scripts/build-unified-ghana-peps.mjs", "Core Ghana PEP Builder (Executive, MPs, SOEs, RCAs)");
  run("node scripts/scrape-and-build-subnational-peps.mjs", "Sub-National & Political Party Builder");

  // 2. Live Crawlers / Registry Enrichers
  if (shouldCrawl) {
    console.log("\n[Step 2] Running Live Web Crawlers...");
    run("node scripts/crawl-all-parliament-mps.mjs", "Live Parliament Web Crawler");
    run("node scripts/sync-wikidata-ghana.mjs", "Wikidata SPARQL Knowledge Graph Fetcher");
    run("node scripts/crawl-all-soes.mjs", "State-Owned Enterprises Live Crawler");
    run("node scripts/crawl-political-parties.mjs", "Senior Political Party Officials Crawler");
    run("node scripts/crawl-judiciary.mjs", "Judicial Service Senior Figures Crawler");
  } else {
    console.log("\n[Step 2] Enriching with live scrapers & verified rosters...");
    run("node scripts/crawl-all-soes.mjs", "State-Owned Enterprises Live Crawler");
    run("node scripts/crawl-political-parties.mjs", "Senior Political Party Officials Registry");
    run("node scripts/crawl-judiciary.mjs", "Judicial Service Senior Figures Registry");
  }

  // 3. Register Datasets in PostgreSQL
  console.log("\n[Step 3] Registering Datasets & Tenant Grants in PostgreSQL...");
  const registerSql = path.join(__dirname, "register-all-ghana-peps.sql");
  try {
    const sqlContent = fs.readFileSync(registerSql, "utf8");
    execSync("docker exec -i new-fraud-system-postgres-1 psql -U fraud -d fraud", {
      cwd: ROOT_DIR,
      input: sqlContent,
      encoding: "utf8",
      stdio: ["pipe", "inherit", "inherit"]
    });
  } catch (err) {
    console.error("ERROR registering SQL datasets:", err.message);
    process.exit(1);
  }

  // 4. Ingest All 6 Datasets
  console.log("\n[Step 4] Copying & Ingesting Datasets into screening-service...");
  const datasets = [
    { id: "gh-pep-executive", file: "gh_pep_executive.json" },
    { id: "gh-pep-parliament", file: "gh_pep_parliament.json" },
    { id: "gh-pep-soe", file: "gh_pep_soe.json" },
    { id: "gh-pep-rca", file: "gh_pep_rca.json" },
    { id: "gh-pep-local-gov", file: "gh_pep_local_gov.json" },
    { id: "gh-pep-parties", file: "gh_pep_parties.json" },
  ];

  for (const ds of datasets) {
    console.log(`    Ingesting ${ds.id} (${ds.file})...`);
    run(`docker cp ${ds.file} new-fraud-system-screening-service-1:/tmp/${ds.file}`, `Copy ${ds.file}`);
    run(
      `docker exec -e SOURCE_DATABASE_URL=postgres://fraud:fraud@postgres:5432/fraud?sslmode=disable new-fraud-system-screening-service-1 /app/source-import -file /tmp/${ds.file} -dataset ${ds.id}`,
      `Import ${ds.id}`
    );
  }

  // 5. Activate Generations and Refresh Statistics
  console.log("\n[Step 5] Activating Generations and Refreshing Statistics...");
  const activateQuery = `UPDATE screening.source_connections c SET verified_generation = d.active_generation, last_success_at = now(), last_error = '', rights_state = 'approved', enabled = true, max_stale = interval '30 days' FROM screening.source_datasets d WHERE (d.owner_scope, d.id) = (c.owner_scope, c.dataset_id) AND d.active_generation IS NOT NULL; SELECT screening.refresh_source_statistics_v1();`;
  run(
    `docker exec new-fraud-system-postgres-1 psql -U fraud -d fraud -c "${activateQuery}"`,
    "Activate & Refresh Statistics"
  );

  // 6. Summary Output
  console.log("\n[Step 6] Verification of Active Records in Database:");
  const countQuery = `SELECT d.id, count(r.record_id) as records FROM screening.source_datasets d JOIN screening.source_records r ON r.owner_scope = d.owner_scope AND r.dataset_id = d.id AND r.generation = d.active_generation GROUP BY d.id ORDER BY records DESC;`;
  run(
    `docker exec new-fraud-system-postgres-1 psql -U fraud -d fraud -c "${countQuery}"`,
    "Count Records Query"
  );

  console.log("\n================================================================================");
  console.log("SUCCESS! All Ghana PEP & Watchlist datasets have been compiled & seeded.");
  console.log("================================================================================\n");
}

main().catch((err) => {
  console.error("FATAL ERROR during seeding:", err);
  process.exit(1);
});
