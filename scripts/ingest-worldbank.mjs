import fs from 'node:fs';
import { spawnSync } from 'node:child_process';
import path from 'node:url';

// 1. Read raw official World Bank JSON
const raw = JSON.parse(fs.readFileSync('worldbank_raw.json', 'utf8'));
const list = raw.response?.ZPROCSUPP || [];
console.log(`Processing ${list.length} official World Bank records...`);

const records = [];
const seenIds = new Set();

for (const item of list) {
  const suppId = String(item.SUPP_ID || '').trim();
  if (!suppId) continue;
  const id = `wb-${suppId}`;
  if (seenIds.has(id)) continue;
  seenIds.add(id);

  let name = (item.SUPP_NAME || '').trim();
  if (item.ADD_SUPP_INFO) {
    name = `${name} ${item.ADD_SUPP_INFO}`.trim();
  }
  if (!name) continue;

  const type = item.SUPP_TYPE_CODE === 'I' ? 'Person' : 'Organization';
  const country = (item.LAND1 || '').trim().toLowerCase();
  const countryName = (item.COUNTRY_NAME || '').trim();

  // Address parts
  const addrParts = [item.SUPP_ADDR, item.SUPP_CITY, item.SUPP_STATE_CODE, countryName].filter(Boolean).map(s => String(s).trim()).filter(Boolean);
  const address = addrParts.join(', ');

  const properties = {};
  if (country && /^[a-z]{2}$/.test(country)) {
    properties.country = [country];
  }
  if (address) {
    properties.address = [address.slice(0, 500)];
  }

  const reason = (item.DEBAR_REASON || '').trim();
  if (reason) {
    properties.programs = [reason.slice(0, 500)];
  }

  const fromDate = (item.DEBAR_FROM_DATE || '').trim();
  if (fromDate) {
    properties.listedAt = [fromDate.slice(0, 50)];
  }

  const toDate = (item.DEBAR_TO_DATE || '').trim();
  const status = (item.INELIGIBLY_STATUS || item.SUPP_ELIG_STAT || 'Debarred').trim();
  properties.sanctionsImposed = [`Debarment (${status}) through ${toDate || 'indefinite'}`.slice(0, 500)];
  properties.sourceStatus = [`World Bank Ineligible Firm/Individual; Grounds: ${reason || 'Sanctioned'}`.slice(0, 500)];

  records.push({
    id,
    name: name.slice(0, 500),
    type,
    source_reference: 'https://www.worldbank.org/en/projects-operations/procurement/debarred-firms',
    source_type: 'procurement_debarred',
    properties,
  });
}

console.log(`Prepared ${records.length} valid World Bank records.`);

const fixture = {
  version: 2,
  source: 'worldbank-debarred',
  parser: 'worldbank-v1',
  records,
};

fs.writeFileSync('worldbank_fixture.json', JSON.stringify(fixture, null, 2), 'utf8');
console.log('Saved worldbank_fixture.json');

