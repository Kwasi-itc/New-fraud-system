import fs from 'node:fs';

/**
 * Unified Ghana PEP & RCA Live Snapshot Builder
 *
 * Strictly aggregates ONLY live-fetched datasets (no hardcoded/prepopulated people):
 * 1. parliament_crawled_mps.json  -> gh_pep_parliament.json
 * 2. gh_pep_wikidata.json         -> gh_pep_executive.json
 * 3. crawled_siga_peps.json       -> gh_pep_soe.json
 * 4. gh_rca_wikidata.json         -> gh_pep_rca.json
 */

function enrichAliases(name, existingAliases = []) {
  const aliasSet = new Set(existingAliases.map(a => a.trim()).filter(Boolean));
  const cleanName = name.replace(/^(Dr\.|Prof\.|Hon\.|Alhaji|Hajia|Nana|Mr\.|Mrs\.|Ms\.)\s+/i, '').trim();
  if (cleanName && cleanName !== name) {
    aliasSet.add(cleanName);
  }
  const tokens = cleanName.split(/\s+/).filter(Boolean);
  if (tokens.length >= 2) {
    const surname = tokens[tokens.length - 1];
    if (surname.length >= 3) {
      aliasSet.add(surname);
    }
    aliasSet.add(`${tokens[0]} ${tokens[tokens.length - 1]}`);
  }
  aliasSet.delete(name);
  return Array.from(aliasSet);
}

function readJsonIfPresent(filePath) {
  if (!fs.existsSync(filePath)) return null;
  return JSON.parse(fs.readFileSync(filePath, 'utf8'));
}

const today = new Date().toISOString().slice(0, 10);
let builtCount = 0;

// 1. Parliament (live parliament.gh crawl)
const crawledMps = readJsonIfPresent('parliament_crawled_mps.json');
if (Array.isArray(crawledMps) && crawledMps.length > 0) {
  const seen = new Set();
  const records = [];
  for (const mp of crawledMps) {
    const key = (mp.name || '').trim().toLowerCase();
    if (!key || seen.has(key)) continue;
    seen.add(key);
    records.push({
      id: mp.id,
      name: mp.name,
      type: 'Person',
      aliases: enrichAliases(mp.name, mp.aliases || []),
      source_reference: mp.ref || 'https://www.parliament.gh/members',
      source_type: 'pep_evidence',
      properties: {
        country: ['gh'],
        nationality: ['gh'],
        countryText: ['Ghana'],
        nationalityText: ['Ghanaian'],
        programs: [
          'Politically Exposed Person (PEP)',
          'Active Office Holder',
          mp.role || 'Member of Parliament of Ghana',
        ],
        listedAt: [today],
        sourceStatus: [`Ghana PEP; Role: ${mp.role || 'Member of Parliament'}; Status: active`],
        sanctionsImposed: ['Enhanced Due Diligence (EDD) Required; AML Act 1044 Active PEP Screening'],
        address: [mp.constituency ? `Constituency: ${mp.constituency}, Ghana` : 'Ghana'],
      },
    });
  }
  fs.writeFileSync('gh_pep_parliament.json', JSON.stringify({
    version: 2,
    source: 'gh-pep-parliament',
    parser: 'gh-pep-v1',
    records,
  }, null, 2), 'utf8');
  console.log(`[Master Builder] Saved gh_pep_parliament.json (${records.length} live records).`);
  builtCount++;
} else {
  console.warn('[Master Builder] WARN: parliament_crawled_mps.json missing or empty; skipping gh_pep_parliament.json.');
}

// 2. Executive & Political Office Holders (live Wikidata SPARQL)
const wdPeps = readJsonIfPresent('gh_pep_wikidata.json');
if (wdPeps && Array.isArray(wdPeps.records) && wdPeps.records.length > 0) {
  const records = wdPeps.records.map(r => ({
    ...r,
    aliases: enrichAliases(r.name, r.aliases || []),
  }));
  fs.writeFileSync('gh_pep_executive.json', JSON.stringify({
    version: 2,
    source: 'gh-pep-executive',
    parser: 'gh-pep-v1',
    records,
  }, null, 2), 'utf8');
  console.log(`[Master Builder] Saved gh_pep_executive.json (${records.length} live Wikidata records).`);
  builtCount++;
} else {
  console.warn('[Master Builder] WARN: gh_pep_wikidata.json missing or empty; skipping gh_pep_executive.json.');
}

// 3. SOE & SIGA Leadership (live Scrapling crawl)
const sigaPeps = readJsonIfPresent('crawled_siga_peps.json') || readJsonIfPresent('scripts/crawled_siga_peps.json');
if (sigaPeps && Array.isArray(sigaPeps.records) && sigaPeps.records.length > 0) {
  const records = sigaPeps.records.map(r => ({
    ...r,
    aliases: enrichAliases(r.name, r.aliases || []),
  }));
  fs.writeFileSync('gh_pep_soe.json', JSON.stringify({
    version: 2,
    source: 'gh-pep-soe',
    parser: 'gh-pep-v1',
    records,
  }, null, 2), 'utf8');
  console.log(`[Master Builder] Saved gh_pep_soe.json (${records.length} live SOE records).`);
  builtCount++;
} else {
  console.warn('[Master Builder] WARN: crawled_siga_peps.json missing or empty; skipping gh_pep_soe.json.');
}

// 4. Relatives & Close Associates (live Wikidata SPARQL)
const wdRcas = readJsonIfPresent('gh_rca_wikidata.json');
if (wdRcas && Array.isArray(wdRcas.records) && wdRcas.records.length > 0) {
  const seen = new Set();
  const records = [];
  for (const r of wdRcas.records) {
    const key = (r.name || '').trim().toLowerCase();
    if (!key || seen.has(key)) continue;
    seen.add(key);
    records.push({
      ...r,
      aliases: enrichAliases(r.name, r.aliases || []),
    });
  }
  fs.writeFileSync('gh_pep_rca.json', JSON.stringify({
    version: 2,
    source: 'gh-pep-rca',
    parser: 'gh-pep-v1',
    records,
  }, null, 2), 'utf8');
  console.log(`[Master Builder] Saved gh_pep_rca.json (${records.length} live Wikidata RCA records).`);
  builtCount++;
} else {
  console.warn('[Master Builder] WARN: gh_rca_wikidata.json missing or empty; skipping gh_pep_rca.json.');
}

if (builtCount === 0) {
  console.error('[Master Builder] ERROR: No live datasets were available to build.');
  process.exit(1);
}
console.log(`[Master Builder] Completed ${builtCount} live snapshot(s).`);
