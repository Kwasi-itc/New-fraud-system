import fs from 'node:fs';

/**
 * Fast Multi-Source Wikidata SPARQL Sync Engine for Ghana PEPs & RCAs
 * 
 * Queries all political offices with jurisdiction Ghana (wd:Q1001 wd:Q117)
 * plus President, VP, Speaker, Chief Justice, extracting:
 * 1. PEPs with positions, dates of birth, Wikidata Q-IDs
 * 2. Connected Family/RCAs (P26 Spouse, P40 Child, P3373 Sibling, P22 Father, P25 Mother)
 */

const WIKIDATA_SPARQL_ENDPOINT = "https://query.wikidata.org/sparql";
const USER_AGENT = "AntiFraudScreeningEngine/1.0 (Ghana PEP & RCA Compliance Engine; dev-screening@local)";

async function querySparql(sparql) {
  const url = `${WIKIDATA_SPARQL_ENDPOINT}?query=${encodeURIComponent(sparql)}&format=json`;
  const response = await fetch(url, {
    headers: {
      "User-Agent": USER_AGENT,
      "Accept": "application/sparql-results+json"
    }
  });

  if (!response.ok) {
    throw new Error(`Wikidata SPARQL HTTP ${response.status}: ${response.statusText}`);
  }

  const data = await response.json();
  return data.results?.bindings || [];
}

const PEPS_QUERY = `
PREFIX wd: <http://www.wikidata.org/entity/>
PREFIX wdt: <http://www.wikidata.org/prop/direct/>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>

SELECT DISTINCT ?person ?personLabel ?birthDate ?positionLabel WHERE {
  {
    ?position wdt:P1001 wd:Q117 . # jurisdiction: Ghana
  } UNION {
    VALUES ?position {
      wd:Q506720   # President of Ghana
      wd:Q3556942  # Vice President of Ghana
      wd:Q7144005  # Speaker of Parliament of Ghana
      wd:Q5096899  # Chief Justice of Ghana
    }
  }
  ?person wdt:P39 ?position .
  OPTIONAL { ?person wdt:P569 ?birthDate . }
  
  SERVICE wikibase:label { 
    bd:serviceParam wikibase:language "en" . 
    ?person rdfs:label ?personLabel .
    ?position rdfs:label ?positionLabel .
  }
}
LIMIT 2000
`;

const RELATIVES_QUERY = `
PREFIX wd: <http://www.wikidata.org/entity/>
PREFIX wdt: <http://www.wikidata.org/prop/direct/>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>

SELECT DISTINCT ?person ?personLabel ?relType ?relative ?relativeLabel WHERE {
  {
    ?position wdt:P1001 wd:Q117 . # jurisdiction: Ghana
  } UNION {
    VALUES ?position {
      wd:Q506720   # President of Ghana
      wd:Q3556942  # Vice President of Ghana
      wd:Q7144005  # Speaker of Parliament of Ghana
      wd:Q5096899  # Chief Justice of Ghana
    }
  }
  ?person wdt:P39 ?position .
  
  VALUES (?prop ?relType) {
    (wdt:P26 "Spouse")
    (wdt:P40 "Child")
    (wdt:P3373 "Sibling")
    (wdt:P22 "Father")
    (wdt:P25 "Mother")
  }
  ?person ?prop ?relative .
  
  SERVICE wikibase:label { 
    bd:serviceParam wikibase:language "en" . 
    ?person rdfs:label ?personLabel .
    ?relative rdfs:label ?relativeLabel .
  }
}
LIMIT 2000
`;

