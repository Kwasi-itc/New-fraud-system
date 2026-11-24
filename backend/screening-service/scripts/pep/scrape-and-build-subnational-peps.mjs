import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

/**
 * Ghana Sub-National PEPs (MMDCEs) Live Snapshot Builder
 *
 * Strictly uses live observations from scripts/crawled_mmdces.json
 * (produced by scripts/scrape-live-mmdces.mjs -> scripts/crawl_mmdces.py).
 * No prepopulated/hardcoded regional ministers or party officials are used.
 *
 * Output: gh_pep_local_gov.json (dataset: gh-pep-local-gov)
 */

function enrichAliases(name, existingAliases = []) {
  const aliasSet = new Set(existingAliases.map(a => a.trim()).filter(Boolean));
  const cleanName = name.replace(/^(Dr\.|Prof\.|Hon\.|Alhaji|Hajia|Nana|Mr\.|Mrs\.|Ms\.|Lawyer|Ing\.|Maj\.\s*Gen\.|Lt\.\s*Gen\.|Rev\.)\s+/i, '').trim();
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

const candidates = [
  'scripts/crawled_mmdces.json',
  'crawled_mmdces.json',
  path.join(__dirname, '..', 'crawled_mmdces.json'),
];
const inputPath = candidates.find(p => fs.existsSync(p));
if (!inputPath) {
  console.error('[Subnational Builder] ERROR: crawled_mmdces.json not found. Run node scripts/scrape-live-mmdces.mjs first.');
  process.exit(1);
}

const raw = JSON.parse(fs.readFileSync(inputPath, 'utf8'));
const observed = Array.isArray(raw) ? raw : (raw.records || raw.observations || []);
if (!Array.isArray(observed) || observed.length === 0) {
  console.error(`[Subnational Builder] ERROR: ${inputPath} contains zero live MMDCE records.`);
  process.exit(1);
}

const today = new Date().toISOString().slice(0, 10);
const seen = new Set();
const records = [];

for (let i = 0; i < observed.length; i++) {
  const row = observed[i];
  const name = (row.name || row.mmdce || row.chief_executive || '').trim();
  if (!name) continue;
  const district = (row.district || row.assembly || '').trim();
  const region = (row.region || '').trim();
  const dedupKey = `${name.toLowerCase()}|${district.toLowerCase()}`;
  if (seen.has(dedupKey)) continue;
  seen.add(dedupKey);

  const role = district
    ? `Metropolitan/Municipal/District Chief Executive, ${district}${region ? ` (${region} Region)` : ''}`
    : 'Metropolitan/Municipal/District Chief Executive';

  records.push({
    id: row.id || `gh-pep-mmdce-${String(i + 1).padStart(4, '0')}`,
    name,
    type: 'Person',
    aliases: enrichAliases(name, row.aliases || []),
    source_reference: row.source_reference || row.ref || 'https://www.ghanadistricts.com/home/AllDistricts',
    source_type: 'pep_evidence',
    properties: {
      country: ['gh'],
      nationality: ['gh'],
      countryText: ['Ghana'],
      nationalityText: ['Ghanaian'],
      programs: [
        'Politically Exposed Person (PEP)',
        'Active Office Holder',
        role,
      ],
      listedAt: [today],
      sourceStatus: [`Ghana PEP; Role: ${role}; Status: active`],
      sanctionsImposed: ['Enhanced Due Diligence (EDD) Required; AML Act 1044 Active PEP Screening'],
      address: [region ? `${district ? `${district}, ` : ''}${region} Region, Ghana` : 'Ghana'],
    },
  });
}

if (records.length === 0) {
  console.error('[Subnational Builder] ERROR: No valid MMDCE records parsed from live observations.');
  process.exit(1);
}

const payload = {
  version: 2,
  source: 'gh-pep-local-gov',
  parser: 'gh-pep-v1',
  records,
};

fs.writeFileSync('gh_pep_local_gov.json', JSON.stringify(payload, null, 2), 'utf8');
console.log(`[Subnational Builder] Saved gh_pep_local_gov.json (${records.length} live records from ${path.basename(inputPath)}).`);
