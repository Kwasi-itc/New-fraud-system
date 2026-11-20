import fs from 'node:fs';
import path from 'node:path';

/**
 * Ghana Senior Political Party Officials PEP Crawler & Registry
 * 
 * Statutory Mandate:
 * - Anti-Money Laundering Act, 2020 (Act 1044), Section 59(1)(f):
 *   "Politically exposed person includes senior political party officials"
 * - FATF Recommendation 12: Domestic PEPs
 * - Political Parties Act, 2000 (Act 574)
 * 
 * Regulates:
 * National Executive Committees (NEC) of registered political parties in Ghana:
 * 1. New Patriotic Party (NPP)
 * 2. National Democratic Congress (NDC)
 * 3. Convention People's Party (CPP)
 * 4. People's National Convention (PNC)
 * 
 * Output: gh_pep_parties.json
 */

const POLITICAL_PARTIES_DATA = [
  // =========================================================================
  // NEW PATRIOTIC PARTY (NPP) - NATIONAL EXECUTIVES
  // =========================================================================
  {
    name: "Stephen Ayesu Ntim",
    party: "New Patriotic Party (NPP)",
    partyCode: "NPP",
    role: "National Chairman, New Patriotic Party (NPP)",
    aliases: ["Chairman Stephen Ntim", "Stephen Ntim", "Ntim"],
    status: "active",
    family: ["Richmond Ntim (Son)"],
    ref: "https://newpatrioticparty.org"
  },
  {
    name: "Danquah Smith Buttey",
    party: "New Patriotic Party (NPP)",
    partyCode: "NPP",
    role: "1st Vice Chairman, New Patriotic Party (NPP)",
    aliases: ["Smith Buttey", "Danquah Buttey"],
    status: "active",
    ref: "https://newpatrioticparty.org"
  },
  {
    name: "Rita Talata Asobayire",
    party: "New Patriotic Party (NPP)",
    partyCode: "NPP",
    role: "2nd Vice Chairperson, New Patriotic Party (NPP)",
    aliases: ["Rita Asobayire", "Talata Asobayire"],
    status: "active",
    ref: "https://newpatrioticparty.org"
  },
  {
    name: "Alhaji Masawudu Osman",
    party: "New Patriotic Party (NPP)",
    partyCode: "NPP",
    role: "3rd Vice Chairman, New Patriotic Party (NPP)",
    aliases: ["Masawudu Osman", "Alhaji Masawudu"],
    status: "active",
    ref: "https://newpatrioticparty.org"
  },
  {
    name: "Justin Kodua Frimpong",
    party: "New Patriotic Party (NPP)",
    partyCode: "NPP",
    role: "General Secretary, New Patriotic Party (NPP)",
    aliases: ["JFK", "Justin Frimpong Kodua", "Lawyer Justin Kodua", "Kodua Frimpong"],
    status: "active",
    ref: "https://newpatrioticparty.org"
  },
  {
    name: "Henry Nana Boakye",
    party: "New Patriotic Party (NPP)",
    partyCode: "NPP",
    role: "National Organiser, New Patriotic Party (NPP)",
    aliases: ["Nana B", "Henry Boakye", "Lawyer Nana Boakye"],
    status: "active",
    ref: "https://newpatrioticparty.org"
  },
  {
    name: "Dr. Charles Dwamena",
    party: "New Patriotic Party (NPP)",
    partyCode: "NPP",
    role: "National Treasurer, New Patriotic Party (NPP)",
    aliases: ["Dr. China", "Charles Dwamena"],
    status: "active",
    ref: "https://newpatrioticparty.org"
  },
  {
    name: "Kate Gyamfua",
    party: "New Patriotic Party (NPP)",
    partyCode: "NPP",
    role: "National Women's Organiser, New Patriotic Party (NPP)",
    aliases: ["Madam Kate Gyamfua", "Gyamfua"],
    status: "active",
    ref: "https://newpatrioticparty.org"
  },
  {
    name: "Salam Mohammed Mustapha",
    party: "New Patriotic Party (NPP)",
    partyCode: "NPP",
    role: "National Youth Organiser, New Patriotic Party (NPP)",
    aliases: ["Salam Mustapha", "Commander Salam"],
    status: "active",
    ref: "https://newpatrioticparty.org"
  },
  {
    name: "Abdul Aziz Haruna Futa",
    party: "New Patriotic Party (NPP)",
    partyCode: "NPP",
    role: "National Nasara Coordinator, New Patriotic Party (NPP)",
    aliases: ["Aziz Futa", "Alhaji Aziz Futa"],
    status: "active",
    ref: "https://newpatrioticparty.org"
  },
  {
    name: "Richard Ahiagbah",
    party: "New Patriotic Party (NPP)",
    partyCode: "NPP",
    role: "Director of Communications, New Patriotic Party (NPP)",
    aliases: ["Ahiagbah"],
    status: "active",
    ref: "https://newpatrioticparty.org"
  },
  // NPP Former Executives (AML Cooling-Off)
  {
    name: "Freddie Blay",
    party: "New Patriotic Party (NPP)",
    partyCode: "NPP",
    role: "Former National Chairman, New Patriotic Party (NPP) (2018–2022)",
    aliases: ["Frederick Worsemao Armah Blay", "Chairman Blay"],
    status: "cooling_off",
    ref: "https://newpatrioticparty.org"
  },
  {
    name: "John Boadu",
    party: "New Patriotic Party (NPP)",
    partyCode: "NPP",
    role: "Former General Secretary, New Patriotic Party (NPP) (2018–2022)",
    aliases: ["Boadu"],
    status: "cooling_off",
    ref: "https://newpatrioticparty.org"
  },
  {
    name: "Sammi Awuku",
    party: "New Patriotic Party (NPP)",
    partyCode: "NPP",
    role: "Former National Organiser, New Patriotic Party (NPP) (2018–2022)",
    aliases: ["Samuel Awuku", "Boys Abre"],
    status: "cooling_off",
    ref: "https://newpatrioticparty.org"
  },
  {
    name: "Kwabena Abankwah Yeboah",
    party: "New Patriotic Party (NPP)",
    partyCode: "NPP",
    role: "Former National Treasurer, New Patriotic Party (NPP)",
    aliases: ["Abankwah Yeboah"],
    status: "cooling_off",
    ref: "https://newpatrioticparty.org"
  },

  // =========================================================================
  // NATIONAL DEMOCRATIC CONGRESS (NDC) - NATIONAL EXECUTIVES
  // =========================================================================
  {
    name: "Johnson Asiedu Nketiah",
    party: "National Democratic Congress (NDC)",
    partyCode: "NDC",
    role: "National Chairman, National Democratic Congress (NDC)",
    aliases: ["General Mosquito", "Asiedu Nketiah", "Chairman Asiedu Nketiah"],
    status: "active",
    family: ["Charles Asiedu (Son)"],
    ref: "https://ndc.org.gh"
  },
  {
    name: "Awudu Sofo Azorka",
    party: "National Democratic Congress (NDC)",
    partyCode: "NDC",
    role: "1st National Vice Chairman, National Democratic Congress (NDC)",
    aliases: ["Chief Azorka", "Sofo Azorka"],
    status: "active",
    ref: "https://ndc.org.gh"
  },
  {
    name: "Abanga Yakubu Alhassan",
    party: "National Democratic Congress (NDC)",
    partyCode: "NDC",
    role: "National Vice Chairman, National Democratic Congress (NDC)",
    aliases: ["Yakubu Alhassan"],
    status: "active",
    ref: "https://ndc.org.gh"
  },
  {
    name: "Fifi Fiavi Kwetey",
    party: "National Democratic Congress (NDC)",
    partyCode: "NDC",
    role: "General Secretary, National Democratic Congress (NDC)",
    aliases: ["Fifi Kwetey", "Fiavi Kwetey"],
    status: "active",
    ref: "https://ndc.org.gh"
  },
  {
    name: "Mustapha Gbande",
    party: "National Democratic Congress (NDC)",
    partyCode: "NDC",
    role: "Deputy General Secretary (Operations), National Democratic Congress (NDC)",
    aliases: ["Gbande"],
    status: "active",
    ref: "https://ndc.org.gh"
  },
  {
    name: "Barbara Serwaa Asamoah",
    party: "National Democratic Congress (NDC)",
    partyCode: "NDC",
    role: "Deputy General Secretary (Administration), National Democratic Congress (NDC)",
    aliases: ["Barbara Asamoah"],
    status: "active",
    ref: "https://ndc.org.gh"
  },
  {
    name: "Joseph Yamin",
    party: "National Democratic Congress (NDC)",
    partyCode: "NDC",
    role: "National Organiser, National Democratic Congress (NDC)",
    aliases: ["Yamin"],
    status: "active",
    ref: "https://ndc.org.gh"
  },
  {
    name: "Alex Kyeremeh",
    party: "National Democratic Congress (NDC)",
    partyCode: "NDC",
    role: "National Treasurer, National Democratic Congress (NDC)",
    aliases: ["Kyeremeh"],
    status: "active",
    ref: "https://ndc.org.gh"
  },
  {
    name: "Thomas Hughes Amissah",
    party: "National Democratic Congress (NDC)",
    partyCode: "NDC",
    role: "Deputy National Treasurer, National Democratic Congress (NDC)",
    aliases: ["Hughes Amissah"],
    status: "active",
    ref: "https://ndc.org.gh"
  },
  {
    name: "George Opare Addo",
    party: "National Democratic Congress (NDC)",
    partyCode: "NDC",
    role: "National Youth Organiser, National Democratic Congress (NDC)",
    aliases: ["Pablo", "Opare Addo"],
    status: "active",
    ref: "https://ndc.org.gh"
  },
  {
    name: "Dr. Louisa Hannah Bissiw",
    party: "National Democratic Congress (NDC)",
    partyCode: "NDC",
    role: "National Women's Organiser, National Democratic Congress (NDC)",
    aliases: ["Hannah Bissiw", "Dr. Hannah Bissiw"],
    status: "active",
    ref: "https://ndc.org.gh"
  },
  {
    name: "Sammy Gyamfi",
    party: "National Democratic Congress (NDC)",
    partyCode: "NDC",
    role: "National Communications Officer, National Democratic Congress (NDC)",
    aliases: ["Sammy Gyamfi, Esq.", "Lawyer Sammy Gyamfi"],
    status: "active",
    ref: "https://ndc.org.gh"
  },
  {
    name: "Malik Basintale",
    party: "National Democratic Congress (NDC)",
    partyCode: "NDC",
    role: "Deputy National Communications Officer, National Democratic Congress (NDC)",
    aliases: ["Basintale"],
    status: "active",
    ref: "https://ndc.org.gh"
  },
  // NDC Former Executives (AML Cooling-Off)
  {
    name: "Samuel Ofosu-Ampofo",
    party: "National Democratic Congress (NDC)",
    partyCode: "NDC",
    role: "Former National Chairman, National Democratic Congress (NDC) (2018–2022)",
    aliases: ["Elder Ofosu-Ampofo", "Ofosu Ampofo"],
    status: "cooling_off",
    ref: "https://ndc.org.gh"
  },
  {
    name: "Peter Boamah Otokunor",
    party: "National Democratic Congress (NDC)",
    partyCode: "NDC",
    role: "Former Deputy General Secretary, National Democratic Congress (NDC)",
    aliases: ["Dr. Peter Otokunor", "Otokunor"],
    status: "cooling_off",
    ref: "https://ndc.org.gh"
  },

  // =========================================================================
  // OTHER REGISTERED PARTIES (CPP, PNC, PPP)
  // =========================================================================
  {
    name: "Nana Frimpomaa Sarpong Kumankumah",
    party: "Convention People's Party (CPP)",
    partyCode: "CPP",
    role: "National Chairperson and Leader, Convention People's Party (CPP)",
    aliases: ["Nana Frimpomaa", "Kumankumah"],
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    name: "Nana Yaa Jantuah",
    party: "Convention People's Party (CPP)",
    partyCode: "CPP",
    role: "Former General Secretary, Convention People's Party (CPP)",
    aliases: ["Yaa Jantuah"],
    status: "cooling_off",
    ref: "https://ec.gov.gh"
  },
  {
    name: "Moses Dani Baah",
    party: "People's National Convention (PNC)",
    partyCode: "PNC",
    role: "National Chairman, People's National Convention (PNC)",
    aliases: ["Dani Baah"],
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    name: "Janet Asana Nabla",
    party: "People's National Convention (PNC)",
    partyCode: "PNC",
    role: "General Secretary, People's National Convention (PNC)",
    aliases: ["Janet Nabla"],
    status: "active",
    ref: "https://ec.gov.gh"
  }
];

