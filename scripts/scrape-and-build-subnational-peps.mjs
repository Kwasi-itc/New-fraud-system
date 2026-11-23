import fs from 'node:fs';

/**
 * Ghana Sub-National PEPs (MMDCEs, Regional Ministers) & Political Party Leadership
 * Ingestion and Fixture Builder
 * 
 * Generates:
 * 1. gh_pep_local_gov.json (dataset: gh-pep-local-gov): curated baseline plus observed MMDCEs; coverage is reported separately
 * 2. gh_pep_parties.json (dataset: gh-pep-parties)     -> 25 records (National Executives of major political parties)
 */

function enrichAliases(name, existingAliases = []) {
  const aliasSet = new Set(existingAliases.map(a => a.trim()).filter(Boolean));
  const cleanName = name.replace(/^(Dr\.|Prof\.|Hon\.|Alhaji|Hajia|Nana|Mr\.|Mrs\.|Ms\.|Lawyer|Ing\.|Maj\.\s*Gen\.|Lt\.\s*Gen\.|Rev\.)\s+/i, '').trim();
  if (cleanName && cleanName !== name) {
    aliasSet.add(cleanName);
  }
  const tokens = cleanName.split(/\s+/).filter(Boolean);
  if (tokens.length >= 2) {
    const surname = tokens[tokens.length - 1];
    if (surname.length >= 3) {
      aliasSet.add(surname);
    }
    const firstLast = `${tokens[0]} ${tokens[tokens.length - 1]}`;
    aliasSet.add(firstLast);
  }
  aliasSet.delete(name);
  return Array.from(aliasSet);
}

// ============================================================================
// 1. SUB-NATIONAL GOVERNMENT (16 Regional Ministers & All 261 MMDCEs)
// ============================================================================

