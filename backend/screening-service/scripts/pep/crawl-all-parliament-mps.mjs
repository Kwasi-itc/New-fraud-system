import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const ROOT_DIR = path.resolve(__dirname, '..');

/**
 * Full Parliament of Ghana Official Web Crawler
 * Crawls all 7 pages from https://www.parliament.gh/members?page=1..7
 * Extracts exact Name, Constituency, and Political Party from HTML.
 */

async function crawlPage(page) {
  const url = `https://www.parliament.gh/members?page=${page}`;
  console.log(`[Parliament Crawler] Fetching page ${page}/7: ${url}...`);

  const res = await fetch(url, {
    headers: {
      "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
      "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
    }
  });

  if (!res.ok) {
    console.warn(`[Parliament Crawler] Page ${page} failed with HTTP ${res.status}`);
    return [];
  }

  const html = await res.text();
  
  // Exact card regex based on live parliament.gh DOM structure:
  // <a ... href="members?mp=XXXXX"> ... <h5 ...>Name</h5> ... <p ...>Constituency<br>Party</p>
  const cardRegex = /<a[^>]+href=["']members\?mp=([0-9]+)["'][^>]*>[\s\S]*?<h5[^>]*>([\s\S]*?)<\/h5>[\s\S]*?<p[^>]*>([\s\S]*?)<\/p>/gi;
  const mps = [];
  const seenIds = new Set();
  let m;

  while ((m = cardRegex.exec(html)) !== null) {
    const mpId = m[1];
    if (seenIds.has(mpId)) continue;
    seenIds.add(mpId);

    const name = m[2].replace(/<[^>]+>/g, '').trim();
    const pContent = m[3];
    const lines = pContent.split(/<br\s*\/?>|\n/i).map(l => l.replace(/<[^>]+>/g, '').trim()).filter(Boolean);

    const constituency = lines[0] || "Ghana";
    const party = lines[1] || "Parliament of Ghana";

    mps.push({
      id: `gh-mp-${mpId}`,
      mpId,
      name,
      constituency,
      party,
      role: `Member of Parliament for ${constituency} (${party})`,
      ref: `https://www.parliament.gh/members?mp=${mpId}`
    });
  }

  console.log(`[Parliament Crawler] Page ${page} successfully parsed ${mps.length} MPs.`);
  return mps;
}

async function main() {
  const allMps = [];
  const seen = new Set();

  for (let p = 1; p <= 7; p++) {
    const pageMps = await crawlPage(p);
    for (const mp of pageMps) {
      if (!seen.has(mp.mpId)) {
        seen.add(mp.mpId);
        allMps.push(mp);
      }
    }
    await new Promise(r => setTimeout(r, 400));
  }

  console.log(`[Parliament Crawler] Total MPs successfully crawled and parsed: ${allMps.length}`);
  const rootTarget = path.join(ROOT_DIR, "parliament_crawled_mps.json");
  const scriptsTarget = path.join(__dirname, "parliament_crawled_mps.json");
  fs.writeFileSync(rootTarget, JSON.stringify(allMps, null, 2), "utf8");
  fs.writeFileSync(scriptsTarget, JSON.stringify(allMps, null, 2), "utf8");
  console.log(`[Parliament Crawler] Saved:\n  1. ${rootTarget}\n  2. ${scriptsTarget}`);
}

main().catch(console.error);
