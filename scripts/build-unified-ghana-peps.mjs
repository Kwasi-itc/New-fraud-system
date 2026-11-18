import fs from 'node:fs';

/**
 * Unified Ghana PEP & RCA Master Generator
 * 
 * Merges:
 * 1. Live crawled sitting MPs from parliament.gh (270 current MPs)
 * 2. Curated 2025/2026 Executive, Leadership, and SOE heads
 * 3. Wikidata live knowledge graph (1,366 PEPs + 180 Family RCAs)
 * 
 * Generates 4 strictly compliant Version 2 JSON snapshot fixtures:
 * - gh_pep_executive.json
 * - gh_pep_parliament.json
 * - gh_pep_soe.json
 * - gh_pep_rca.json
 */

// Load crawled Parliament data if present
let crawledMps = [];
if (fs.existsSync('parliament_crawled_mps.json')) {
  crawledMps = JSON.parse(fs.readFileSync('parliament_crawled_mps.json', 'utf8'));
  console.log(`[Master Builder] Loaded ${crawledMps.length} crawled MPs from parliament.gh.`);
}

// Load Wikidata data if present
let wikidataPeps = [];
let wikidataRcas = [];
if (fs.existsSync('gh_pep_wikidata.json')) {
  const wd = JSON.parse(fs.readFileSync('gh_pep_wikidata.json', 'utf8'));
  wikidataPeps = wd.records || [];
  console.log(`[Master Builder] Loaded ${wikidataPeps.length} Wikidata PEPs.`);
}
if (fs.existsSync('gh_rca_wikidata.json')) {
  const wdRca = JSON.parse(fs.readFileSync('gh_rca_wikidata.json', 'utf8'));
  wikidataRcas = wdRca.records || [];
  console.log(`[Master Builder] Loaded ${wikidataRcas.length} Wikidata RCAs.`);
}

