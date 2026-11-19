import fs from 'node:fs';

/**
 * Direct Live MMDCE Scraper (No Fallback)
 * 
 * Target: https://ghanadistricts.com/Home/AllDistricts
 * Mandate: Anti-Money Laundering Act, 2020 (Act 1044) - Sub-National Political Figures (PEPs)
 * 
 * Connects directly to the live server, parses the ASP.NET DevExpress data grid,
 * and outputs the verified active Metropolitan, Municipal, and District Chief Executives (MMDCEs).
 */

async function scrapeLiveMmdces() {
  const url = 'https://ghanadistricts.com/Home/AllDistricts';

  console.log('========================================================================');
  console.log('         GHANA MMDCE DIRECT LIVE SCRAPER (NO FALLBACK)                  ');
  console.log('========================================================================');
  console.log(`Connecting directly to live endpoint: ${url}`);
  console.log('Source: Ministry of Local Gov & Decentralisation / GhanaDistricts Repository\n');

  const startTime = Date.now();
  const res = await fetch(url, {
    headers: {
      'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
      'Accept': 'text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8',
      'Accept-Language': 'en-US,en;q=0.9'
    }
  });

  if (!res.ok) {
    throw new Error(`Failed to fetch from ${url}: HTTP ${res.status} ${res.statusText}`);
  }

  const html = await res.text();
  const elapsed = ((Date.now() - startTime) / 1000).toFixed(2);
  console.log(`[✓] HTTP 200 OK received in ${elapsed}s (Payload: ${html.length.toLocaleString()} bytes)`);

  // Parse DevExpress Data Rows
  const rowRe = /<tr id="grid_DXDataRow(\d+)"[^>]*>([\s\S]*?)<\/tr>/gi;
  let match;
  const mmdces = [];

  while ((match = rowRe.exec(html)) !== null) {
    const rowHtml = match[2];
    const cellRe = /<td[^>]*>([\s\S]*?)<\/td>/gi;
    const cells = [];
    let cellMatch;

    while ((cellMatch = cellRe.exec(rowHtml)) !== null) {
      const text = cellMatch[1].replace(/<[^>]+>/g, '').trim();
      cells.push(text);
    }

    if (cells.length >= 4) {
      const name = cells[0];
      const gender = cells[1];
      const region = cells[2];
      const district = cells[3];

      if (name && district) {
        mmdces.push({
          rowId: match[1],
          name,
          gender: gender === 'M' ? 'Male' : gender === 'F' ? 'Female' : gender,
          region,
          district
        });
      }
    }
  }

  console.log(`\n[✓] Successfully extracted ${mmdces.length} live MMDCE records directly from the website:`);
  console.log('------------------------------------------------------------------------');

  mmdces.forEach((item, idx) => {
    const num = String(idx + 1).padStart(3, ' ');
    const name = item.name.padEnd(30, ' ');
    const district = item.district.padEnd(35, ' ');
    const region = item.region.padEnd(16, ' ');
    console.log(`${num}. ${name} | District: ${district} | Region: ${region} (${item.gender})`);
  });

  console.log('------------------------------------------------------------------------');

  // Save results to file
  const rootTarget = 'crawled_mmdces.json';
  const scriptsTarget = 'scripts/crawled_mmdces.json';

  const outputPayload = {
    crawledAt: new Date().toISOString(),
    sourceUrl: url,
    sourceName: "Ministry of Local Government, Decentralisation & Rural Development / GhanaDistricts Repository",
    totalExtracted: mmdces.length,
    records: mmdces
  };

  fs.writeFileSync(rootTarget, JSON.stringify(outputPayload, null, 2), 'utf8');
  fs.writeFileSync(scriptsTarget, JSON.stringify(outputPayload, null, 2), 'utf8');

  console.log(`[✓] Output successfully written to:`);
  console.log(`    1. ${rootTarget}`);
  console.log(`    2. ${scriptsTarget}`);
  console.log(`Direct live scrape finished successfully in ${elapsed}s with ZERO fallback.`);
  console.log('========================================================================\n');

  return mmdces;
}

scrapeLiveMmdces().catch(err => {
  console.error('\n[X] Direct scrape encountered error:', err.message);
  process.exit(1);
});