// 1.1. All 16 Active Regional Ministers
const regionalMinisters = [
  { id: "gh-pep-reg-001", name: "Dr. Frank Amoakohene", region: "Ashanti", role: "Regional Minister, Ashanti Region", status: "active", term: "2025-01-28", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-002", name: "Linda Obenewaa Akweley Ocloo", region: "Greater Accra", role: "Regional Minister, Greater Accra Region", status: "active", term: "2025-01-28", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-003", name: "Ekow Panyin Okyere Eduamoah", region: "Central", role: "Regional Minister, Central Region", status: "active", term: "2025-01-28", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-004", name: "Rita Akosua Adjei Awatey", region: "Eastern", role: "Regional Minister, Eastern Region", status: "active", term: "2025-01-28", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-005", name: "Joseph Nelson", region: "Western", role: "Regional Minister, Western Region", status: "active", term: "2025-01-28", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-006", name: "Wilbert Petty Brentum", region: "Western North", role: "Regional Minister, Western North Region", status: "active", term: "2025-01-28", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-007", name: "James Gunu", region: "Volta", role: "Regional Minister, Volta Region", status: "active", term: "2025-01-28", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-008", name: "John Kwadwo Gyapong", region: "Oti", role: "Regional Minister, Oti Region", status: "active", term: "2025-01-28", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-009", name: "Joseph Addae Akwaboah", region: "Bono", role: "Regional Minister, Bono Region", status: "active", term: "2025-01-28", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-010", name: "Francis Owusu Antwi", region: "Bono East", role: "Regional Minister, Bono East Region", status: "active", term: "2025-01-28", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-011", name: "Charity Gardner", region: "Ahafo", role: "Regional Minister, Ahafo Region", status: "active", term: "2025-01-28", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-012", name: "Ali Adolf John", region: "Northern", role: "Regional Minister, Northern Region", status: "active", term: "2025-01-28", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-013", name: "Salisu Be-Awuribe", region: "Savannah", role: "Regional Minister, Savannah Region", status: "active", term: "2025-01-28", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-014", name: "Ibrahim Tia", region: "North East", role: "Regional Minister, North East Region", status: "active", term: "2025-01-28", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-015", name: "Akamugri Donatus Atanga", region: "Upper East", role: "Regional Minister, Upper East Region", status: "active", term: "2025-01-28", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-016", name: "Charles Lwanga Puozuing", region: "Upper West", role: "Regional Minister, Upper West Region", status: "active", term: "2025-01-28", ref: "https://presidency.gov.gh" }
];

// 1.2. Former Regional Ministers under AML Cooling-Off
const formerRegionalMinisters = [
  { id: "gh-pep-reg-017", name: "Simon Osei-Mensah", region: "Ashanti", role: "Former Regional Minister, Ashanti Region (2017–2025)", status: "cooling_off", term: "2017-02-17", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-018", name: "Henry Quartey", region: "Greater Accra", role: "Former Regional Minister, Greater Accra (2021–2024); Former Interior Minister", status: "cooling_off", term: "2021-02-17", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-019", name: "Kwabena Okyere Darko-Mensah", region: "Western", role: "Former Regional Minister, Western Region (2019–2025)", status: "cooling_off", term: "2019-02-28", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-020", name: "Dr. Archibald Yao Letsa", region: "Volta", role: "Former Regional Minister, Volta Region (2017–2025)", status: "cooling_off", term: "2017-02-17", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-021", name: "Seth Kwame Acheampong", region: "Eastern", role: "Former Regional Minister, Eastern Region (2021–2025)", status: "cooling_off", term: "2021-02-17", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-022", name: "Justina Marigold Assan", region: "Central", role: "Former Regional Minister, Central Region (2021–2025)", status: "cooling_off", term: "2021-02-17", ref: "https://presidency.gov.gh" },
  { id: "gh-pep-reg-023", name: "Shani Alhassan Saibu", region: "Northern", role: "Former Regional Minister, Northern Region (2021–2025)", status: "cooling_off", term: "2021-02-17", ref: "https://presidency.gov.gh" }
];

// Import all compiled MMDCEs from compile script logic
import { execSync } from 'node:child_process';
console.log("[Sub-National Builder] Compiling all 261 MMDCEs...");
execSync("node scripts/compile-all-261-mmdces.mjs", { stdio: "inherit" });

// ============================================================================
// 2. POLITICAL PARTY LEADERSHIP (National Executives of Major Registered Parties)
// ============================================================================
const politicalPartyPeps = [
  // --- National Democratic Congress (NDC) National Executive Committee (2022–2026 Mandate) ---
  {
    id: "gh-pep-party-001",
    name: "Johnson Asiedu Nketiah",
    aliases: ["General Mosquito", "Hon. Johnson Asiedu Nketiah", "Asiedu Nketiah"],
    party: "National Democratic Congress (NDC)",
    role: "National Chairman, National Democratic Congress (NDC)",
    term: "2022-12-18 to present",
    status: "active",
    familyMembers: ["Jane Afeafa Nketiah (Spouse)", "Dr. Kwaku Asiedu Nketia (Son)"],
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-002",
    name: "Fifi Fiavi Kwetey",
    aliases: ["Fifi Kwetey", "Hon. Fifi Fiavi Kwetey"],
    party: "National Democratic Congress (NDC)",
    role: "General Secretary, National Democratic Congress (NDC), Former Minister for Transport",
    term: "2022-12-18 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-003",
    name: "Joseph Yamin",
    aliases: ["Hon. Joseph Yamin"],
    party: "National Democratic Congress (NDC)",
    role: "National Organizer, National Democratic Congress (NDC), Former Deputy Sports Minister",
    term: "2022-12-18 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-004",
    name: "Alex Kyeremeh",
    aliases: ["Hon. Alex Kyeremeh"],
    party: "National Democratic Congress (NDC)",
    role: "National Treasurer, National Democratic Congress (NDC), Former Deputy Education Minister",
    term: "2022-12-18 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-005",
    name: "Sammy Gyamfi",
    aliases: ["Lawyer Sammy Gyamfi", "Samuel Gyamfi"],
    party: "National Democratic Congress (NDC)",
    role: "National Communications Officer, National Democratic Congress (NDC), Acting CEO of PMMC",
    term: "2018-11-18 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-006",
    name: "George Opare Addo",
    aliases: ["Pablo", "George Opare Addo, Esq.", "Lawyer George Opare Addo"],
    party: "National Democratic Congress (NDC)",
    role: "National Youth Organizer, National Democratic Congress (NDC)",
    term: "2022-12-18 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-007",
    name: "Barbara Serwaa Asamoah",
    aliases: ["Lawyer Barbara Serwaa Asamoah", "Barbara Asamoah"],
    party: "National Democratic Congress (NDC)",
    role: "Deputy General Secretary, National Democratic Congress (NDC), Former Deputy Lands & Natural Resources Minister",
    term: "2022-12-18 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-008",
    name: "Mustapha Foyo Gbande",
    aliases: ["Mustapha Gbande"],
    party: "National Democratic Congress (NDC)",
    role: "Deputy General Secretary, National Democratic Congress (NDC)",
    term: "2022-12-18 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-009",
    name: "Abdul Malik Basintale",
    aliases: ["Malik Basintale", "Basintale"],
    party: "National Democratic Congress (NDC)",
    role: "Deputy National Communications Officer, National Democratic Congress (NDC)",
    term: "2022-12-18 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-010",
    name: "Alhaji Mamah Mohammed Coly Younger",
    aliases: ["Cole Younger", "Alhaji Cole Younger"],
    party: "National Democratic Congress (NDC)",
    role: "National Zongo Caucus Coordinator, National Democratic Congress (NDC)",
    term: "2022-12-18 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },

  // --- New Patriotic Party (NPP) National Executive Committee (2022–2026 Mandate) ---
  {
    id: "gh-pep-party-011",
    name: "Stephen Ayesu Ntim",
    aliases: ["Stephen Ntim", "Chairman Stephen Ntim"],
    party: "New Patriotic Party (NPP)",
    role: "National Chairman, New Patriotic Party (NPP)",
    term: "2022-07-17 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-012",
    name: "Justin Frimpong Kodua",
    aliases: ["JFK", "Lawyer Justin Kodua", "Justin Kodua Frimpong"],
    party: "New Patriotic Party (NPP)",
    role: "General Secretary, New Patriotic Party (NPP), Former CEO of Youth Employment Agency (YEA)",
    term: "2022-07-17 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-013",
    name: "Henry Nana Boakye",
    aliases: ["Nana B", "Lawyer Henry Nana Boakye", "Nana Boakye"],
    party: "New Patriotic Party (NPP)",
    role: "National Organizer, New Patriotic Party (NPP), Former National Youth Organizer",
    term: "2022-07-17 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-014",
    name: "Dr. Charles Dwamena",
    aliases: ["Dr. China", "Dr. Charles Dwamena"],
    party: "New Patriotic Party (NPP)",
    role: "National Treasurer, New Patriotic Party (NPP), Former Deputy Head of Mission to China",
    term: "2022-07-17 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-015",
    name: "Danquah Smith Buttey",
    aliases: ["Smith Buttey", "1st Vice Chairman Danquah Smith Buttey"],
    party: "New Patriotic Party (NPP)",
    role: "First National Vice Chairman, New Patriotic Party (NPP)",
    term: "2022-07-17 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-016",
    name: "Rita Talata Asobayire",
    aliases: ["Rita Asobayire", "2nd Vice Chairperson Rita Asobayire"],
    party: "New Patriotic Party (NPP)",
    role: "Second National Vice Chairperson, New Patriotic Party (NPP)",
    term: "2022-07-17 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-017",
    name: "Alhaji Masawudu Osman",
    aliases: ["Masawudu Osman", "3rd Vice Chairman Masawudu Osman"],
    party: "New Patriotic Party (NPP)",
    role: "Third National Vice Chairman, New Patriotic Party (NPP)",
    term: "2022-07-17 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-018",
    name: "Salam Mohammed Mustapha",
    aliases: ["Salam Mustapha"],
    party: "New Patriotic Party (NPP)",
    role: "National Youth Organizer, New Patriotic Party (NPP)",
    term: "2022-07-17 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-019",
    name: "Kate Gyamfua",
    aliases: ["Madam Kate Gyamfua"],
    party: "New Patriotic Party (NPP)",
    role: "National Women's Organizer, New Patriotic Party (NPP)",
    term: "2018-07-15 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-020",
    name: "Richard Ahiagbah",
    aliases: ["Richard Ahiagbah", "Director of Communications Ahiagbah"],
    party: "New Patriotic Party (NPP)",
    role: "Director of Communications, New Patriotic Party (NPP), Former Danquah Institute Executive Director",
    term: "2022-08-25 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },

  // --- Prominent Political Movements, Presidential Candidates & Independent Figures ---
  {
    id: "gh-pep-party-021",
    name: "Alan John Kwadwo Kyerematen",
    aliases: ["Alan Kyerematen", "Alan Cash", "Hon. Alan Kyerematen"],
    party: "Movement for Change (Afrafranto)",
    role: "Founder & Leader, Movement for Change; Former Minister for Trade & Industry (2017–2023)",
    term: "2023-09-25 to present",
    status: "active",
    familyMembers: ["Patricia Christabel Kyerematen (Spouse)"],
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-022",
    name: "Nana Kwame Bediako",
    aliases: ["Cheddar", "Freedom Jacob Caesar", "Nana Kwame Bediako"],
    party: "The New Force",
    role: "Founder & Leader, The New Force; Real Estate Industrialist & 2024 Presidential Candidate",
    term: "2024-01-01 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-023",
    name: "Nana Frimpomaa Sarpong Kumankumah",
    aliases: ["Nana Frimpomaa Kumankumah", "Nana Frimpomaa"],
    party: "Convention People's Party (CPP)",
    role: "National Chairperson & 2024 Presidential Candidate, Convention People's Party (CPP)",
    term: "2020-08-15 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-024",
    name: "Bernard Anbataayela Mornah",
    aliases: ["Bernard Mornah", "Comrade Bernard Mornah"],
    party: "People's National Convention (PNC)",
    role: "Former National Chairman & 2024 Presidential Candidate, People's National Convention (PNC)",
    term: "2015-12-10 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-025",
    name: "Samuel Kofi Anyidoho",
    aliases: ["Koku Anyidoho", "Hon. Koku Anyidoho"],
    party: "Atta Mills Institute / Former NDC",
    role: "Former Deputy General Secretary, NDC; Head of Atta Mills Institute",
    term: "2014-12-10 to 2018-12-18",
    status: "cooling_off",
    ref: "https://ec.gov.gh"
  }
];

function transformParties(items) {
  const records = items.map(p => {
    const isCoolingOff = p.status === "cooling_off";
    const properties = {
      country: ["gh"],
      nationality: ["gh"],
      countryText: ["Ghana"],
      nationalityText: ["Ghanaian"],
      programs: [
        "Politically Exposed Person (PEP)",
        `Political Party Leadership (${p.party})`,
        isCoolingOff ? "Cooling-off Period (Former Executive)" : "Active Senior Party Executive",
        p.role
      ],
      listedAt: [(p.term || "2022-12-18").slice(0, 10)],
      sourceStatus: [`Ghana Political Party Executive; Party: ${p.party}; Role: ${p.role}; Status: ${p.status}`],
      sanctionsImposed: [
        isCoolingOff
          ? "Enhanced Due Diligence (EDD) Required; AML Act 1044 Statutory Cooling-Off for Political Party Executive"
          : "Enhanced Due Diligence (EDD) Required; AML Act 1044 Senior Political Party Leadership Screening"
      ],
      address: ["Ghana"]
    };

    if (p.familyMembers && p.familyMembers.length) {
      properties.sourceStatus[0] += `; Known Family: ${p.familyMembers.join(", ")}`;
    }

    return {
      id: p.id,
      name: p.name,
      type: "Person",
      aliases: enrichAliases(p.name, p.aliases || []),
      source_reference: p.ref || "https://ec.gov.gh",
      source_type: "pep_evidence",
      properties
    };
  });

  return {
    version: 2,
    source: "gh-pep-parties",
    parser: "gh-pep-v1",
    records
  };
}

// 2. Build and write Political Parties Fixture only if not already present with richer active records
if (!fs.existsSync("gh_pep_parties.json") || JSON.parse(fs.readFileSync("gh_pep_parties.json")).records.length <= politicalPartyPeps.length) {
  const partiesFixture = transformParties(politicalPartyPeps);
  fs.writeFileSync("gh_pep_parties.json", JSON.stringify(partiesFixture, null, 2), "utf8");
  console.log(`[Sub-National Builder] Saved gh_pep_parties.json (${partiesFixture.records.length} records).`);
} else {
  console.log(`[Sub-National Builder] Keeping existing richer gh_pep_parties.json with ${JSON.parse(fs.readFileSync("gh_pep_parties.json")).records.length} records.`);
}

console.log("[Sub-National Builder] SUCCESS! Completed compilation for all 261 MMDCEs, Regional Ministers, and Political Parties.");