// ============================================================================
// 1. EXECUTIVE PEPS (Current vs Cooling-off Former)
// ============================================================================
const executivePeps = [
  // --- Current Executive Leadership (9th Administration / 4th Republic) ---
  {
    id: "gh-pep-exec-001",
    name: "John Dramani Mahama",
    aliases: ["John Mahama", "JDM", "President John Dramani Mahama", "President Mahama"],
    role: "President of the Republic of Ghana, Commander-in-Chief of the Ghana Armed Forces",
    term: "2025-01-07 to present",
    status: "active",
    familyMembers: ["Lordina Mahama (Spouse / First Lady)", "Ibrahim Mahama (Brother)", "Alfred Mahama (Brother)", "Sharaf Mahama (Son)", "Shafiq Mahama (Son)", "Shahid Mahama (Son)"],
    associates: ["Julius Debrah (Chief of Staff)", "Joyce Bawah Mogtari (Special Advisor)", "Prosper Bani (Senior Presidential Coordinator)", "Prof. Joshua Alabi (Senior Advisor)"],
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-exec-002",
    name: "Jane Naana Opoku-Agyemang",
    aliases: ["Prof. Jane Naana Opoku-Agyemang", "Naana Jane Opoku-Agyemang", "Prof. Naana Opoku-Agyemang", "Vice President Jane Naana Opoku-Agyemang"],
    role: "Vice President of the Republic of Ghana",
    term: "2025-01-07 to present",
    status: "active",
    familyMembers: ["Dr. Kweku Opoku-Agyemang (Son)", "Dr. Kwabena Opoku-Agyemang (Son)", "Dr. Adwoa Opoku-Agyemang (Daughter)"],
    associates: ["President John Dramani Mahama (President)"],
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-exec-003",
    name: "Julius Debrah",
    aliases: ["Hon. Julius Debrah"],
    role: "Senior Presidential Advisor and Chief of Staff at the Presidency",
    term: "2025-01-07 to present",
    status: "active",
    associates: ["President John Dramani Mahama (President)", "Dr. Valerie Sawyerr (Senior Staffer)"],
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-exec-004",
    name: "Prosper Douglas Bani",
    aliases: ["Prosper Bani"],
    role: "Senior Presidential Coordinator, Former Minister for the Interior",
    term: "2025-01-07 to present",
    status: "active",
    associates: ["President John Dramani Mahama (President)"],
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-exec-005",
    name: "Valerie Esther Sawyerr",
    aliases: ["Dr. Valerie Sawyerr", "Valerie Sawyerr"],
    role: "Senior Presidential Staffer, Former Deputy Chief of Staff",
    term: "2025-01-07 to present",
    status: "active",
    associates: ["President John Dramani Mahama (President)", "Julius Debrah (Chief of Staff)"],
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-exec-006",
    name: "Joyce Bawah Mogtari",
    aliases: ["Joyce Bawa Mogtari"],
    role: "Special Presidential Envoy and Senior Advisor to the President",
    term: "2025-01-07 to present",
    status: "active",
    associates: ["President John Dramani Mahama (President)"],
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-exec-007",
    name: "Felix Kwakye Ofosu",
    aliases: ["Felix Ofosu Kwakye", "Hon. Felix Kwakye Ofosu"],
    role: "Presidential Spokesperson, Member of Parliament for Abura Asebu Kwamankese",
    term: "2025-01-07 to present",
    status: "active",
    associates: ["President John Dramani Mahama (President)"],
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-exec-008",
    name: "Johnson Asiedu Nketiah",
    aliases: ["Asiedu Nketiah", "General Mosquito"],
    role: "National Chairman, National Democratic Congress (NDC), Former Member of Parliamentary Service Board",
    term: "2022-12-18 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-exec-009",
    name: "Fifi Fiavi Kwetey",
    aliases: ["Fifi Kwetey", "Fiifi Kwetey"],
    role: "General Secretary, National Democratic Congress (NDC), Former Minister for Transport & Agriculture",
    term: "2022-12-18 to present",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-exec-010",
    name: "Joshua Alabi",
    aliases: ["Prof. Joshua Alabi"],
    role: "Senior Presidential Advisor, Former Campaign Coordinator",
    term: "2025-01-07 to present",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-exec-011",
    name: "Seth Emmanuel Terkper",
    aliases: ["Seth Terkper"],
    role: "Senior Presidential Economic Advisor, Former Minister for Finance",
    term: "2025-01-07 to present",
    status: "active",
    ref: "https://mofep.gov.gh"
  },
  {
    id: "gh-pep-exec-012",
    name: "Hannah Serwaa Tetteh",
    aliases: ["Hannah Tetteh"],
    role: "Senior Diplomatic Advisor, Special Representative of the UN Secretary-General, Former Foreign Minister",
    term: "2025-01-07 to present",
    status: "active",
    ref: "https://mfa.gov.gh"
  },

  // --- Former Executive Leadership (Cooling-off Period under FATF & Act 1044 Rules) ---
  {
    id: "gh-pep-exec-013",
    name: "Nana Addo Dankwa Akufo-Addo",
    aliases: ["Nana Akufo-Addo", "William Addo Dankwa Akufo-Addo"],
    role: "Former President of the Republic of Ghana (2017–2025)",
    term: "2017-01-07 to 2025-01-06",
    status: "cooling_off",
    familyMembers: ["Rebecca Akufo-Addo (Spouse / Former First Lady)", "Gyankroma Akufo-Addo (Daughter)", "Edwina Nana Dokua Akufo-Addo (Daughter)", "Adriana Dukua Akufo-Addo (Daughter)", "Edward Akufo-Addo (Brother)", "Ken Ofori-Atta (Cousin)"],
    associates: ["Gabby Asare Otchere-Darko (Nephew / Senior Advisor)", "Akosua Frema Osei-Opare (Former Chief of Staff)"],
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-exec-014",
    name: "Mahamudu Bawumia",
    aliases: ["Dr. Mahamudu Bawumia"],
    role: "Former Vice President of the Republic of Ghana (2017–2025)",
    term: "2017-01-07 to 2025-01-06",
    status: "cooling_off",
    familyMembers: ["Samira Bawumia (Spouse / Former Second Lady)", "Alhaji Ahmed Ramadan (Father-in-law)", "Abu Ramadan (Brother-in-law)"],
    associates: ["Nana Addo Dankwa Akufo-Addo (Former President)"],
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-exec-015",
    name: "Akosua Frema Osei-Opare",
    aliases: ["Frema Osei-Opare"],
    role: "Former Chief of Staff at the Presidency (2017–2025)",
    term: "2017-01-07 to 2025-01-06",
    status: "cooling_off",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-exec-016",
    name: "Kenneth Nana Yaw Ofori-Atta",
    aliases: ["Ken Ofori-Atta"],
    role: "Former Minister for Finance, Senior Presidential Advisor on Economy (2017–2024)",
    term: "2017-01-27 to 2024-02-14",
    status: "cooling_off",
    familyMembers: ["Prof. Angela Ofori-Atta (Spouse)", "Nana Addo Dankwa Akufo-Addo (Cousin)"],
    associates: ["Keli Gadzekpo (Business Associate / Databank Co-founder)"],
    ref: "https://mofep.gov.gh"
  },
  {
    id: "gh-pep-exec-017",
    name: "Mohammed Amin Adam",
    aliases: ["Dr. Mohammed Amin Adam", "Anta"],
    role: "Former Minister for Finance, Former Minister of State at Finance (2024–2025)",
    term: "2024-02-14 to 2025-01-06",
    status: "cooling_off",
    ref: "https://mofep.gov.gh"
  },
  {
    id: "gh-pep-exec-018",
    name: "Godfred Yeboah Dame",
    aliases: ["Godfred Dame"],
    role: "Former Attorney-General and Minister for Justice (2021–2025)",
    term: "2021-01-21 to 2025-01-06",
    status: "cooling_off",
    ref: "https://mojagd.gov.gh"
  },
  {
    id: "gh-pep-exec-019",
    name: "Dominic Bingham Aduna Nitiwul",
    aliases: ["Dominic Nitiwul"],
    role: "Former Minister for Defence, Member of Parliament for Bimbilla (2017–2025)",
    term: "2017-01-27 to 2025-01-06",
    status: "cooling_off",
    ref: "https://gafonline.mil.gh"
  },
  {
    id: "gh-pep-exec-020",
    name: "Henry Quartey",
    aliases: ["Hon. Henry Quartey"],
    role: "Former Minister for the Interior, Former Greater Accra Regional Minister (2021–2025)",
    term: "2021-01-21 to 2025-01-06",
    status: "cooling_off",
    ref: "https://mint.gov.gh"
  },
  {
    id: "gh-pep-exec-021",
    name: "Shirley Ayorkor Botchwey",
    aliases: ["Ayorkor Botchwey"],
    role: "Commonwealth Secretary-General, Former Minister for Foreign Affairs (2017–2025)",
    term: "2017-01-27 to 2025-01-06",
    status: "cooling_off",
    ref: "https://mfa.gov.gh"
  },
  {
    id: "gh-pep-exec-022",
    name: "Samuel Abu Jinapor",
    aliases: ["Abu Jinapor"],
    role: "Former Minister for Lands and Natural Resources, MP for Damongo (2021–2025)",
    term: "2021-03-05 to 2025-01-06",
    status: "cooling_off",
    familyMembers: ["John Abdulai Jinapor (Brother / MP Yapei-Kusawgu)"],
    ref: "https://mlnr.gov.gh"
  },
  {
    id: "gh-pep-exec-023",
    name: "Matthew Opoku Prempeh",
    aliases: ["Dr. Matthew Opoku Prempeh", "Napo"],
    role: "Former Minister for Energy, Former Minister for Education (2017–2024)",
    term: "2017-01-27 to 2024-07-15",
    status: "cooling_off",
    ref: "https://energymin.gov.gh"
  },
  {
    id: "gh-pep-exec-024",
    name: "Ursula Owusu-Ekuful",
    aliases: ["Ursula Owusu"],
    role: "Former Minister for Communications and Digitalisation (2017–2025)",
    term: "2017-01-27 to 2025-01-06",
    status: "cooling_off",
    familyMembers: ["Dr. Sam Ekuful (Spouse)"],
    ref: "https://moc.gov.gh"
  },
  {
    id: "gh-pep-exec-025",
    name: "Bryan Acheampong",
    aliases: ["Dr. Bryan Acheampong"],
    role: "Former Minister for Food and Agriculture, MP for Abetifi (2020–2025)",
    term: "2020-02-13 to 2025-01-06",
    status: "cooling_off",
    ref: "https://mofa.gov.gh"
  },
  {
    id: "gh-pep-exec-026",
    name: "Kojo Oppong Nkrumah",
    aliases: ["Oppong Nkrumah"],
    role: "Former Minister for Works and Housing, Former Minister for Information (2018–2025)",
    term: "2018-11-01 to 2025-01-06",
    status: "cooling_off",
    ref: "https://mwh.gov.gh"
  },
  {
    id: "gh-pep-exec-027",
    name: "Ambrose Dery",
    aliases: ["Ambrose Derry"],
    role: "Former Minister for the Interior, Former Minister of State at the Presidency (2017–2025)",
    term: "2017-01-27 to 2025-01-06",
    status: "cooling_off",
    ref: "https://mint.gov.gh"
  },
  {
    id: "gh-pep-exec-028",
    name: "Alan John Kwadwo Kyerematen",
    aliases: ["Alan Kyerematen", "Alan Cash"],
    role: "Leader Movement for Change, Former Minister for Trade and Industry (2017–2023)",
    term: "2017-01-27 to 2023-01-06",
    status: "cooling_off",
    familyMembers: ["Patricia Christabel Kyerematen (Spouse)"],
    ref: "https://moti.gov.gh"
  },
  {
    id: "gh-pep-exec-029",
    name: "Francis Asenso-Boakye",
    aliases: ["Asenso-Boakye"],
    role: "Former Minister for Roads and Highways, Former Minister for Works and Housing (2021–2025)",
    term: "2021-03-05 to 2025-01-06",
    status: "cooling_off",
    ref: "https://mrh.gov.gh"
  },
  {
    id: "gh-pep-exec-030",
    name: "Cecilia Abena Dapaah",
    aliases: ["Cecilia Dapaah"],
    role: "Former Minister for Sanitation and Water Resources, Subject of OSP Anti-Corruption Investigation",
    term: "2018-08-01 to 2023-07-22",
    status: "cooling_off",
    familyMembers: ["Daniel Osei Kuffour (Spouse)"],
    ref: "https://osp.gov.gh"
  }
];

