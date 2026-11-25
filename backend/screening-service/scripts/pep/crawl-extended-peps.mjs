#!/usr/bin/env node
import fs from 'node:fs';
import path from 'node:path';

/**
 * Live Extended PEP Fetcher (Wikidata SPARQL)
 *
 * Covers Guideline categories missing from base scrapers:
 *  - traditional   -> gh_pep_traditional.json (Category 14: Traditional Rulers)
 *  - security      -> gh_pep_security.json    (Category 11: Senior Security Officials)
 *  - foreign_heads -> global_pep_heads.json   (Categories 1-2: Foreign Heads of State/Gov)
 *
 * Usage:
 *   node crawl-extended-peps.mjs                 # fetch all 3
 *   node crawl-extended-peps.mjs --only traditional
 */

const WIKIDATA_SPARQL_ENDPOINT = 'https://query.wikidata.org/sparql';
const USER_AGENT = 'AntiFraudScreeningEngine/1.0 (PEP Compliance Engine)';

function enrichAliases(name) {
  const aliasSet = new Set();
  const cleanName = name.replace(/^(Dr\.|Prof\.|Hon\.|Alhaji|Hajia|Nana|Otumfuo|Osagyefuo|Togbe|Mr\.|Mrs\.|Ms\.)\s+/i, '').trim();
  if (cleanName && cleanName !== name) aliasSet.add(cleanName);
  const tokens = cleanName.split(/\s+/).filter(Boolean);
  if (tokens.length >= 2) {
    if (tokens[tokens.length - 1].length >= 3) aliasSet.add(tokens[tokens.length - 1]);
    aliasSet.add(`${tokens[0]} ${tokens[tokens.length - 1]}`);
  }
  aliasSet.delete(name);
  return Array.from(aliasSet);
}

async function querySparql(sparql) {
  const url = `${WIKIDATA_SPARQL_ENDPOINT}?query=${encodeURIComponent(sparql)}&format=json`;
  const res = await fetch(url, {
    headers: { 'User-Agent': USER_AGENT, Accept: 'application/sparql-results+json' },
    signal: AbortSignal.timeout(30_000),
  });
  if (!res.ok) throw new Error(`Wikidata SPARQL HTTP ${res.status}`);
  const data = await res.json();
  return data.results?.bindings || [];
}

function isHumanLabel(label) {
  return label && !/^Q\d+$/.test(label);
}

async function fetchTraditionalRulers() {
  console.log('[Extended PEPs] Querying Wikidata for Ghanaian Traditional Rulers...');
  const rows = await querySparql(`
    PREFIX wd: <http://www.wikidata.org/entity/>
    PREFIX wdt: <http://www.wikidata.org/prop/direct/>
    PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
    SELECT DISTINCT ?person ?personLabel ?birthDate ?roleLabel WHERE {
      ?person wdt:P27 wd:Q117 .
      FILTER NOT EXISTS { ?person wdt:P570 ?died . }
      {
        ?person wdt:P39 ?role .
        ?role rdfs:label ?lbl . FILTER(LANG(?lbl)="en")
        FILTER(REGEX(?lbl, "Asantehene|Okyenhene|Ya-Na|Nayiri|Yagbonwura|paramount chief|traditional ruler|Queen mother|monarch", "i"))
      } UNION {
        ?person wdt:P97 ?role .
        ?role rdfs:label ?tlbl . FILTER(LANG(?tlbl)="en")
        FILTER(REGEX(?tlbl, "Asantehene|Okyenhene|Ya-Na|Nayiri|Yagbonwura|chief|king|queen|monarch|ruler|togbe|nana", "i"))
      }
      OPTIONAL { ?person wdt:P569 ?birthDate . }
      SERVICE wikibase:label { bd:serviceParam wikibase:language "en". }
    } LIMIT 300
  `);

  const byQid = new Map();
  for (const r of rows) {
    const qid = r.person?.value?.split('/').pop();
    const name = r.personLabel?.value;
    const role = r.roleLabel?.value;
    if (!qid || !isHumanLabel(name)) continue;
    if (!byQid.has(qid)) {
      byQid.set(qid, {
        qid,
        name,
        birthDate: r.birthDate?.value?.slice(0, 10) || null,
        roles: new Set(),
        ref: r.person.value,
      });
    }
    if (isHumanLabel(role)) byQid.get(qid).roles.add(role);
  }

  const today = new Date().toISOString().slice(0, 10);
  const records = Array.from(byQid.values()).map(p => {
    const roles = Array.from(p.roles);
    const props = {
      country: ['gh'],
      nationality: ['gh'],
      countryText: ['Ghana'],
      nationalityText: ['Ghanaian'],
      programs: ['Politically Exposed Person (PEP)', 'Traditional Ruler', ...roles],
      listedAt: [today],
      sourceStatus: [`Ghana Traditional Ruler (Wikidata: ${p.qid}); Titles: ${roles.join(', ') || 'Traditional Ruler'}`],
      sanctionsImposed: ['Enhanced Due Diligence (EDD) Required; AML Act 1044 Traditional Ruler PEP'],
      address: ['Ghana'],
    };
    if (p.birthDate) props.birthDate = [p.birthDate];
    return {
      id: `gh-wd-trad-${p.qid.toLowerCase()}`,
      name: p.name,
      type: 'Person',
      aliases: enrichAliases(p.name),
      source_reference: p.ref,
      source_type: 'pep_evidence',
      properties: props,
    };
  });

  if (records.length === 0) throw new Error('Zero traditional ruler records returned');
  const out = path.join(process.cwd(), 'gh_pep_traditional.json');
  fs.writeFileSync(out, JSON.stringify({ version: 2, source: 'gh-pep-traditional', parser: 'wikidata-sparql-v1', records }, null, 2), 'utf8');
  console.log(`[Extended PEPs] Saved ${records.length} traditional ruler records to ${out}`);
}

