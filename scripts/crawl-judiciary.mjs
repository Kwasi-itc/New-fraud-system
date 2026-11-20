import fs from 'node:fs';
import path from 'node:path';

/**
 * Judicial Service of Ghana PEP Crawler & Registry
 * 
 * Statutory Mandates:
 * - Anti-Money Laundering Act, 2020 (Act 1044), Section 59(1):
 *   "Politically exposed person includes senior judicial figures"
 * - 1992 Constitution of the Republic of Ghana (Chapter 11: The Judiciary)
 * - Judicial Service Act
 * 
 * Scope:
 * 1. Chief Justice of the Republic of Ghana & Head of the Judiciary
 * 2. Justices of the Supreme Court of Ghana (Apex Court)
 * 3. Judicial Secretary & Senior Judicial Service Administration
 * 4. Senior Court of Appeal Leadership
 * 5. Former Chief Justices under Statutory AML Cooling-Off Window
 * 
 * Output: gh_pep_judiciary.json
 */

const JUDICIARY_DATA = [
  // =========================================================================
  // APEX JUDICIARY: CHIEF JUSTICE & SUPREME COURT JUSTICES
  // =========================================================================
  {
    name: "Gertrude Araba Esaaba Torkornoo",
    role: "Chief Justice of the Republic of Ghana, Head of the Judiciary",
    court: "Supreme Court of Ghana",
    aliases: ["Chief Justice Gertrude Torkornoo", "Gertrude Torkornoo", "Justice Torkornoo", "Torkornoo"],
    status: "active",
    appointedAt: "2023-06-12",
    family: ["Francis Torkornoo (Spouse)"],
    ref: "https://judicial.gov.gh"
  },
  {
    name: "Paul Baffoe-Bonnie",
    role: "Senior Justice of the Supreme Court of Ghana",
    court: "Supreme Court of Ghana",
    aliases: ["Justice Paul Baffoe-Bonnie", "Baffoe-Bonnie"],
    status: "active",
    appointedAt: "2008-06-11",
    ref: "https://judicial.gov.gh"
  },
  {
    name: "Gabriel Pwamang",
    role: "Justice of the Supreme Court of Ghana",
    court: "Supreme Court of Ghana",
    aliases: ["Justice Gabriel Pwamang", "Pwamang"],
    status: "active",
    appointedAt: "2015-06-29",
    ref: "https://judicial.gov.gh"
  },
  {
    name: "Mariama Owusu",
    role: "Justice of the Supreme Court of Ghana",
    court: "Supreme Court of Ghana",
    aliases: ["Justice Mariama Owusu"],
    status: "active",
    appointedAt: "2019-12-17",
    ref: "https://judicial.gov.gh"
  },
  {
    name: "Avril Lovelace-Johnson",
    role: "Justice of the Supreme Court of Ghana",
    court: "Supreme Court of Ghana",
    aliases: ["Justice Avril Lovelace-Johnson", "Lovelace-Johnson"],
    status: "active",
    appointedAt: "2019-12-17",
    ref: "https://judicial.gov.gh"
  },
  {
    name: "Issifu Omoro Tanko Amadu",
    role: "Justice of the Supreme Court of Ghana",
    court: "Supreme Court of Ghana",
    aliases: ["Justice Tanko Amadu", "Tanko Amadu"],
    status: "active",
    appointedAt: "2020-05-22",
    ref: "https://judicial.gov.gh"
  },
  {
    name: "Prof. Henrietta Mensa-Bonsu",
    role: "Justice of the Supreme Court of Ghana",
    court: "Supreme Court of Ghana",
    aliases: ["Justice Henrietta Mensa-Bonsu", "Henrietta Mensa-Bonsu", "Mensa-Bonsu"],
    status: "active",
    appointedAt: "2020-05-26",
    ref: "https://judicial.gov.gh"
  },
  {
    name: "Emmanuel Yonny Kulendi",
    role: "Justice of the Supreme Court of Ghana",
    court: "Supreme Court of Ghana",
    aliases: ["Justice Yonny Kulendi", "Yonny Kulendi", "Kulendi"],
    status: "active",
    appointedAt: "2020-05-26",
    ref: "https://judicial.gov.gh"
  },
  {
    name: "Barbara Frances Ackah-Yensu",
    role: "Justice of the Supreme Court of Ghana",
    court: "Supreme Court of Ghana",
    aliases: ["Justice Barbara Ackah-Yensu", "Ackah-Yensu"],
    status: "active",
    appointedAt: "2022-12-28",
    ref: "https://judicial.gov.gh"
  },
  {
    name: "Samuel Kwame Adibu Asiedu",
    role: "Justice of the Supreme Court of Ghana",
    court: "Supreme Court of Ghana",
    aliases: ["Justice Samuel Adibu Asiedu", "Adibu Asiedu"],
    status: "active",
    appointedAt: "2022-12-28",
    ref: "https://judicial.gov.gh"
  },
  {
    name: "George Kingsley Koomson",
    role: "Justice of the Supreme Court of Ghana",
    court: "Supreme Court of Ghana",
    aliases: ["Justice George Koomson", "Koomson"],
    status: "active",
    appointedAt: "2023-04-05",
    ref: "https://judicial.gov.gh"
  },
  {
    name: "Ernest Yao Gaewu",
    role: "Justice of the Supreme Court of Ghana",
    court: "Supreme Court of Ghana",
    aliases: ["Justice Ernest Gaewu", "Gaewu"],
    status: "active",
    appointedAt: "2023-04-05",
    ref: "https://judicial.gov.gh"
  },
  {
    name: "Henry Anthony Kwofie",
    role: "Justice of the Supreme Court of Ghana",
    court: "Supreme Court of Ghana",
    aliases: ["Justice Henry Kwofie", "Kwofie"],
    status: "active",
    appointedAt: "2024-01-03",
    ref: "https://judicial.gov.gh"
  },
  {
    name: "Yaw Darko Asare",
    role: "Justice of the Supreme Court of Ghana",
    court: "Supreme Court of Ghana",
    aliases: ["Justice Yaw Darko Asare", "Darko Asare"],
    status: "active",
    appointedAt: "2024-01-03",
    ref: "https://judicial.gov.gh"
  },
  {
    name: "Richard Adjei-Frimpong",
    role: "Justice of the Supreme Court of Ghana",
    court: "Supreme Court of Ghana",
    aliases: ["Justice Richard Adjei-Frimpong", "Adjei-Frimpong"],
    status: "active",
    appointedAt: "2024-01-03",
    ref: "https://judicial.gov.gh"
  },

  // =========================================================================
  // JUDICIAL ADMINISTRATION & COURT OF APPEAL SENIOR LEADERSHIP
  // =========================================================================
  {
    name: "Cyra Pamela C.A. Koranteng",
    role: "Judicial Secretary of the Republic of Ghana, Head of Judicial Administration",
    court: "Judicial Service Administration / Court of Appeal",
    aliases: ["Justice Pamela Koranteng", "Pamela Koranteng", "Judicial Secretary Koranteng"],
    status: "active",
    appointedAt: "2022-10-01",
    ref: "https://judicial.gov.gh"
  },
  {
    name: "Afia Serwah Asare-Botwe",
    role: "Justice of the Court of Appeal, High Court Financial & Economic Crimes Division",
    court: "Court of Appeal of Ghana",
    aliases: ["Justice Afia Serwah Asare-Botwe", "Asare-Botwe"],
    status: "active",
    appointedAt: "2023-01-01",
    ref: "https://judicial.gov.gh"
  },
  {
    name: "Eric Kyei Baffour",
    role: "Justice of the Court of Appeal",
    court: "Court of Appeal of Ghana",
    aliases: ["Justice Eric Kyei Baffour", "Kyei Baffour"],
    status: "active",
    appointedAt: "2023-01-01",
    ref: "https://judicial.gov.gh"
  },

  // =========================================================================
  // FORMER CHIEF JUSTICES UNDER STATUTORY AML COOLING-OFF
  // =========================================================================
  {
    name: "Kwasi Anin-Yeboah",
    role: "Former Chief Justice of the Republic of Ghana (2020–2023)",
    court: "Supreme Court of Ghana (Retired)",
    aliases: ["Chief Justice Anin-Yeboah", "Justice Anin-Yeboah", "Anin-Yeboah"],
    status: "cooling_off",
    appointedAt: "2020-01-07",
    ref: "https://judicial.gov.gh"
  },
  {
    name: "Sophia Akuffo",
    role: "Former Chief Justice of the Republic of Ghana (2017–2019)",
    court: "Supreme Court of Ghana (Retired)",
    aliases: ["Chief Justice Sophia Akuffo", "Sophia A.B. Akuffo"],
    status: "cooling_off",
    appointedAt: "2017-06-19",
    ref: "https://judicial.gov.gh"
  },
  {
    name: "Georgina Theodora Wood",
    role: "Former Chief Justice of the Republic of Ghana (2007–2017)",
    court: "Supreme Court of Ghana (Retired)",
    aliases: ["Chief Justice Georgina Wood", "Georgina Wood"],
    status: "cooling_off",
    appointedAt: "2007-06-15",
    ref: "https://judicial.gov.gh"
  }
];

