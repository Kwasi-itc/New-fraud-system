import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';

/**
 * Ghana State-Owned Enterprises (SOE) PEP Crawler & Live Verification Engine
 * 
 * Statutory Mandates:
 * - Anti-Money Laundering Act, 2020 (Act 1044) - Senior Executives of State Enterprises
 * - State Interests and Governance Authority Act, 2019 (Act 990) - Central Oversight of 175+ State Entities
 * 
 * Integrated with Scrapling (Python) for TLS-fingerprinted live scraping of SIGA.
 */

const HEADERS = {
  'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
};

const SOE_PORTALS = [
  { code: 'SIGA', name: 'State Interests and Governance Authority', url: 'https://siga.gov.gh/the-board/', type: 'Apex Oversight Regulator (Act 990)' },
  { code: 'GNPC', name: 'Ghana National Petroleum Corporation', url: 'https://www.gnpcghana.com/about-us/board', type: 'State Oil & Gas Producer' },
  { code: 'GHANA_GAS', name: 'Ghana National Gas Limited Company', url: 'https://ghanagas.com.gh', type: 'Natural Gas Infrastructure' },
  { code: 'COCOBOD', name: 'Ghana Cocoa Board', url: 'https://cocobod.gh', type: 'Commodities & Agribusiness' },
  { code: 'VRA', name: 'Volta River Authority', url: 'https://www.vra.com', type: 'Power Generation Utility' },
  { code: 'ECG', name: 'Electricity Company of Ghana', url: 'https://ecg.com.gh', type: 'Power Distribution Utility' },
  { code: 'BOST', name: 'Bulk Energy Storage & Transportation', url: 'https://bost.com.gh', type: 'Petroleum Strategic Reserves' },
  { code: 'NPA', name: 'National Petroleum Authority', url: 'https://www.npa.gov.gh', type: 'Downstream Petroleum Regulator' },
  { code: 'GPHA', name: 'Ghana Ports and Harbours Authority', url: 'https://ghanaports.gov.gh', type: 'Maritime & Port Infrastructure' },
  { code: 'BOG', name: 'Bank of Ghana (Central Bank)', url: 'https://bog.gov.gh', type: 'Central Banking & Financial Stability' },
  { code: 'GRA', name: 'Ghana Revenue Authority', url: 'https://gra.gov.gh', type: 'National Tax & Customs Administration' },
  { code: 'SSNIT', name: 'Social Security and National Insurance Trust', url: 'https://ssnit.org.gh', type: 'Pension & Investment Fund' },
  { code: 'MIIF', name: 'Minerals Income Investment Fund', url: 'https://miif.gov.gh', type: 'Sovereign Mining Royalties' },
  { code: 'PMMC', name: 'Precious Minerals Marketing Company', url: 'https://pmmc.gov.gh', type: 'National Gold & Diamond Assayer' }
];

async function probeEndpoint(soe) {
  try {
    const res = await fetch(soe.url, {
      headers: HEADERS,
      signal: AbortSignal.timeout(4000)
    });
    return { status: res.status, ok: res.ok };
  } catch (err) {
    return { status: 'Timeout / Unreachable', ok: false, error: err.message };
  }
}

function runSigaScraplingCrawler() {
  console.log('\n[*] Executing Python Scrapling Crawler (scripts/scrape_siga_scrapling.py)...');
  const pythonBin = process.platform === 'win32'
    ? 'C:\\Users\\T H I N K\\AppData\\Local\\Python\\bin\\python.exe'
    : 'python3';

  const scriptPath = path.resolve('scripts', 'scrape_siga_scrapling.py');
  if (fs.existsSync(pythonBin) && fs.existsSync(scriptPath)) {
    const proc = spawnSync(pythonBin, [scriptPath], { encoding: 'utf8' });
    if (proc.status === 0) {
      console.log('[✓] Scrapling SIGA crawler finished successfully.');
    } else {
      console.log(`[!] Scrapling crawler note: exited with code ${proc.status}`);
      if (proc.stderr) console.log(proc.stderr.trim());
    }
  } else {
    console.log('[!] Python binary or Scrapling script not found at standard path, verifying existing crawled artifact...');
  }
}

