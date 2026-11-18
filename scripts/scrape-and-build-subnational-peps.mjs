import fs from 'node:fs';

/**
 * Ghana Sub-National PEPs (MMDCEs, Regional Ministers) & Political Party Leadership
 * Ingestion and Fixture Builder
 * 
 * Generates:
 * 1. gh_pep_local_gov.json (dataset: gh-pep-local-gov)
 * 2. gh_pep_parties.json (dataset: gh-pep-parties)
 */

function enrichAliases(name, existingAliases = []) {
  const aliasSet = new Set(existingAliases.map(a => a.trim()).filter(Boolean));
  const cleanName = name.replace(/^(Dr\.|Prof\.|Hon\.|Alhaji|Hajia|Nana|Mr\.|Mrs\.|Ms\.|Lawyer|Ing\.|Maj\.\s*Gen\.|Lt\.\s*Gen\.)\s+/i, '').trim();
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
// 1. SUB-NATIONAL GOVERNMENT (16 Regional Ministers & Metropolitan/Municipal Mayors)
// ============================================================================
const localGovPeps = [
  // --- 16 Regional Ministers of Ghana (2025/2026 Mahama Administration) ---
  {
    id: "gh-pep-reg-001",
    name: "Dr. Frank Amoakohene",
    aliases: ["Frank Amoakohene", "Dr. Frank Amoakohene (Ashanti Regional Minister)"],
    role: "Regional Minister, Ashanti Region",
    term: "2025-01-28 to present",
    region: "Ashanti",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-reg-002",
    name: "Linda Obenewaa Akweley Ocloo",
    aliases: ["Linda Ocloo", "Hon. Linda Ocloo"],
    role: "Regional Minister, Greater Accra Region",
    term: "2025-01-28 to present",
    region: "Greater Accra",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-reg-003",
    name: "Ekow Panyin Okyere Eduamoah",
    aliases: ["Ekow Eduamoah", "Hon. Ekow Panyin Okyere Eduamoah"],
    role: "Regional Minister, Central Region",
    term: "2025-01-28 to present",
    region: "Central",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-reg-004",
    name: "Rita Akosua Adjei Awatey",
    aliases: ["Rita Awatey", "Rita Akosua Awatey"],
    role: "Regional Minister, Eastern Region",
    term: "2025-01-28 to present",
    region: "Eastern",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-reg-005",
    name: "Joseph Nelson",
    aliases: ["Hon. Joseph Nelson"],
    role: "Regional Minister, Western Region",
    term: "2025-01-28 to present",
    region: "Western",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-reg-006",
    name: "Wilbert Petty Brentum",
    aliases: ["Wilbert Brentum"],
    role: "Regional Minister, Western North Region",
    term: "2025-01-28 to present",
    region: "Western North",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-reg-007",
    name: "James Gunu",
    aliases: ["Hon. James Gunu"],
    role: "Regional Minister, Volta Region",
    term: "2025-01-28 to present",
    region: "Volta",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-reg-008",
    name: "John Kwadwo Gyapong",
    aliases: ["John Gyapong", "Hon. John Kwadwo Gyapong"],
    role: "Regional Minister, Oti Region",
    term: "2025-01-28 to present",
    region: "Oti",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-reg-009",
    name: "Joseph Addae Akwaboah",
    aliases: ["Joseph Akwaboah", "Hon. Joseph Addae Akwaboah"],
    role: "Regional Minister, Bono Region",
    term: "2025-01-28 to present",
    region: "Bono",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-reg-010",
    name: "Francis Owusu Antwi",
    aliases: ["Francis Antwi", "Hon. Francis Owusu Antwi"],
    role: "Regional Minister, Bono East Region",
    term: "2025-01-28 to present",
    region: "Bono East",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-reg-011",
    name: "Charity Gardner",
    aliases: ["Hon. Charity Gardner", "Mrs. Charity Gardner"],
    role: "Regional Minister, Ahafo Region",
    term: "2025-01-28 to present",
    region: "Ahafo",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-reg-012",
    name: "Ali Adolf John",
    aliases: ["Ali Adolf", "Hon. Ali Adolf John"],
    role: "Regional Minister, Northern Region",
    term: "2025-01-28 to present",
    region: "Northern",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-reg-013",
    name: "Salisu Be-Awuribe",
    aliases: ["Salisu Be Awuribe", "Hon. Salisu Be-Awuribe"],
    role: "Regional Minister, Savannah Region",
    term: "2025-01-28 to present",
    region: "Savannah",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-reg-014",
    name: "Ibrahim Tia",
    aliases: ["Hon. Ibrahim Tia"],
    role: "Regional Minister, North East Region",
    term: "2025-01-28 to present",
    region: "North East",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-reg-015",
    name: "Akamugri Donatus Atanga",
    aliases: ["Donatus Akamugri", "Hon. Donatus Atanga"],
    role: "Regional Minister, Upper East Region",
    term: "2025-01-28 to present",
    region: "Upper East",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-reg-016",
    name: "Charles Lwanga Puozuing",
    aliases: ["Charles Puozuing", "Lawyer Charles Puozuing"],
    role: "Regional Minister, Upper West Region",
    term: "2025-01-28 to present",
    region: "Upper West",
    status: "active",
    ref: "https://presidency.gov.gh"
  },

  // --- Key Metropolitan Mayors (MCEs) & Municipal Heads ---
  {
    id: "gh-pep-mce-001",
    name: "Elizabeth Sackey",
    aliases: ["Hon. Elizabeth Sackey", "Elizabeth Naa Kwatsoe Sackey", "Mayor of Accra"],
    role: "Former Metropolitan Chief Executive (Mayor), Accra Metropolitan Assembly (AMA) (2021–2025)",
    term: "2021-09-20 to 2025-01-10",
    region: "Greater Accra",
    status: "cooling_off",
    ref: "https://ama.gov.gh"
  },
  {
    id: "gh-pep-mce-002",
    name: "Samuel Pyne",
    aliases: ["Sam Pyne", "Hon. Sam Pyne", "Mayor of Kumasi"],
    role: "Former Metropolitan Chief Executive (Mayor), Kumasi Metropolitan Assembly (KMA) (2021–2025)",
    term: "2021-09-20 to 2025-01-10",
    region: "Ashanti",
    status: "cooling_off",
    ref: "https://kma.gov.gh"
  },
  {
    id: "gh-pep-mce-003",
    name: "Abdul-Mumin Issah",
    aliases: ["Abdul Mumin Issah", "Hon. Abdul-Mumin Issah", "Mayor of Sekondi-Takoradi"],
    role: "Former Metropolitan Chief Executive (Mayor), Sekondi-Takoradi Metropolitan Assembly (STMA) (2021–2025)",
    term: "2021-09-20 to 2025-01-10",
    region: "Western",
    status: "cooling_off",
    ref: "https://stma.gov.gh"
  },
  {
    id: "gh-pep-mce-004",
    name: "Sule Salifu",
    aliases: ["Hon. Sule Salifu", "Mayor of Tamale"],
    role: "Former Metropolitan Chief Executive (Mayor), Tamale Metropolitan Assembly (TaMA) (2021–2025)",
    term: "2021-09-20 to 2025-01-10",
    region: "Northern",
    status: "cooling_off",
    ref: "https://tama.gov.gh"
  },
  {
    id: "gh-pep-mce-005",
    name: "Ernest Arthur",
    aliases: ["Hon. Ernest Arthur", "Mayor of Cape Coast"],
    role: "Former Metropolitan Chief Executive (Mayor), Cape Coast Metropolitan Assembly (CCMA) (2017–2025)",
    term: "2017-05-15 to 2025-01-10",
    region: "Central",
    status: "cooling_off",
    ref: "https://ghanadistricts.com"
  },
  {
    id: "gh-pep-mce-006",
    name: "Yohane Amarh Ashitey",
    aliases: ["Yohane Ashitey", "Hon. Yohane Amarh Ashitey", "MCE Tema"],
    role: "Former Metropolitan Chief Executive, Tema Metropolitan Assembly (TMA) (2021–2025)",
    term: "2021-09-20 to 2025-01-10",
    region: "Greater Accra",
    status: "cooling_off",
    ref: "https://tma.gov.gh"
  },
  {
    id: "gh-pep-mce-007",
    name: "George Cyril Bray",
    aliases: ["Cyril Bray", "Hon. George Cyril Bray"],
    role: "Former Municipal Chief Executive, Ablekuma West Municipal Assembly (2018–2025)",
    term: "2018-03-15 to 2025-01-10",
    region: "Greater Accra",
    status: "cooling_off",
    ref: "https://ghanadistricts.com"
  },
  {
    id: "gh-pep-mce-008",
    name: "Dr. Divine Richard Bosson",
    aliases: ["Divine Bosson", "Hon. Divine Bosson"],
    role: "Former Municipal Chief Executive, Ho Municipal Assembly (2021–2025)",
    term: "2021-09-20 to 2025-01-10",
    region: "Volta",
    status: "cooling_off",
    ref: "https://ghanadistricts.com"
  },
  {
    id: "gh-pep-mce-009",
    name: "Ansu Kumi",
    aliases: ["Hon. Ansu Kumi"],
    role: "Former Municipal Chief Executive, Sunyani Municipal Assembly (2021–2025)",
    term: "2021-09-20 to 2025-01-10",
    region: "Bono",
    status: "cooling_off",
    ref: "https://ghanadistricts.com"
  },
  {
    id: "gh-pep-mce-010",
    name: "Isaac Kwadjo Buabeng",
    aliases: ["Isaac Buabeng", "Hon. Isaac Kwadjo Buabeng"],
    role: "Former Municipal Chief Executive, Nsawam Adoagyiri Municipal Assembly (2017–2025)",
    term: "2017-05-15 to 2025-01-10",
    region: "Eastern",
    status: "cooling_off",
    ref: "https://ghanadistricts.com"
  },
  {
    id: "gh-pep-mce-011",
    name: "Tahir Zubairu",
    aliases: ["Hon. Tahir Zubairu"],
    role: "Former Municipal Chief Executive, Wa Municipal Assembly (2021–2025)",
    term: "2021-09-20 to 2025-01-10",
    region: "Upper West",
    status: "cooling_off",
    ref: "https://ghanadistricts.com"
  },
  {
    id: "gh-pep-mce-012",
    name: "Rex Asanga",
    aliases: ["Hon. Rex Asanga"],
    role: "Former Municipal Chief Executive, Bolgatanga Municipal Assembly (2021–2025)",
    term: "2021-09-20 to 2025-01-10",
    region: "Upper East",
    status: "cooling_off",
    ref: "https://ghanadistricts.com"
  },

  // --- Prominent Former Regional Ministers (Under Statutory AML Act 1044 Cooling-off Period) ---
  {
    id: "gh-pep-reg-017",
    name: "Simon Osei-Mensah",
    aliases: ["Hon. Simon Osei-Mensah"],
    role: "Former Regional Minister, Ashanti Region (2017–2025)",
    term: "2017-02-17 to 2025-01-06",
    region: "Ashanti",
    status: "cooling_off",
    ref: "https://ashanti.gov.gh"
  },
  {
    id: "gh-pep-reg-018",
    name: "Henry Quartey",
    aliases: ["Hon. Henry Quartey"],
    role: "Former Regional Minister, Greater Accra Region (2021–2024), Former Minister for the Interior (2024–2025)",
    term: "2021-02-17 to 2025-01-06",
    region: "Greater Accra",
    status: "cooling_off",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-reg-019",
    name: "Kwabena Okyere Darko-Mensah",
    aliases: ["Kobby Okyere Darko-Mensah", "Hon. Kwabena Okyere Darko-Mensah"],
    role: "Former Regional Minister, Western Region (2019–2025)",
    term: "2019-02-28 to 2025-01-06",
    region: "Western",
    status: "cooling_off",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-reg-020",
    name: "Dr. Archibald Yao Letsa",
    aliases: ["Dr. Archibald Letsa", "Hon. Archibald Yao Letsa"],
    role: "Former Regional Minister, Volta Region (2017–2025)",
    term: "2017-02-17 to 2025-01-06",
    region: "Volta",
    status: "cooling_off",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-reg-021",
    name: "Seth Kwame Acheampong",
    aliases: ["Hon. Seth Kwame Acheampong"],
    role: "Former Regional Minister, Eastern Region (2021–2025)",
    term: "2021-02-17 to 2025-01-06",
    region: "Eastern",
    status: "cooling_off",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-reg-022",
    name: "Justina Marigold Assan",
    aliases: ["Mrs. Justina Marigold Assan", "Justina Assan"],
    role: "Former Regional Minister, Central Region (2021–2025)",
    term: "2021-02-17 to 2025-01-06",
    region: "Central",
    status: "cooling_off",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-reg-023",
    name: "Shani Alhassan Saibu",
    aliases: ["Alhaji Shani Alhassan Saibu", "Hon. Shani Saibu"],
    role: "Former Regional Minister, Northern Region (2021–2025)",
    term: "2021-02-17 to 2025-01-06",
    region: "Northern",
    status: "cooling_off",
    ref: "https://presidency.gov.gh"
  }
];

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
    familyMembers: ["Dr. Kwaku Asiedu Nketia (Son / Deputy CEO of MIIF)"],
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-002",
    name: "Fifi Fiavi Kwetey",
    aliases: ["Fifi Kwetey", "Hon. Fifi Kwetey", "Fifi Fiavi Franklin Kwetey"],
    party: "National Democratic Congress (NDC)",
    role: "General Secretary, National Democratic Congress (NDC), Former Minister of Transport & Agriculture",
    term: "2022-12-18 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-003",
    name: "Joseph Yamin",
    aliases: ["Hon. Joseph Yamin", "Joseph Yammin"],
    party: "National Democratic Congress (NDC)",
    role: "National Organizer, National Democratic Congress (NDC), Former Deputy Minister of Youth and Sports",
    term: "2022-12-18 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-004",
    name: "Alhaji Awudu Sofo Azorka",
    aliases: ["Chief Sofo Azorka", "Sofo Azorka", "Alhaji Sofo Azorka"],
    party: "National Democratic Congress (NDC)",
    role: "National First Vice Chairman, National Democratic Congress (NDC)",
    term: "2022-12-18 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-005",
    name: "Dr. Hanna Louisa Bisiw",
    aliases: ["Dr. Hanna Bisiw", "Hon. Dr. Hannah Louisa Bisiw"],
    party: "National Democratic Congress (NDC)",
    role: "National Women's Organizer, National Democratic Congress (NDC), Former Deputy Minister of Agriculture",
    term: "2022-12-18 to present",
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
    name: "Alhaji Masawuud Osman",
    aliases: ["Masawuud Osman", "Alhaji Masawudu Osman"],
    party: "New Patriotic Party (NPP)",
    role: "Third National Vice Chairman, New Patriotic Party (NPP)",
    term: "2022-07-17 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-018",
    name: "Kate Gyamfua",
    aliases: ["Mrs. Kate Gyamfua", "Kate Gyamfua"],
    party: "New Patriotic Party (NPP)",
    role: "National Women's Organizer, New Patriotic Party (NPP)",
    term: "2018-07-08 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-019",
    name: "Salam Mohammed Mustafa",
    aliases: ["Salam Mustapha", "Salam Mohammed Mustapha"],
    party: "New Patriotic Party (NPP)",
    role: "National Youth Organizer, New Patriotic Party (NPP)",
    term: "2022-07-17 to present",
    status: "active",
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-party-020",
    name: "Abdul Aziz Haruna Futa",
    aliases: ["Aziz Futa", "Alhaji Aziz Futa"],
    party: "New Patriotic Party (NPP)",
    role: "National Nasara Coordinator, New Patriotic Party (NPP)",
    term: "2018-07-08 to present",
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

function transformLocalGov(items) {
  const records = items.map(p => {
    const isCoolingOff = p.status === "cooling_off";
    const properties = {
      country: ["gh"],
      nationality: ["gh"],
      countryText: ["Ghana"],
      nationalityText: ["Ghanaian"],
      programs: [
        "Politically Exposed Person (PEP)",
        "Sub-National Domestic PEP (MMDCE / Regional Minister)",
        isCoolingOff ? "Cooling-off Period (Former Office Holder)" : "Active Office Holder",
        p.role
      ],
      listedAt: [(p.term || "2025-01-28").slice(0, 10)],
      sourceStatus: [`Ghana Sub-National PEP; Region: ${p.region}; Role: ${p.role}; Status: ${p.status}`],
      sanctionsImposed: [
        isCoolingOff
          ? "Enhanced Due Diligence (EDD) Required; AML Act 1044 Statutory Cooling-Off Period for Sub-National Official"
          : "Enhanced Due Diligence (EDD) Required; AML Act 1044 Active Sub-National PEP Screening"
      ],
      address: [p.region ? `${p.region} Region, Ghana` : "Ghana"]
    };

    return {
      id: p.id,
      name: p.name,
      type: "Person",
      aliases: enrichAliases(p.name, p.aliases || []),
      source_reference: p.ref || "https://presidency.gov.gh",
      source_type: "pep_evidence",
      properties
    };
  });

  return {
    version: 2,
    source: "gh-pep-local-gov",
    parser: "gh-pep-v1",
    records
  };
}

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

// 1. Build and write Local Gov Fixture
const localGovFixture = transformLocalGov(localGovPeps);
fs.writeFileSync("gh_pep_local_gov.json", JSON.stringify(localGovFixture, null, 2), "utf8");
console.log(`[Sub-National Builder] Saved gh_pep_local_gov.json (${localGovFixture.records.length} records).`);

// 2. Build and write Political Parties Fixture
const partiesFixture = transformParties(politicalPartyPeps);
fs.writeFileSync("gh_pep_parties.json", JSON.stringify(partiesFixture, null, 2), "utf8");
console.log(`[Sub-National Builder] Saved gh_pep_parties.json (${partiesFixture.records.length} records).`);

console.log("[Sub-National Builder] SUCCESS! Generated both fixtures.");