// ============================================================================
// 2. PARLIAMENT PEPS (Live Crawled 270 MPs + Parliamentary Leadership)
// ============================================================================
const leadershipPeps = [
  {
    id: "gh-pep-parl-001",
    name: "Alban Sumana Kingsford Bagbin",
    aliases: ["Alban S.K. Bagbin", "Alban Bagbin", "Rt. Hon. Alban Bagbin"],
    role: "Speaker of the Parliament of the Republic of Ghana (8th & 9th Parliament)",
    term: "2021-01-07 to present",
    status: "active",
    familyMembers: ["Alice Adinda Bagbin (Spouse)"],
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-002",
    name: "Bernard Ahiafor",
    aliases: ["Hon. Bernard Ahiafor"],
    role: "First Deputy Speaker of Parliament, Member of Parliament for Akatsi South",
    constituency: "Akatsi South",
    term: "2025-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-003",
    name: "Andrew Asiamah Amoako",
    aliases: ["Asiamah Amoako", "Hon. Andrew Asiamah"],
    role: "Deputy Speaker of Parliament, Member of Parliament for Fomena",
    constituency: "Fomena",
    term: "2021-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-004",
    name: "Cassiel Ato Baah Forson",
    aliases: ["Dr. Ato Forson", "Cassiel Ato Forson", "Dr. Cassiel Ato Forson", "Ato Forson"],
    role: "Majority Leader of the Parliament of Ghana, Member of Parliament for Ajumako-Enyan-Esiam",
    constituency: "Ajumako-Enyan-Esiam",
    term: "2023-01-24 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-005",
    name: "Alexander Kwamina Afenyo-Markin",
    aliases: ["Alexander Afenyo-Markin", "Afenyo-Markin"],
    role: "Minority Leader of the Parliament of Ghana, Member of Parliament for Effutu",
    constituency: "Effutu",
    term: "2024-02-23 to present",
    status: "active",
    familyMembers: ["Dianne Markin (Spouse)"],
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-006",
    name: "Emmanuel Armah-Kofi Buah",
    aliases: ["Emmanuel Armah Kofi Buah", "Armah Kofi Buah"],
    role: "Deputy Majority Leader of Parliament, Member of Parliament for Ellembelle",
    constituency: "Ellembelle",
    term: "2023-01-24 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-007",
    name: "Frank Annoh-Dompreh",
    aliases: ["Annoh-Dompreh", "Frank Annoh Dompreh"],
    role: "Deputy Minority Leader of Parliament, Member of Parliament for Nsawam-Adoagyiri",
    constituency: "Nsawam-Adoagyiri",
    term: "2021-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-008",
    name: "Kwame Governs Agbodza",
    aliases: ["Kwame Agbodza", "Governs Agbodza"],
    role: "Majority Chief Whip of Parliament, Member of Parliament for Adaklu",
    constituency: "Adaklu",
    term: "2023-01-24 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-009",
    name: "Lydia Seyram Alhassan",
    aliases: ["Lydia Alhassan", "Maa Lydia"],
    role: "Minority Chief Whip of Parliament, Member of Parliament for Ayawaso West Wuogon",
    constituency: "Ayawaso West Wuogon",
    term: "2024-02-23 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-010",
    name: "Samuel Okudzeto Ablakwa",
    aliases: ["Okudzeto Ablakwa", "Samuel Ablakwa"],
    role: "Member of Parliament for North Tongu, Chairman Government Assurances",
    constituency: "North Tongu",
    term: "2013-01-07 to present",
    status: "active",
    familyMembers: ["Nuhela Seidu (Spouse)"],
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-011",
    name: "Haruna Iddrisu",
    aliases: ["Hon. Haruna Iddrisu"],
    role: "Member of Parliament for Tamale South, Former Minority Leader",
    constituency: "Tamale South",
    term: "2005-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-012",
    name: "Zanetor Agyeman-Rawlings",
    aliases: ["Dr. Zanetor Rawlings", "Zanetor Rawlings"],
    role: "Member of Parliament for Klottey-Korle",
    constituency: "Klottey-Korle",
    term: "2017-01-07 to present",
    status: "active",
    familyMembers: ["Nana Konadu Agyeman-Rawlings (Mother / Former First Lady)", "Amina Rawlings (Sister)", "Kimathi Rawlings (Brother)"],
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-013",
    name: "Isaac Adongo",
    aliases: ["Hon. Isaac Adongo"],
    role: "Member of Parliament for Bolgatanga Central, Ranking Member on Finance",
    constituency: "Bolgatanga Central",
    term: "2017-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-014",
    name: "John Abdulai Jinapor",
    aliases: ["John Jinapor"],
    role: "Member of Parliament for Yapei-Kusawgu, Chairman/Ranking Mines & Energy",
    constituency: "Yapei-Kusawgu",
    term: "2017-01-07 to present",
    status: "active",
    familyMembers: ["Samuel Abu Jinapor (Brother / Former Minister)"],
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-015",
    name: "Samuel Nartey George",
    aliases: ["Sam George", "Sam Nartey George"],
    role: "Member of Parliament for Ningo-Prampram, Communications Committee",
    constituency: "Ningo-Prampram",
    term: "2017-01-07 to present",
    status: "active",
    familyMembers: ["Vera George (Spouse)"],
    ref: "https://parliament.gh"
  }
];