function formatPartyRecord(idx, item) {
  const statusLabel = item.status === "active" ? "Active Office Holder" : "Former Official (AML Cooling-Off)";
  const sanctionNote = item.status === "active"
    ? "Enhanced Due Diligence (EDD) Required; AML Act 1044 Active PEP Screening (Section 59(1)(f))"
    : "Statutory 1-Year AML Cooling-Off Period (Act 1044); Monitored Status";

  const familyNote = item.family ? `; Known Family: ${item.family.join(', ')}` : '';

  return {
    id: `gh-pep-party-${String(idx).padStart(4, '0')}`,
    name: item.name,
    type: "Person",
    aliases: item.aliases || [item.name.split(' ').pop()],
    source_reference: item.ref,
    source_type: "pep_evidence",
    properties: {
      country: ["gh"],
      nationality: ["gh"],
      countryText: ["Ghana"],
      nationalityText: ["Ghanaian"],
      programs: [
        "Politically Exposed Person (PEP)",
        statusLabel,
        item.role,
        `Political Party: ${item.party}`
      ],
      listedAt: ["2024-01-01"],
      sourceStatus: [
        `Ghana PEP; Role: ${item.role}; Status: ${item.status}; Party: ${item.partyCode}${familyNote}`
      ],
      sanctionsImposed: [
        sanctionNote
      ],
      address: ["Ghana"]
    }
  };
}

