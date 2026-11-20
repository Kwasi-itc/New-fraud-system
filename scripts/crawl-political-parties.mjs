import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';

/**
 * Ghana Senior Political Party Officials PEP Active Live Scraper & Orchestrator
 * 
 * Statutory Mandates:
 * - Anti-Money Laundering Act, 2020 (Act 1044), Section 59(1)(f)
 * - FATF Recommendation 12 (Domestic PEPs - Senior Political Party Officials)
 * 
 * Uses Scrapling (Python) for active live scraping of party leadership and infoboxes.
 */

function run() {
  console.log("========================================================================");
  console.log("     GHANA SENIOR POLITICAL PARTY OFFICIALS (ACT 1044) ACTIVE SCRAPER   ");
  console.log("========================================================================");

  const pythonBin = process.platform === 'win32'
    ? 'C:\\Users\\T H I N K\\AppData\\Local\\Python\\bin\\python.exe'
    : 'python3';

  const scriptPath = path.resolve('scripts', 'scrape_political_parties_live.py');
  if (fs.existsSync(pythonBin) && fs.existsSync(scriptPath)) {
    console.log(`[*] Executing active live scraper: ${scriptPath} ...`);
    const proc = spawnSync(pythonBin, [scriptPath], { encoding: 'utf8', stdio: 'inherit' });
    if (proc.status === 0) {
      console.log('[✓] Live Political Parties scraper completed successfully.');
    } else {
      console.log(`[!] Scraper exited with code ${proc.status}`);
    }
  } else {
    console.log('[!] Scraper script or Python runtime not found at expected path.');
  }

  // Load and report generated dataset
  const datasetPath = fs.existsSync('gh_pep_parties.json')
    ? 'gh_pep_parties.json'
    : path.join('scripts', 'gh_pep_parties.json');

  if (fs.existsSync(datasetPath)) {
    const data = JSON.parse(fs.readFileSync(datasetPath, 'utf8'));
    console.log(`\nVerified Dataset: ${datasetPath} (${data.total_records || data.records.length} records)`);
  }
}

run();