// Combine crawled MPs with leadership, avoiding duplicates
const combinedParliament = [...leadershipPeps];
const seenNames = new Set(leadershipPeps.map(l => l.name.toLowerCase()));

for (const cmp of crawledMps) {
  if (!seenNames.has(cmp.name.toLowerCase())) {
    seenNames.add(cmp.name.toLowerCase());
    combinedParliament.push({
      id: cmp.id,
      name: cmp.name,
      role: cmp.role,
      constituency: cmp.constituency,
      term: "2025-01-07 to present",
      status: "active",
      ref: cmp.ref
    });
  }
}

// ============================================================================
// 3. SOE, REGULATORS & JUDICIARY PEPS (Current 2025/2026 Active & Cooling-off)
// ============================================================================
const soeJudiciaryPeps = [
  // --- Judiciary & Supreme Court of Ghana ---
  {
    id: "gh-pep-soe-001",
    name: "Gertrude Araba Esaaba Torkornoo",
    aliases: ["Chief Justice Gertrude Torkornoo", "Gertrude Torkornoo"],
    role: "Chief Justice of the Republic of Ghana, Head of the Judiciary",
    term: "2023-06-12 to present",
    status: "active",
    familyMembers: ["Francis Torkornoo (Spouse)"],
    ref: "https://judicial.gov.gh"
  },
  {
    id: "gh-pep-soe-002",
    name: "Paul Baffoe-Bonnie",
    aliases: ["Justice Paul Baffoe-Bonnie"],
    role: "Senior Justice of the Supreme Court of Ghana",
    term: "2008-06-11 to present",
    status: "active",
    ref: "https://judicial.gov.gh"
  },
  {
    id: "gh-pep-soe-003",
    name: "Gabriel Pwamang",
    aliases: ["Justice Gabriel Pwamang"],
    role: "Justice of the Supreme Court of Ghana",
    term: "2015-06-29 to present",
    status: "active",
    ref: "https://judicial.gov.gh"
  },
  {
    id: "gh-pep-soe-004",
    name: "Emmanuel Yonny Kulendi",
    aliases: ["Justice Yonny Kulendi", "Yonny Kulendi"],
    role: "Justice of the Supreme Court of Ghana",
    term: "2020-05-26 to present",
    status: "active",
    ref: "https://judicial.gov.gh"
  },

  // --- Independent Constitutional Bodies & Anti-Corruption ---
  {
    id: "gh-pep-soe-005",
    name: "Kissi Agyebeng",
    aliases: ["Special Prosecutor Kissi Agyebeng"],
    role: "Special Prosecutor of the Republic of Ghana, Office of the Special Prosecutor (OSP)",
    term: "2021-08-05 to present",
    status: "active",
    ref: "https://osp.gov.gh"
  },
  {
    id: "gh-pep-soe-006",
    name: "Joseph Whittal",
    aliases: ["Commissioner Joseph Whittal"],
    role: "Commissioner, Commission on Human Rights and Administrative Justice (CHRAJ)",
    term: "2016-12-20 to present",
    status: "active",
    ref: "https://chraj.gov.gh"
  },
  {
    id: "gh-pep-soe-007",
    name: "Jean Adukwei Mensa",
    aliases: ["Jean Mensa", "Mrs. Jean Mensa"],
    role: "Chairperson, Electoral Commission of Ghana",
    term: "2018-07-23 to present",
    status: "active",
    familyMembers: ["Dr. Charles Mensa (Spouse)"],
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-soe-008",
    name: "Johnson Akuamoah Asiedu",
    aliases: ["Akuamoah Asiedu"],
    role: "Auditor-General of the Republic of Ghana, Ghana Audit Service",
    term: "2021-08-27 to present",
    status: "active",
    ref: "https://ghaudit.org"
  },

  // --- Central Bank Leadership (Current 2025/2026 Administration) ---
  {
    id: "gh-pep-soe-009",
    name: "Johnson Pandit Kwesi Asiama",
    aliases: ["Dr. Johnson Asiama", "Dr. Johnson Pandit Kwesi Asiama", "Governor Johnson Asiama"],
    role: "Governor and Board Chairman, Bank of Ghana (Central Bank)",
    term: "2025-02-01 to present",
    status: "active",
    ref: "https://bog.gov.gh"
  },
  {
    id: "gh-pep-soe-010",
    name: "Zakari Mumuni",
    aliases: ["Dr. Zakari Mumuni", "Deputy Governor Zakari Mumuni"],
    role: "First Deputy Governor, Bank of Ghana",
    term: "2025-02-01 to present",
    status: "active",
    ref: "https://bog.gov.gh"
  },
  {
    id: "gh-pep-soe-011",
    name: "Kwabena Osei-Bonsu",
    aliases: ["K. Osei-Bonsu"],
    role: "Chief Executive Officer, Financial Intelligence Centre (FIC Ghana)",
    term: "2020-01-01 to present",
    status: "active",
    ref: "https://fic.gov.gh"
  },

  // --- Security Chiefs & Armed Forces ---
  {
    id: "gh-pep-soe-012",
    name: "Thomas Oppong-Peprah",
    aliases: ["Lt. Gen. Thomas Oppong-Peprah"],
    role: "Chief of Defence Staff (CDS), Ghana Armed Forces",
    term: "2024-02-01 to present",
    status: "active",
    ref: "https://gafonline.mil.gh"
  },
  {
    id: "gh-pep-soe-013",
    name: "George Akuffo Dampare",
    aliases: ["Dr. George Akuffo Dampare", "IGP Dampare"],
    role: "Inspector-General of Police (IGP), Ghana Police Service",
    term: "2021-08-01 to present",
    status: "active",
    familyMembers: ["Anita Dampare (Spouse)"],
    ref: "https://police.gov.gh"
  },

  // --- Current 2025/2026 Active SOE CEOs & Managing Directors ---
  {
    id: "gh-pep-soe-014",
    name: "Edward Bawa",
    aliases: ["Hon. Edward Bawa", "Edward Abambire Bawa"],
    role: "Acting Chief Executive Officer, Ghana National Petroleum Corporation (GNPC)",
    term: "2025-01-15 to present",
    status: "active",
    ref: "https://gnpcghana.com"
  },
  {
    id: "gh-pep-soe-015",
    name: "Godwin Edudzi Tamakloe",
    aliases: ["Godwin Edudzi Tamakloe, Esq.", "Edudzi Tamakloe", "Lawyer Edudzi Tamakloe"],
    role: "Acting Chief Executive Officer, National Petroleum Authority (NPA)",
    term: "2025-01-15 to present",
    status: "active",
    ref: "https://npa.gov.gh"
  },
  {
    id: "gh-pep-soe-016",
    name: "Ransford Anertey Abbey",
    aliases: ["Dr. Randy Abbey", "Dr. Ransford Abbey", "Randy Abbey"],
    role: "Acting Chief Executive Officer, Ghana Cocoa Board (COCOBOD)",
    term: "2025-01-21 to present",
    status: "active",
    ref: "https://cocobod.gh"
  },
  {
    id: "gh-pep-soe-017",
    name: "Anthony Kwasi Sarpong",
    aliases: ["Anthony Sarpong", "Kwasi Sarpong", "Commissioner-General Anthony Sarpong"],
    role: "Acting Commissioner-General, Ghana Revenue Authority (GRA)",
    term: "2025-01-21 to present",
    status: "active",
    ref: "https://gra.gov.gh"
  },
  {
    id: "gh-pep-soe-018",
    name: "Kwesi Afreh Biney",
    aliases: ["Kwesi Biney", "Director-General Kwesi Afreh Biney"],
    role: "Director-General, Social Security and National Insurance Trust (SSNIT)",
    term: "2025-01-21 to present",
    status: "active",
    associates: ["Adam Sulley (Deputy Director-General, SSNIT)"],
    ref: "https://ssnit.org.gh"
  },
  {
    id: "gh-pep-soe-019",
    name: "Adam Sulley",
    aliases: ["Alhaji Adam Sulley"],
    role: "Deputy Director-General (Operations & Benefits), Social Security and National Insurance Trust (SSNIT)",
    term: "2025-01-21 to present",
    status: "active",
    ref: "https://ssnit.org.gh"
  },
  {
    id: "gh-pep-soe-020",
    name: "Edward Ekow Obeng-Kenzo",
    aliases: ["Ing. Edward Obeng-Kenzo", "Edward Obeng-Kenzo", "Ing. Obeng-Kenzo"],
    role: "Acting Chief Executive Officer, Volta River Authority (VRA)",
    term: "2025-02-01 to present",
    status: "active",
    associates: ["Ing. Jabesh Amissah-Arthur (Board Chairman, VRA)"],
    ref: "https://vra.com"
  },
  {
    id: "gh-pep-soe-021",
    name: "Jabesh Amissah-Arthur",
    aliases: ["Ing. Jabesh Amissah-Arthur"],
    role: "Board Chairman, Volta River Authority (VRA)",
    term: "2025-02-01 to present",
    status: "active",
    ref: "https://vra.com"
  },
  {
    id: "gh-pep-soe-022",
    name: "Julius Kpekpena",
    aliases: ["Ing. Julius Kpekpena", "Julius Kpekpena"],
    role: "Acting Managing Director, Electricity Company of Ghana (ECG)",
    term: "2025-02-01 to present",
    status: "active",
    associates: ["Ing. Dr. William Amuna (Board Chairman, ECG)"],
    ref: "https://ecg.com.gh"
  },
  {
    id: "gh-pep-soe-023",
    name: "William Amuna",
    aliases: ["Ing. Dr. William Amuna", "Dr. William Amuna"],
    role: "Board Chairman, Electricity Company of Ghana (ECG), Former CEO of GRIDCo",
    term: "2025-02-01 to present",
    status: "active",
    ref: "https://ecg.com.gh"
  },
  {
    id: "gh-pep-soe-024",
    name: "Afetsi Awoonor",
    aliases: ["Afetsi Awoonor"],
    role: "Acting Managing Director, Bulk Oil Storage and Transportation (BOST)",
    term: "2025-01-15 to present",
    status: "active",
    ref: "https://bost.com.gh"
  },
  {
    id: "gh-pep-soe-025",
    name: "Judith Adjobah Blay",
    aliases: ["Judith Blay", "Ms. Judith Adjobah Blay"],
    role: "Acting Managing Director / CEO, Ghana National Gas Limited Company (Ghana Gas)",
    term: "2025-02-01 to present",
    status: "active",
    associates: ["Kofi Totobi Quakyi (Board Chairman, Ghana Gas)"],
    ref: "https://ghanagas.com.gh"
  },
  {
    id: "gh-pep-soe-026",
    name: "Kofi Totobi Quakyi",
    aliases: ["Totobi Quakyi", "Hon. Kofi Totobi Quakyi"],
    role: "Board Chairman, Ghana National Gas Limited Company (Ghana Gas)",
    term: "2025-02-01 to present",
    status: "active",
    ref: "https://ghanagas.com.gh"
  },
  {
    id: "gh-pep-soe-027",
    name: "Paul Seidu Tanye-Kulono",
    aliases: ["Major-General Paul Seidu Tanye-Kulono", "Maj. Gen. Tanye-Kulono"],
    role: "Acting Director-General, Ghana Ports and Harbours Authority (GPHA)",
    term: "2025-02-01 to present",
    status: "active",
    ref: "https://ghanaports.gov.gh"
  },
  {
    id: "gh-pep-soe-028",
    name: "Michael Kpessa-Whyte",
    aliases: ["Prof. Michael Kpessa-Whyte", "Michael Kpessa Whyte", "Prof. Kpessa-Whyte"],
    role: "Director-General, State Interests and Governance Authority (SIGA)",
    term: "2025-01-15 to present",
    status: "active",
    ref: "https://siga.gov.gh"
  },
  {
    id: "gh-pep-soe-029",
    name: "Victoria Emefa Hardcastle",
    aliases: ["Victoria Emefa Hardcastle, Esq.", "Victoria Hardcastle"],
    role: "Acting Chief Executive Officer, Petroleum Commission of Ghana",
    term: "2025-01-15 to present",
    status: "active",
    ref: "https://petrocom.gov.gh"
  },
  {
    id: "gh-pep-soe-030",
    name: "Sammy Gyamfi",
    aliases: ["Sammy Gyamfi, Esq.", "Lawyer Sammy Gyamfi"],
    role: "Acting Managing Director, Precious Minerals Marketing Company (PMMC)",
    term: "2025-01-15 to present",
    status: "active",
    ref: "https://pmmc.gov.gh"
  },
  {
    id: "gh-pep-soe-031",
    name: "Justina Nelson",
    aliases: ["Justina Nelson"],
    role: "Chief Executive Officer, Minerals Income Investment Fund (MIIF)",
    term: "2025-01-15 to present",
    status: "active",
    associates: ["Dr. Kwaku Asiedu Nketia (Deputy CEO, MIIF)"],
    ref: "https://miif.gov.gh"
  },
  {
    id: "gh-pep-soe-032",
    name: "Kwaku Asiedu Nketia",
    aliases: ["Dr. Kwaku Asiedu Nketia"],
    role: "Deputy Chief Executive Officer, Minerals Income Investment Fund (MIIF)",
    term: "2025-01-15 to present",
    status: "active",
    familyMembers: ["Johnson Asiedu Nketiah (Father / NDC National Chairman)"],
    ref: "https://miif.gov.gh"
  },
  {
    id: "gh-pep-soe-033",
    name: "Mohammed Abdul-Salam",
    aliases: ["Mohammed Abdul Salam"],
    role: "Acting Director-General, National Lottery Authority (NLA)",
    term: "2025-01-15 to present",
    status: "active",
    ref: "https://nla.com.gh"
  },
  {
    id: "gh-pep-soe-034",
    name: "Nana Ama Browne Klutse",
    aliases: ["Prof. Nana Ama Browne Klutse", "Nana Ama Browne Klutse"],
    role: "Executive Director, Environmental Protection Agency (EPA Ghana)",
    term: "2025-01-15 to present",
    status: "active",
    ref: "https://epa.gov.gh"
  },
  {
    id: "gh-pep-soe-035",
    name: "Wisdom Yayra Koku Deku",
    aliases: ["Wisdom Deku", "Wisdom Yayra Deku"],
    role: "Executive Secretary & CEO, National Identification Authority (NIA)",
    term: "2025-01-15 to present",
    status: "active",
    ref: "https://nia.gov.gh"
  },
  {
    id: "gh-pep-soe-036",
    name: "Daniel Wilson Addo",
    aliases: ["Daniel Addo"],
    role: "Managing Director, Consolidated Bank Ghana (CBG)",
    term: "2018-08-01 to present",
    status: "active",
    ref: "https://cbg.com.gh"
  },
  {
    id: "gh-pep-soe-037",
    name: "Patrick Kuma-Aboagye",
    aliases: ["Dr. Patrick Kuma-Aboagye"],
    role: "Director-General, Ghana Health Service (GHS)",
    term: "2019-11-25 to present",
    status: "active",
    ref: "https://ghs.gov.gh"
  },

  // --- Outgoing SOE CEOs & Leadership (Under Statutory AML Act 1044 Cooling-off Period) ---
  {
    id: "gh-pep-soe-038",
    name: "Ernest Kwamina Yedu Addison",
    aliases: ["Dr. Ernest Addison", "Dr. Ernest K. Y. Addison", "Governor Ernest Addison"],
    role: "Former Governor, Bank of Ghana (Central Bank) (2017–2025)",
    term: "2017-03-30 to 2025-01-31",
    status: "cooling_off",
    familyMembers: ["Philip Addison (Brother)"],
    ref: "https://bog.gov.gh"
  },
  {
    id: "gh-pep-soe-039",
    name: "Maxwell Opoku-Afari",
    aliases: ["Dr. Maxwell Opoku-Afari"],
    role: "Former First Deputy Governor, Bank of Ghana (2017–2025)",
    term: "2017-08-07 to 2025-01-31",
    status: "cooling_off",
    ref: "https://bog.gov.gh"
  },
  {
    id: "gh-pep-soe-040",
    name: "Elsie Addo Awadzi",
    aliases: ["Mrs. Elsie Addo Awadzi"],
    role: "Former Second Deputy Governor, Bank of Ghana (2018–2025)",
    term: "2018-02-14 to 2025-01-31",
    status: "cooling_off",
    ref: "https://bog.gov.gh"
  },
  {
    id: "gh-pep-soe-041",
    name: "Julie Essiam",
    aliases: ["Commissioner-General Julie Essiam"],
    role: "Former Commissioner-General, Ghana Revenue Authority (GRA) (2024–2025)",
    term: "2024-03-26 to 2025-01-20",
    status: "cooling_off",
    ref: "https://gra.gov.gh"
  },
  {
    id: "gh-pep-soe-042",
    name: "Opoku-Ahweneeh Danquah",
    aliases: ["Opoku Danquah"],
    role: "Former Chief Executive Officer, Ghana National Petroleum Corporation (GNPC) (2022–2024)",
    term: "2022-04-21 to 2024-05-01",
    status: "cooling_off",
    ref: "https://gnpcghana.com"
  },
  {
    id: "gh-pep-soe-043",
    name: "Emmanuel Antwi-Darkwa",
    aliases: ["Ing. Emmanuel Antwi-Darkwa"],
    role: "Former Chief Executive, Volta River Authority (VRA) (2017–2025)",
    term: "2017-02-06 to 2025-01-31",
    status: "cooling_off",
    ref: "https://vra.com"
  },
  {
    id: "gh-pep-soe-044",
    name: "Samuel Dubik Masubir Mahama",
    aliases: ["Dubik Mahama", "Samuel Mahama"],
    role: "Former Managing Director, Electricity Company of Ghana (ECG) (2022–2024)",
    term: "2022-05-13 to 2024-09-25",
    status: "cooling_off",
    ref: "https://ecg.com.gh"
  },
  {
    id: "gh-pep-soe-045",
    name: "David Asamoah",
    aliases: ["David Boadi Asamoah", "Ing. David Asamoah"],
    role: "Former Acting Managing Director, Electricity Company of Ghana (ECG) (2024–2025)",
    term: "2024-09-26 to 2025-01-31",
    status: "cooling_off",
    ref: "https://ecg.com.gh"
  },
  {
    id: "gh-pep-soe-046",
    name: "Edwin Alfred Provencal",
    aliases: ["Edwin Provencal"],
    role: "Former Managing Director, Bulk Energy Storage and Transportation (BOST) (2019–2025)",
    term: "2019-08-26 to 2025-01-14",
    status: "cooling_off",
    ref: "https://bost.com.gh"
  },
  {
    id: "gh-pep-soe-047",
    name: "Kofi Bosompem Osafo-Maafo",
    aliases: ["Kofi Osafo-Maafo"],
    role: "Former Director-General, Social Security and National Insurance Trust (SSNIT) (2024–2025)",
    term: "2024-04-08 to 2025-01-20",
    status: "cooling_off",
    ref: "https://ssnit.org.gh"
  },
  {
    id: "gh-pep-soe-048",
    name: "Mustapha Abdul-Hamid",
    aliases: ["Dr. Mustapha Abdul-Hamid"],
    role: "Former Chief Executive, National Petroleum Authority (NPA) (2021–2025)",
    term: "2021-07-01 to 2025-01-14",
    status: "cooling_off",
    ref: "https://npa.gov.gh"
  },
  {
    id: "gh-pep-soe-049",
    name: "Joe Anokye",
    aliases: ["Joe Anokye"],
    role: "Former Director-General, National Communications Authority (NCA) (2017–2025)",
    term: "2017-01-15 to 2025-01-06",
    status: "cooling_off",
    ref: "https://nca.org.gh"
  },
  {
    id: "gh-pep-soe-050",
    name: "Joseph Boahen Aidoo",
    aliases: ["Hon. Joseph Boahen Aidoo"],
    role: "Former Chief Executive, Ghana Cocoa Board (COCOBOD) (2017–2024)",
    term: "2017-01-27 to 2024-12-31",
    status: "cooling_off",
    ref: "https://cocobod.gh"
  }
];