function run() {
  console.log("========================================================================");
  console.log("     GHANA SENIOR POLITICAL PARTY OFFICIALS (ACT 1044) REGISTRY         ");
  console.log("========================================================================");
  console.log("Legal Mandate: AML Act, 2020 (Act 1044) Section 59(1)(f) & FATF Rec 12");
  console.log("Parties Covered: NPP, NDC, CPP, PNC (National Executive Committees)\n");

  const records = POLITICAL_PARTIES_DATA.map((p, idx) => formatPartyRecord(idx + 1, p));

  const payload = {
    version: 2,
    source: "gh-pep-party",
    parser: "gh-pep-v1",
    description: "Ghana Senior Political Party Officials (Act 1044 Sec 59(1)(f)) PEP Registry",
    last_updated_at: new Date().toISOString(),
    total_records: records.length,
    records: records
  };

  const outputPath = "gh_pep_parties.json";
  fs.writeFileSync(outputPath, JSON.stringify(payload, null, 2), "utf8");
  console.log(`[✓] Generated ${records.length} Senior Political Party PEP records in ${outputPath}`);

  // Also sync to scripts/
  const scriptsPath = path.join("scripts", "gh_pep_parties.json");
  fs.writeFileSync(scriptsPath, JSON.stringify(payload, null, 2), "utf8");
  console.log(`[✓] Synced to ${scriptsPath}`);

  const activeCount = records.filter(r => r.properties.sourceStatus[0].includes("active")).length;
  const coolingCount = records.filter(r => r.properties.sourceStatus[0].includes("cooling_off")).length;

  console.log("\nBreakdown by Political Affiliation:");
  ["NPP", "NDC", "CPP", "PNC"].forEach(code => {
    const pCount = records.filter(r => r.properties.sourceStatus[0].includes(`Party: ${code}`)).length;
    console.log(`  - ${code.padEnd(6)}: ${pCount} National Executives`);
  });

  console.log(`\nActive Sitting Officials:  ${activeCount}`);
  console.log(`AML Cooling-Off Retained:  ${coolingCount}`);
  console.log("========================================================================\n");
}

run();
