async function probe() {
  const res = await fetch("https://parliament.gh", {
    headers: { "User-Agent": "Mozilla/5.0" }
  });
  const html = await res.text();
  const re = /href=["']([^"']+)["']/gi;
  let match;
  const links = new Set();
  while ((match = re.exec(html)) !== null) {
    const l = match[1];
    if (/mp|member|leadership|parliament/i.test(l)) {
      links.add(l);
    }
  }
  console.log("Found links:", Array.from(links).slice(0, 30));
}
probe().catch(console.error);