async function main() {
  console.log(`[Wikidata Sync] 1/2: Querying Ghana PEPs from Wikidata...`);
  const pepBindings = await querySparql(PEPS_QUERY);
  console.log(`[Wikidata Sync] Retrieved ${pepBindings.length} PEP position records.`);

  console.log(`[Wikidata Sync] 2/2: Querying connected Family/RCA Graph...`);
  const relBindings = await querySparql(RELATIVES_QUERY);
  console.log(`[Wikidata Sync] Retrieved ${relBindings.length} connected Family/RCA relationships.`);

  // Consolidate PEPs
  const pepsMap = new Map();
  for (const b of pepBindings) {
    const qid = b.person?.value?.split('/').pop();
    const name = b.personLabel?.value;
    if (!qid || !name || name === qid) continue;

    if (!pepsMap.has(qid)) {
      pepsMap.set(qid, {
        qid,
        name,
        birthDate: b.birthDate?.value ? b.birthDate.value.slice(0, 10) : null,
        positions: new Set(),
        ref: b.person.value,
        family: []
      });
    }

    if (b.positionLabel?.value) {
      pepsMap.get(qid).positions.add(b.positionLabel.value);
    }
  }

  // Consolidate RCAs and link to PEPs
  const rcasMap = new Map();
  for (const r of relBindings) {
    const pepQid = r.person?.value?.split('/').pop();
    const pepName = r.personLabel?.value;
    const relQid = r.relative?.value?.split('/').pop();
    const relName = r.relativeLabel?.value;
    const relType = r.relType?.value || "Relative";

    if (!relQid || !relName || relName === relQid) continue;

    // Add to PEP's family list
    if (pepsMap.has(pepQid)) {
      pepsMap.get(pepQid).family.push(`${relName} (${relType})`);
    }

    // Add or update RCA
    if (!rcasMap.has(relQid)) {
      rcasMap.set(relQid, {
        qid: relQid,
        name: relName,
        relationship: relType,
        ref: r.relative.value,
        connectedPeps: new Set()
      });
    }
    if (pepName) {
      rcasMap.get(relQid).connectedPeps.add(pepName);
    }
  }

  // Build Version 2 Records
  const pepRecords = Array.from(pepsMap.values()).map(p => {
    const posList = Array.from(p.positions);
    const programs = ["Politically Exposed Person (PEP)", ...posList];
    const uniqueFamily = [...new Set(p.family)];

    let statusDesc = `Ghana PEP (Wikidata: ${p.qid}); Positions: ${posList.join("; ") || "Public Official"}`;
    if (uniqueFamily.length) {
      statusDesc += `; Known Family (Wikidata Graph): ${uniqueFamily.join(", ")}`;
    }

    const properties = {
      country: ["gh"],
      nationality: ["gh"],
      countryText: ["Ghana"],
      nationalityText: ["Ghanaian"],
      programs,
      sourceStatus: [statusDesc],
      sanctionsImposed: ["Enhanced Due Diligence (EDD) Required; AML Act 1044 PEP Screening"],
      address: ["Ghana"]
    };

    if (p.birthDate) {
      properties.birthDate = [p.birthDate];
    }

    return {
      id: `gh-wd-${p.qid.toLowerCase()}`,
      name: p.name,
      type: "Person",
      aliases: [],
      source_reference: p.ref,
      source_type: "pep_evidence",
      properties
    };
  });

  const rcaRecords = Array.from(rcasMap.values()).map(r => {
    const pepsList = Array.from(r.connectedPeps);
    const properties = {
      country: ["gh"],
      nationality: ["gh"],
      countryText: ["Ghana"],
      nationalityText: ["Ghanaian"],
      programs: [
        "Relative or Close Associate (RCA)",
        `Relationship: ${r.relationship}`,
        `Connected PEPs: ${pepsList.join(", ")}`
      ],
      sourceStatus: [
        `Ghana RCA (Wikidata: ${r.qid}); Relationship: ${r.relationship}; Connected PEP: ${pepsList.join(", ")}`
      ],
      sanctionsImposed: [
        `Enhanced Due Diligence (EDD) Required; AML Act 1044 RCA Screening [${r.relationship} of ${pepsList.join(", ")}]`
      ],
      address: ["Ghana"]
    };

    return {
      id: `gh-wd-rca-${r.qid.toLowerCase()}`,
      name: r.name,
      type: "Person",
      aliases: [],
      source_reference: r.ref,
      source_type: "rca_evidence",
      properties
    };
  });

  console.log(`[Wikidata Sync] Generated ${pepRecords.length} Wikidata PEPs and ${rcaRecords.length} Wikidata RCAs.`);

  const pepsSnapshot = {
    version: 2,
    source: "gh-pep-wikidata",
    parser: "wikidata-sparql-v1",
    records: pepRecords
  };

  const rcasSnapshot = {
    version: 2,
    source: "gh-rca-wikidata",
    parser: "wikidata-sparql-v1",
    records: rcaRecords
  };

  fs.writeFileSync("gh_pep_wikidata.json", JSON.stringify(pepsSnapshot, null, 2), "utf8");
  console.log(`[Wikidata Sync] Saved gh_pep_wikidata.json (${pepsSnapshot.records.length} records)`);

  fs.writeFileSync("gh_rca_wikidata.json", JSON.stringify(rcasSnapshot, null, 2), "utf8");
  console.log(`[Wikidata Sync] Saved gh_rca_wikidata.json (${rcasSnapshot.records.length} records)`);

  console.log(`[Wikidata Sync] SUCCESS! All data fetched and saved.`);
}

main().catch(err => {
  console.error(`[Wikidata Sync] Failed:`, err);
  process.exit(1);
});
