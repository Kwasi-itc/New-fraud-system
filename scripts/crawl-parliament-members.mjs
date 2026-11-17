import fs from 'node:fs';

async function crawlMembers() {
  console.log("[Parliament Crawler] Fetching https://www.parliament.gh/members...");
  const res = await fetch("https://www.parliament.gh/members", {
    headers: {
      "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
      "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
    }
  });

  console.log(`[Parliament Crawler] Status: ${res.status}`);
  if (!res.ok) return;

  const html = await res.text();
  console.log(`[Parliament Crawler] Received ${html.length} bytes.`);
  fs.writeFileSync("parliament_members.html", html, "utf8");

  // Extract MP cards / links
  const mpRegex = /members\?mp=([0-9]+)/g;
  const mpIds = new Set();
  let m;
  while ((m = mpRegex.exec(html)) !== null) {
    mpIds.add(m[1]);
  }
  console.log(`[Parliament Crawler] Found ${mpIds.size} distinct MP IDs on the directory page.`);
}

crawlMembers().catch(console.error);