async function fetchSecurityOfficials() {
  console.log('[Extended PEPs] Querying Wikidata for Ghanaian Senior Security Officials...');
  const rows = await querySparql(`
    PREFIX wd: <http://www.wikidata.org/entity/>
    PREFIX wdt: <http://www.wikidata.org/prop/direct/>
    PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
    SELECT DISTINCT ?person ?personLabel ?birthDate ?roleLabel WHERE {
      ?person wdt:P27 wd:Q117 .
      ?person wdt:P39 ?role .
      FILTER NOT EXISTS { ?person wdt:P570 ?died . }
      ?role rdfs:label ?lbl . FILTER(LANG(?lbl)="en")
      FILTER(REGEX(?lbl, "Police|Defence|Army|Naval|Air Staff|Immigration|Prisons|Fire|Security", "i"))
      OPTIONAL { ?person wdt:P569 ?birthDate . }
      SERVICE wikibase:label { bd:serviceParam wikibase:language "en". }
    } LIMIT 300
  `);

  const byQid = new Map();
  for (const r of rows) {
    const qid = r.person?.value?.split('/').pop();
    const name = r.personLabel?.value;
    const role = r.roleLabel?.value;
    if (!qid || !isHumanLabel(name)) continue;
    if (!byQid.has(qid)) {
      byQid.set(qid, {
        qid,
        name,
        birthDate: r.birthDate?.value?.slice(0, 10) || null,
        roles: new Set(),
        ref: r.person.value,
      });
    }
    if (isHumanLabel(role)) byQid.get(qid).roles.add(role);
  }

  const today = new Date().toISOString().slice(0, 10);
  const records = Array.from(byQid.values()).map(p => {
    const roles = Array.from(p.roles);
    const props = {
      country: ['gh'],
      nationality: ['gh'],
      countryText: ['Ghana'],
      nationalityText: ['Ghanaian'],
      programs: ['Politically Exposed Person (PEP)', 'Senior Security Official', ...roles],
      listedAt: [today],
      sourceStatus: [`Ghana Senior Security Official (Wikidata: ${p.qid}); Roles: ${roles.join(', ')}`],
      sanctionsImposed: ['Enhanced Due Diligence (EDD) Required; AML Act 1044 Senior Security PEP'],
      address: ['Ghana'],
    };
    if (p.birthDate) props.birthDate = [p.birthDate];
    return {
      id: `gh-wd-sec-${p.qid.toLowerCase()}`,
      name: p.name,
      type: 'Person',
      aliases: enrichAliases(p.name),
      source_reference: p.ref,
      source_type: 'pep_evidence',
      properties: props,
    };
  });

  if (records.length === 0) throw new Error('Zero security official records returned');
  const out = path.join(process.cwd(), 'gh_pep_security.json');
  fs.writeFileSync(out, JSON.stringify({ version: 2, source: 'gh-pep-security', parser: 'wikidata-sparql-v1', records }, null, 2), 'utf8');
  console.log(`[Extended PEPs] Saved ${records.length} security official records to ${out}`);
}

