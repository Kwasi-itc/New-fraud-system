#!/usr/bin/env node
import fs from 'node:fs';
import path from 'node:path';

/**
 * Live World Bank Debarred Firms & Individuals Fetcher
 *
 * 1. Fetches https://www.worldbank.org/en/projects-operations/procurement/debarred-firms
 *    and dynamically extracts the current public `prodtabApi` URL and `propApiKey`.
 * 2. Fetches the live JSON list (`response.ZPROCSUPP`).
 * 3. Writes Version 2 snapshot `worldbank_fixture.json` in the current working directory.
 */

const PAGE_URL = 'https://www.worldbank.org/en/projects-operations/procurement/debarred-firms';

async function main() {
  console.log(`[WorldBank] Fetching public API config from ${PAGE_URL}...`);
  const pageRes = await fetch(PAGE_URL, {
    headers: { 'User-Agent': 'ITC-Screening-Collector/1.0' },
    signal: AbortSignal.timeout(30_000),
  });
  if (!pageRes.ok) {
    throw new Error(`World Bank page HTTP ${pageRes.status}`);
  }
  const html = await pageRes.text();

  const apiMatch = html.match(/var\s+prodtabApi\s*=\s*["']([^"']+)["']/);
  const keyMatch = html.match(/var\s+propApiKey\s*=\s*["']([^"']+)["']/);
  if (!apiMatch || !keyMatch) {
    throw new Error('Could not extract prodtabApi or propApiKey from World Bank page');
  }
  const apiUrl = apiMatch[1];
  const apiKey = keyMatch[1];

  console.log(`[WorldBank] Fetching live debarred list from ${apiUrl}...`);
  const dataRes = await fetch(apiUrl, {
    headers: {
      apikey: apiKey,
      Accept: 'application/json',
      'User-Agent': 'ITC-Screening-Collector/1.0',
    },
    signal: AbortSignal.timeout(60_000),
  });
  if (!dataRes.ok) {
    throw new Error(`World Bank API HTTP ${dataRes.status}`);
  }

  const raw = await dataRes.json();
  const list = raw.response?.ZPROCSUPP || [];
  if (!Array.isArray(list) || list.length === 0) {
    throw new Error('World Bank API returned zero records');
  }
  console.log(`[WorldBank] Downloaded ${list.length} raw World Bank records.`);

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
    const addrParts = [item.SUPP_ADDR, item.SUPP_CITY, item.SUPP_STATE_CODE, countryName]
      .filter(Boolean)
      .map(s => String(s).trim())
      .filter(Boolean);
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
      source_reference: PAGE_URL,
      source_type: 'procurement_debarred',
      properties,
    });
  }

  if (records.length === 0) {
    throw new Error('Zero valid World Bank records after normalization');
  }

  const outPath = path.join(process.cwd(), 'worldbank_fixture.json');
  fs.writeFileSync(outPath, JSON.stringify({
    version: 2,
    source: 'worldbank-debarred',
    parser: 'worldbank-v1',
    records,
  }, null, 2), 'utf8');
  console.log(`[WorldBank] Saved ${records.length} live records to ${outPath}`);
}

main().catch(err => {
  console.error(`[WorldBank] ERROR: ${err.message}`);
  process.exit(1);
});