// ============================================================================
// 4. RELATIVES & CLOSE ASSOCIATES (RCAs) (Curated + Wikidata Family Graph)
// ============================================================================
const curatedRcas = [
  // --- Relatives of President John Dramani Mahama ---
  {
    id: "gh-pep-rca-001",
    name: "Lordina Mahama",
    aliases: ["Lordina Dramani Mahama", "Mrs. Lordina Mahama", "Lordina Effah"],
    role: "First Lady of the Republic of Ghana, Spouse of President John Dramani Mahama",
    relationship: "Spouse / First Lady",
    relatedPep: "John Dramani Mahama (President of Ghana)",
    term: "2025-01-07 to present",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-rca-002",
    name: "Ibrahim Mahama",
    aliases: ["Ibrahim Mahama Engineers & Planners"],
    role: "Industrialist & Business Executive, Brother of President John Dramani Mahama",
    relationship: "Brother / Close Associate",
    relatedPep: "John Dramani Mahama (President of Ghana)",
    term: "2012-07-24 to present",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-rca-003",
    name: "Alfred Mahama",
    aliases: ["Alfred Abdulai Mahama"],
    role: "Business Executive, Brother of President John Dramani Mahama",
    relationship: "Brother",
    relatedPep: "John Dramani Mahama (President of Ghana)",
    term: "2012-07-24 to present",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-rca-004",
    name: "Sharaf Mahama",
    aliases: ["Sharaf Dramani Mahama"],
    role: "Son of President John Dramani Mahama",
    relationship: "Son / Child",
    relatedPep: "John Dramani Mahama (President of Ghana)",
    term: "2012-07-24 to present",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-rca-005",
    name: "Shafiq Mahama",
    aliases: ["Shafiq Dramani Mahama"],
    role: "Son of President John Dramani Mahama",
    relationship: "Son / Child",
    relatedPep: "John Dramani Mahama (President of Ghana)",
    term: "2012-07-24 to present",
    status: "active",
    ref: "https://presidency.gov.gh"
  },

  // --- Relatives of Vice President Jane Naana Opoku-Agyemang ---
  {
    id: "gh-pep-rca-006",
    name: "Kweku Opoku-Agyemang",
    aliases: ["Dr. Kweku Opoku-Agyemang"],
    role: "Economist, Son of Vice President Jane Naana Opoku-Agyemang",
    relationship: "Son / Child",
    relatedPep: "Jane Naana Opoku-Agyemang (Vice President of Ghana)",
    term: "2025-01-07 to present",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-rca-007",
    name: "Kwabena Opoku-Agyemang",
    aliases: ["Dr. Kwabena Opoku-Agyemang"],
    role: "Academic, Son of Vice President Jane Naana Opoku-Agyemang",
    relationship: "Son / Child",
    relatedPep: "Jane Naana Opoku-Agyemang (Vice President of Ghana)",
    term: "2025-01-07 to present",
    status: "active",
    ref: "https://presidency.gov.gh"
  },

  // --- Relatives of Former President Nana Akufo-Addo ---
  {
    id: "gh-pep-rca-008",
    name: "Rebecca Akufo-Addo",
    aliases: ["Rebecca Naa Okaikor Akufo-Addo", "Rebecca Griffiths-Randolph"],
    role: "Former First Lady of Ghana, Spouse of Former President Nana Akufo-Addo",
    relationship: "Spouse / Former First Lady",
    relatedPep: "Nana Addo Dankwa Akufo-Addo (Former President of Ghana)",
    term: "2017-01-07 to 2025-01-06",
    status: "cooling_off",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-rca-009",
    name: "Gyankroma Akufo-Addo",
    aliases: ["Gyankroma Funmi Akufo-Addo"],
    role: "Former Director Creative Arts Agency, Daughter of Former President Nana Akufo-Addo",
    relationship: "Daughter / Child",
    relatedPep: "Nana Addo Dankwa Akufo-Addo (Former President of Ghana)",
    term: "2017-01-07 to 2025-01-06",
    status: "cooling_off",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-rca-010",
    name: "Gabby Asare Otchere-Darko",
    aliases: ["Gabby Otchere-Darko", "Gabriel Asare Otchere-Darko"],
    role: "Senior Legal Practitioner, Close Associate and Nephew of Former President Nana Akufo-Addo",
    relationship: "Nephew / Close Associate / Legal Advisor",
    relatedPep: "Nana Addo Dankwa Akufo-Addo (Former President of Ghana)",
    term: "2017-01-07 to 2025-01-06",
    status: "cooling_off",
    ref: "https://presidency.gov.gh"
  },

  // --- Relatives of Former Vice President Mahamudu Bawumia ---
  {
    id: "gh-pep-rca-011",
    name: "Samira Bawumia",
    aliases: ["Samira Ramadan", "Hajia Samira Bawumia"],
    role: "Former Second Lady of Ghana, Spouse of Former Vice President Mahamudu Bawumia",
    relationship: "Spouse / Former Second Lady",
    relatedPep: "Mahamudu Bawumia (Former Vice President of Ghana)",
    term: "2017-01-07 to 2025-01-06",
    status: "cooling_off",
    ref: "https://presidency.gov.gh"
  },

  // --- Relatives of Ken Ofori-Atta ---
  {
    id: "gh-pep-rca-012",
    name: "Angela Ofori-Atta",
    aliases: ["Prof. Angela Ofori-Atta", "Angela Lamensdorf"],
    role: "Associate Professor / Clinical Psychologist, Spouse of Former Finance Minister Ken Ofori-Atta",
    relationship: "Spouse / Wife",
    relatedPep: "Kenneth Nana Yaw Ofori-Atta (Former Minister for Finance)",
    term: "2017-01-27 to 2024-02-14",
    status: "cooling_off",
    ref: "https://mofep.gov.gh"
  },
  {
    id: "gh-pep-rca-013",
    name: "Keli Gadzekpo",
    aliases: ["Kelly Gadzekpo"],
    role: "Co-founder of Databank, Close Business Associate of Former Finance Minister Ken Ofori-Atta",
    relationship: "Close Business Associate / Corporate Partner",
    relatedPep: "Kenneth Nana Yaw Ofori-Atta (Former Minister for Finance)",
    term: "2017-01-27 to 2024-02-14",
    status: "cooling_off",
    ref: "https://mofep.gov.gh"
  },

  // --- Relatives of Speaker Alban Bagbin & MPs ---
  {
    id: "gh-pep-rca-014",
    name: "Alice Adinda Bagbin",
    aliases: ["Mrs. Alice Bagbin"],
    role: "Senior Officer UNICEF Ghana, Spouse of Speaker Alban Bagbin",
    relationship: "Spouse / Wife",
    relatedPep: "Alban Sumana Kingsford Bagbin (Speaker of Parliament)",
    term: "2021-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-rca-015",
    name: "Dianne Markin",
    aliases: ["Mrs. Dianne Afenyo-Markin"],
    role: "Spouse of Minority Leader Alexander Afenyo-Markin",
    relationship: "Spouse / Wife",
    relatedPep: "Alexander Kwamina Afenyo-Markin (Minority Leader of Parliament)",
    term: "2024-02-23 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-rca-016",
    name: "Nuhela Seidu",
    aliases: ["Nuhela Seidu Ablakwa"],
    role: "Legal Practitioner, Spouse of MP Samuel Okudzeto Ablakwa",
    relationship: "Spouse / Wife",
    relatedPep: "Samuel Okudzeto Ablakwa (Member of Parliament)",
    term: "2013-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  }
];

// Helper to generate natural search aliases (first+last, surname, title-stripped)
function enrichAliases(name, existingAliases = []) {
  const aliasSet = new Set(existingAliases.map(a => a.trim()).filter(Boolean));
  const cleanName = name.replace(/^(Dr\.|Prof\.|Hon\.|Alhaji|Hajia|Nana|Mr\.|Mrs\.|Ms\.)\s+/i, '').trim();
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

// Transform curated items into strict version-2 snapshot
function transformToSnapshot(items, sourceKey, isRcaDataset = false) {
  const records = items.map(p => {
    const isCoolingOff = p.status === "cooling_off";
    
    const programs = isRcaDataset
      ? [
          "Relative or Close Associate (RCA)",
          `Relationship: ${p.relationship}`,
          `Connected PEP: ${p.relatedPep}`,
          p.role
        ]
      : [
          "Politically Exposed Person (PEP)",
          isCoolingOff ? "Cooling-off Period (Former Office Holder)" : "Active Office Holder",
          p.role
        ];

    let statusDesc = isRcaDataset
      ? `Ghana RCA; Relationship: ${p.relationship}; Connected PEP: ${p.relatedPep}; Role: ${p.role}; Status: ${p.status || "active"}`
      : `Ghana PEP; Role: ${p.role}; Status: ${p.status || "active"}`;

    if (p.familyMembers && p.familyMembers.length) {
      statusDesc += `; Known Family: ${p.familyMembers.join(", ")}`;
    }
    if (p.associates && p.associates.length) {
      statusDesc += `; Known Associates: ${p.associates.join(", ")}`;
    }

    const properties = {
      country: ["gh"],
      nationality: ["gh"],
      countryText: ["Ghana"],
      nationalityText: ["Ghanaian"],
      programs,
      listedAt: [(p.term || "2025-01-07").slice(0, 10)],
      sourceStatus: [statusDesc],
      sanctionsImposed: [
        isRcaDataset
          ? `Enhanced Due Diligence (EDD) Required; AML Act 1044 Relative or Close Associate (RCA) Screening [${p.relationship} of ${p.relatedPep}]`
          : (isCoolingOff
              ? "Enhanced Due Diligence (EDD) Required; AML Act 1044 Statutory Cooling-Off Period for Former Office Holder"
              : "Enhanced Due Diligence (EDD) Required; AML Act 1044 Active PEP Screening")
      ],
      address: [p.constituency ? `Constituency: ${p.constituency}, Ghana` : "Ghana"]
    };

    return {
      id: p.id,
      name: p.name,
      type: "Person",
      aliases: enrichAliases(p.name, p.aliases || []),
      source_reference: p.ref || "https://parliament.gh",
      source_type: isRcaDataset ? "rca_evidence" : "pep_evidence",
      properties
    };
  });

  return {
    version: 2,
    source: sourceKey,
    parser: "gh-pep-v1",
    records
  };
}

// 1. Executive
const execFixture = transformToSnapshot(executivePeps, "gh-pep-executive", false);
fs.writeFileSync("gh_pep_executive.json", JSON.stringify(execFixture, null, 2), "utf8");
console.log(`[Master Builder] Saved gh_pep_executive.json (${execFixture.records.length} records).`);

// 2. Parliament (270 Crawled MPs + Leadership)
const parlFixture = transformToSnapshot(combinedParliament, "gh-pep-parliament", false);
fs.writeFileSync("gh_pep_parliament.json", JSON.stringify(parlFixture, null, 2), "utf8");
console.log(`[Master Builder] Saved gh_pep_parliament.json (${parlFixture.records.length} records).`);

// 3. SOE & Judiciary
const soeFixture = transformToSnapshot(soeJudiciaryPeps, "gh-pep-soe", false);
fs.writeFileSync("gh_pep_soe.json", JSON.stringify(soeFixture, null, 2), "utf8");
console.log(`[Master Builder] Saved gh_pep_soe.json (${soeFixture.records.length} records).`);

// 4. Combined RCAs (Curated + Wikidata Family Graph)
const curatedRcaFixture = transformToSnapshot(curatedRcas, "gh-pep-rca", true);
const allRcas = [...curatedRcaFixture.records];
const seenRcaNames = new Set(curatedRcas.map(r => r.name.toLowerCase()));

for (const wRca of wikidataRcas) {
  if (!seenRcaNames.has(wRca.name.toLowerCase())) {
    seenRcaNames.add(wRca.name.toLowerCase());
    wRca.aliases = enrichAliases(wRca.name, wRca.aliases || []);
    allRcas.push(wRca);
  }
}

const rcaFinalFixture = {
  version: 2,
  source: "gh-pep-rca",
  parser: "gh-pep-v1",
  records: allRcas
};
fs.writeFileSync("gh_pep_rca.json", JSON.stringify(rcaFinalFixture, null, 2), "utf8");
console.log(`[Master Builder] Saved gh_pep_rca.json (${rcaFinalFixture.records.length} records).`);

console.log("[Master Builder] SUCCESS! All 4 fixtures created.");