function formatJudiciaryRecord(idx, item) {
  const statusLabel = item.status === "active" ? "Active Office Holder" : "Former Official (AML Cooling-Off)";
  const sanctionNote = item.status === "active"
    ? "Enhanced Due Diligence (EDD) Required; AML Act 1044 Active PEP Screening (Senior Judicial Figure)"
    : "Statutory 1-Year AML Cooling-Off Period (Act 1044); Monitored Judicial Status";

  const familyNote = item.family ? `; Known Family: ${item.family.join(', ')}` : '';
  const cleanName = item.name.replace("Prof.", "").replace("Justice", "").replace("Dr.", "").trim();

  return {
    id: `gh-pep-jud-${String(idx).padStart(4, '0')}`,
    name: cleanName,
    type: "Person",
    aliases: item.aliases || [cleanName.split(' ').pop()],
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
        `Judicial Branch: ${item.court}`
      ],
      listedAt: [item.appointedAt || "2023-01-01"],
      sourceStatus: [
        `Ghana PEP; Role: ${item.role}; Status: ${item.status}; Court: ${item.court}${familyNote}`
      ],
      sanctionsImposed: [
        sanctionNote
      ],
      address: ["Accra, Ghana"]
    }
  };
}

function run() {
  console.log("========================================================================");
  console.log("       GHANA JUDICIAL SERVICE (ACT 1044) SENIOR JUDICIAL REGISTRY       ");
  console.log("========================================================================");
  console.log("Legal Mandate: AML Act, 2020 (Act 1044) & 1992 Constitution Chapter 11");
  console.log("Scope: Supreme Court Justices, Judicial Administration & Appellate Court\n");

  const records = JUDICIARY_DATA.map((j, idx) => formatJudiciaryRecord(idx + 1, j));

  const payload = {
    version: 2,
    source: "gh-pep-judiciary",
    parser: "gh-pep-v1",
    description: "Judicial Service of Ghana Senior Judicial Figures (AML Act 1044) Registry",
    last_updated_at: new Date().toISOString(),
    total_records: records.length,
    records: records
  };

  const outputPath = "gh_pep_judiciary.json";
  fs.writeFileSync(outputPath, JSON.stringify(payload, null, 2), "utf8");
  console.log(`[✓] Generated ${records.length} Senior Judicial PEP records in ${outputPath}`);

  // Also sync to scripts/
  const scriptsPath = path.join("scripts", "gh_pep_judiciary.json");
  fs.writeFileSync(scriptsPath, JSON.stringify(payload, null, 2), "utf8");
  console.log(`[✓] Synced to ${scriptsPath}`);

  const activeCount = records.filter(r => r.properties.sourceStatus[0].includes("active")).length;
  const coolingCount = records.filter(r => r.properties.sourceStatus[0].includes("cooling_off")).length;

  console.log(`\nActive Sitting Judicial Figures:  ${activeCount}`);
  console.log(`AML Cooling-Off Retained Figures: ${coolingCount}`);
  console.log("\nSample Sitting Justices of the Supreme Court:");
  records.slice(0, 8).forEach((r, idx) => {
    console.log(`  ${idx + 1}. ${r.name.padEnd(35)} | ${r.properties.programs[2]}`);
  });
  console.log("========================================================================\n");
}

run();