async function run() {
  console.log('========================================================================');
  console.log('    GHANA STATE-OWNED ENTERPRISES (SOE) MONITORING & SCRAPING ENGINE    ');
  console.log('========================================================================');
  console.log('Legal Framework: Act 1044 (PEP Rules) & Act 990 (SIGA Central Oversight)');
  console.log(`Auditing ${SOE_PORTALS.length} Primary Government Portals in Real-Time...\n`);

  const results = [];
  for (const soe of SOE_PORTALS) {
    const probe = await probeEndpoint(soe);
    results.push({ ...soe, ...probe });
    const indicator = probe.ok ? '✓ ONLINE' : `⚠ ${probe.status}`;
    console.log(`[${soe.code.padEnd(9)}] ${indicator.padEnd(14)} | ${soe.name} (${soe.type})`);
  }

  // Run or verify Scrapling SIGA crawler
  runSigaScraplingCrawler();

  // Load crawled SIGA records
  let sigaRecords = [];
  const sigaFile = fs.existsSync('crawled_siga_peps.json')
    ? 'crawled_siga_peps.json'
    : path.join('scripts', 'crawled_siga_peps.json');

  if (fs.existsSync(sigaFile)) {
    const d = JSON.parse(fs.readFileSync(sigaFile, 'utf8'));
    sigaRecords = d.records || [];
  }

  // Load existing ingested database records
  let repoRecords = [];
  if (fs.existsSync('gh_pep_soe.json')) {
    const d = JSON.parse(fs.readFileSync('gh_pep_soe.json', 'utf8'));
    repoRecords = d.records || [];
  }

  // Merge SIGA records into repoRecords if not already present
  let mergedCount = 0;
  for (const sr of sigaRecords) {
    const exists = repoRecords.some(r => r.name.toLowerCase() === sr.name.toLowerCase());
    if (!exists) {
      repoRecords.push(sr);
      mergedCount++;
    }
  }

  if (mergedCount > 0) {
    fs.writeFileSync('gh_pep_soe.json', JSON.stringify({
      version: 2,
      source: 'gh-pep-soe',
      parser: 'gh-pep-v1',
      records: repoRecords
    }, null, 2));
    console.log(`\n[✓] Ingested ${mergedCount} new Scrapling-crawled SIGA leadership PEPs into gh_pep_soe.json`);
  }

  const activeRecords = repoRecords.filter(r => r.properties.sourceStatus?.[0]?.includes('active'));
  const coolingRecords = repoRecords.filter(r => r.properties.sourceStatus?.[0]?.includes('cooling_off'));

  console.log('\n========================================================================');
  console.log('             COMPLIANCE & SCREENING DATABASE INGESTION STATUS            ');
  console.log('========================================================================');
  console.log(`Total SOE / Statutory Leadership Records in Database: ${repoRecords.length}`);
  console.log(`  - Active Public Enterprise CEOs & Board Chairs:    ${activeRecords.length}`);
  console.log(`  - Outgoing Leadership Retained (AML Cooling-Off):   ${coolingRecords.length}`);
  console.log(`  - SIGA Apex Oversight Leadership Records:          ${sigaRecords.length}`);
  
  console.log('\nSample Active Monitored Executives:');
  activeRecords.slice(0, 10).forEach((r, idx) => {
    const role = r.properties.programs?.find(p => !p.includes('PEP') && !p.includes('Active')) || 'Senior Executive';
    console.log(`  ${(idx + 1).toString().padStart(2)}. ${r.name.padEnd(32)} | ${role}`);
  });

  console.log('\nSample Former Executives under Statutory 1-Year Cooling-Off:');
  coolingRecords.slice(0, 5).forEach((r, idx) => {
    const role = r.properties.programs?.find(p => p.includes('Former')) || 'Former Executive';
    console.log(`  ${(idx + 1).toString().padStart(2)}. ${r.name.padEnd(32)} | ${role}`);
  });
  console.log('========================================================================\n');
}

run();