async function fetchForeignHeads() {
  console.log('[Extended PEPs] Querying Wikidata for Foreign Heads of State & Government...');
  const rows = await querySparql(`
    PREFIX wd: <http://www.wikidata.org/entity/>
    PREFIX wdt: <http://www.wikidata.org/prop/direct/>
    SELECT DISTINCT ?person ?personLabel ?birthDate ?countryLabel ?kind WHERE {
      ?country wdt:P31 wd:Q3624078 .
      { ?country wdt:P35 ?person . BIND("Head of State" AS ?kind) }
      UNION
      { ?country wdt:P6 ?person . BIND("Head of Government" AS ?kind) }
      OPTIONAL { ?person wdt:P569 ?birthDate . }
      SERVICE wikibase:label { bd:serviceParam wikibase:language "en". }
    } LIMIT 600
  `);

  const byQid = new Map();
  for (const r of rows) {
    const qid = r.person?.value?.split('/').pop();
    const name = r.personLabel?.value;
    const country = r.countryLabel?.value;
    const kind = r.kind?.value || 'Head of State/Government';
    if (!qid || !isHumanLabel(name)) continue;
    if (!byQid.has(qid)) {
      byQid.set(qid, {
        qid,
        name,
        birthDate: r.birthDate?.value?.slice(0, 10) || null,
        roles: new Set(),
        countries: new Set(),
        ref: r.person.value,
      });
    }
    if (isHumanLabel(country)) {
      byQid.get(qid).roles.add(`${kind} of ${country}`);
      byQid.get(qid).countries.add(country);
    }
  }

  const today = new Date().toISOString().slice(0, 10);
  const records = Array.from(byQid.values()).map(p => {
    const roles = Array.from(p.roles);
    const countries = Array.from(p.countries);
    const props = {
      countryText: countries.length ? countries : ['International'],
      programs: ['Politically Exposed Person (PEP)', 'Foreign Head of State / Government', ...roles.slice(0, 10)],
      listedAt: [today],
      sourceStatus: [`Foreign PEP (Wikidata: ${p.qid}); Offices: ${roles.slice(0, 10).join(', ')}`],
      sanctionsImposed: ['Enhanced Due Diligence (EDD) Required; Foreign Head of State/Government PEP'],
    };
    if (p.birthDate) props.birthDate = [p.birthDate];
    return {
      id: `global-wd-head-${p.qid.toLowerCase()}`,
      name: p.name,
      type: 'Person',
      aliases: enrichAliases(p.name),
      source_reference: p.ref,
      source_type: 'pep_evidence',
      properties: props,
    };
  });

  if (records.length === 0) throw new Error('Zero foreign head of state/government records returned');
  const out = path.join(process.cwd(), 'global_pep_heads.json');
  fs.writeFileSync(out, JSON.stringify({ version: 2, source: 'global-pep-heads', parser: 'wikidata-sparql-v1', records }, null, 2), 'utf8');
  console.log(`[Extended PEPs] Saved ${records.length} foreign head of state/gov records to ${out}`);
}

async function main() {
  const idx = process.argv.indexOf('--only');
  const only = idx !== -1 ? process.argv[idx + 1] : null;
  if (!only || only === 'traditional') await fetchTraditionalRulers();
  if (!only || only === 'security') await fetchSecurityOfficials();
  if (!only || only === 'foreign_heads') await fetchForeignHeads();
}

main().catch(err => {
  console.error(`[Extended PEPs] ERROR: ${err.message}`);
  process.exit(1);
});
