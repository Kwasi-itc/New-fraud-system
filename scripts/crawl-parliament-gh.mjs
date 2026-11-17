import fs from 'node:fs';

async function testParliamentUrls() {
  const candidates = [
    "https://www.parliament.gh/mps?leadership=true",
    "https://www.parliament.gh/mps?az",
    "https://www.parliament.gh/mps.php"
  ];

  for (const url of candidates) {
    console.log(`[Parliament Crawler] Trying ${url}...`);
    try {
      const res = await fetch(url, {
        headers: {
          "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
          "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
        },
        signal: AbortSignal.timeout(10000)
      });
      console.log(`[Parliament Crawler] ${url} -> HTTP ${res.status}`);
      if (res.ok) {
        const text = await res.text();
        console.log(`[Parliament Crawler] SUCCESS! Got ${text.length} bytes from ${url}`);
        fs.writeFileSync("parliament_mps.html", text, "utf8");
        return;
      }
    } catch (e) {
      console.log(`[Parliament Crawler] ${url} -> Error: ${e.message}`);
    }
  }
}

testParliamentUrls();
