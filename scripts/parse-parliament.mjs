import fs from 'node:fs';

const html = fs.readFileSync('parliament_members.html', 'utf8');

// Search for pagination or all pages in parliament.gh/members
const pageMatch = html.match(/page=([0-9]+)/g);
console.log("Pages found:", pageMatch ? [...new Set(pageMatch)] : "none");

// Extract MP cards
// On parliament.gh, cards usually contain name, constituency, region, party
const cardRegex = /<div class="card[^"]*"[\s\S]*?members\?mp=([0-9]+)[\s\S]*?<\/div>\s*<\/div>/gi;
const mpList = [];

// Simple link regex
const linkRegex = /href=["']members\?mp=([0-9]+)["'][^>]*>([\s\S]*?)<\/a>/gi;
let m;
while ((m = linkRegex.exec(html)) !== null) {
  const clean = m[2].replace(/<[^>]+>/g, '').trim();
  if (clean && clean.length > 3 && !clean.includes("Read More")) {
    mpList.push({ id: m[1], text: clean });
  }
}

console.log("Sample extracted MPs:", mpList.slice(0, 10));

