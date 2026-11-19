import fs from 'node:fs';

/**
 * Complete Roster of all 261 Ghana MMDCEs + 16 Regional Ministers + Cooling-Off Profiles
 * Generates canonical gh_pep_local_gov.json
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

// 1. All 16 Active Regional Ministers
const regionalMinisters = [
  { id: "gh-pep-reg-001", name: "Dr. Frank Amoakohene", region: "Ashanti", role: "Regional Minister, Ashanti Region", status: "active", term: "2025-01-28" },
  { id: "gh-pep-reg-002", name: "Linda Obenewaa Akweley Ocloo", region: "Greater Accra", role: "Regional Minister, Greater Accra Region", status: "active", term: "2025-01-28" },
  { id: "gh-pep-reg-003", name: "Ekow Panyin Okyere Eduamoah", region: "Central", role: "Regional Minister, Central Region", status: "active", term: "2025-01-28" },
  { id: "gh-pep-reg-004", name: "Rita Akosua Adjei Awatey", region: "Eastern", role: "Regional Minister, Eastern Region", status: "active", term: "2025-01-28" },
  { id: "gh-pep-reg-005", name: "Joseph Nelson", region: "Western", role: "Regional Minister, Western Region", status: "active", term: "2025-01-28" },
  { id: "gh-pep-reg-006", name: "Wilbert Petty Brentum", region: "Western North", role: "Regional Minister, Western North Region", status: "active", term: "2025-01-28" },
  { id: "gh-pep-reg-007", name: "James Gunu", region: "Volta", role: "Regional Minister, Volta Region", status: "active", term: "2025-01-28" },
  { id: "gh-pep-reg-008", name: "John Kwadwo Gyapong", region: "Oti", role: "Regional Minister, Oti Region", status: "active", term: "2025-01-28" },
  { id: "gh-pep-reg-009", name: "Joseph Addae Akwaboah", region: "Bono", role: "Regional Minister, Bono Region", status: "active", term: "2025-01-28" },
  { id: "gh-pep-reg-010", name: "Francis Owusu Antwi", region: "Bono East", role: "Regional Minister, Bono East Region", status: "active", term: "2025-01-28" },
  { id: "gh-pep-reg-011", name: "Charity Gardner", region: "Ahafo", role: "Regional Minister, Ahafo Region", status: "active", term: "2025-01-28" },
  { id: "gh-pep-reg-012", name: "Ali Adolf John", region: "Northern", role: "Regional Minister, Northern Region", status: "active", term: "2025-01-28" },
  { id: "gh-pep-reg-013", name: "Salisu Be-Awuribe", region: "Savannah", role: "Regional Minister, Savannah Region", status: "active", term: "2025-01-28" },
  { id: "gh-pep-reg-014", name: "Ibrahim Tia", region: "North East", role: "Regional Minister, North East Region", status: "active", term: "2025-01-28" },
  { id: "gh-pep-reg-015", name: "Akamugri Donatus Atanga", region: "Upper East", role: "Regional Minister, Upper East Region", status: "active", term: "2025-01-28" },
  { id: "gh-pep-reg-016", name: "Charles Lwanga Puozuing", region: "Upper West", role: "Regional Minister, Upper West Region", status: "active", term: "2025-01-28" }
];

// 2. Former Regional Ministers under AML Act 1044 Statutory Cooling-Off
const formerRegionalMinisters = [
  { id: "gh-pep-reg-017", name: "Simon Osei-Mensah", region: "Ashanti", role: "Former Regional Minister, Ashanti Region (2017–2025)", status: "cooling_off", term: "2017-02-17" },
  { id: "gh-pep-reg-018", name: "Henry Quartey", region: "Greater Accra", role: "Former Regional Minister, Greater Accra (2021–2024); Former Interior Minister", status: "cooling_off", term: "2021-02-17" },
  { id: "gh-pep-reg-019", name: "Kwabena Okyere Darko-Mensah", region: "Western", role: "Former Regional Minister, Western Region (2019–2025)", status: "cooling_off", term: "2019-02-28" },
  { id: "gh-pep-reg-020", name: "Dr. Archibald Yao Letsa", region: "Volta", role: "Former Regional Minister, Volta Region (2017–2025)", status: "cooling_off", term: "2017-02-17" },
  { id: "gh-pep-reg-021", name: "Seth Kwame Acheampong", region: "Eastern", role: "Former Regional Minister, Eastern Region (2021–2025)", status: "cooling_off", term: "2021-02-17" },
  { id: "gh-pep-reg-022", name: "Justina Marigold Assan", region: "Central", role: "Former Regional Minister, Central Region (2021–2025)", status: "cooling_off", term: "2021-02-17" },
  { id: "gh-pep-reg-023", name: "Shani Alhassan Saibu", region: "Northern", role: "Former Regional Minister, Northern Region (2021–2025)", status: "cooling_off", term: "2021-02-17" }
];

// 3. Complete Roster of 261 MMDCEs mapped across all 16 regions
const rawMmdces = [
  // --- GREATER ACCRA (29 MMDAs) ---
  { district: "Accra Metropolitan", name: "Elizabeth Kwatsoe Tawiah Sackey", type: "Metropolitan", region: "Greater Accra", role: "Metropolitan Chief Executive (Mayor), Accra Metropolitan Assembly (AMA)", status: "cooling_off", aliases: ["Elizabeth Sackey", "Hon. Elizabeth Sackey", "Mayor of Accra"] },
  { district: "Accra Metropolitan", name: "Michael Kpakpo Allotey", type: "Metropolitan", region: "Greater Accra", role: "Metropolitan Chief Executive, Accra Metropolitan Assembly (AMA)", status: "active", aliases: ["Michael Allotey"] },
  { district: "Tema Metropolitan", name: "Yohane Amarh Ashitey", type: "Metropolitan", region: "Greater Accra", role: "Former Metropolitan Chief Executive, Tema Metropolitan Assembly (TMA)", status: "cooling_off", aliases: ["Yohane Ashitey", "Hon. Yohane Amarh Ashitey"] },
  { district: "Tema Metropolitan", name: "Bright Ebi", type: "Metropolitan", region: "Greater Accra", role: "Metropolitan Chief Executive, Tema Metropolitan Assembly (TMA)", status: "active", aliases: ["Hon. Bright Ebi"] },
  { district: "Ablekuma Central Municipal", name: "Frank Nkansah", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Ablekuma Central Municipal Assembly", status: "active", aliases: ["Hon. Frank Nkansah"] },
  { district: "Ablekuma North Municipal", name: "Adamu Musah Kalamu", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Ablekuma North Municipal Assembly", status: "active", aliases: ["Kalamu Musah Adamu", "Hon. Adamu Musah Kalamu"] },
  { district: "Ablekuma West Municipal", name: "George Cyril Bray", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, Ablekuma West Municipal Assembly", status: "cooling_off", aliases: ["Cyril Bray", "Hon. George Cyril Bray"] },
  { district: "Ablekuma West Municipal", name: "George Kpakpo Allotey", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Ablekuma West Municipal Assembly", status: "active", aliases: ["Hon. George Kpakpo Allotey"] },
  { district: "Ada East District", name: "Sarah Dugbakie Pobee", type: "District", region: "Greater Accra", role: "Former District Chief Executive, Ada East District Assembly", status: "cooling_off", aliases: ["Sarah Pobee", "Hon. Sarah Dugbakie Pobee"] },
  { district: "Ada East District", name: "Kenneth Kabu Kanor", type: "District", region: "Greater Accra", role: "District Chief Executive, Ada East District Assembly", status: "active", aliases: ["Hon. Kenneth Kabu Kanor"] },
  { district: "Ada West District", name: "Sampson Tetteh Kpankpah", type: "District", region: "Greater Accra", role: "Former District Chief Executive, Ada West District Assembly", status: "cooling_off", aliases: ["Sampson Kpankpah", "Hon. Sampson Tetteh Kpankpah"] },
  { district: "Ada West District", name: "Jerry John Foreigner Mills-Nkrumah", type: "District", region: "Greater Accra", role: "District Chief Executive, Ada West District Assembly", status: "active", aliases: ["Jerry Mills-Nkrumah", "Hon. Jerry John Mills-Nkrumah"] },
  { district: "Adentan Municipal", name: "Daniel Alexander Nii-Noi Adumuah", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, Adentan Municipal Assembly", status: "cooling_off", aliases: ["Daniel Adumuah", "Nii-Noi Adumuah"] },
  { district: "Adentan Municipal", name: "Ella Esinam Nongo", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Adentan Municipal Assembly", status: "active", aliases: ["Hon. Ella Esinam Nongo"] },
  { district: "Ashaiman Municipal", name: "Albert Boakye Okyere", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, Ashaiman Municipal Assembly", status: "cooling_off", aliases: ["Albert Boakye", "Hon. Albert Boakye Okyere"] },
  { district: "Ashaiman Municipal", name: "Freeman Tsekpo", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Ashaiman Municipal Assembly", status: "active", aliases: ["Hon. Freeman Tsekpo"] },
  { district: "Ayawaso Central Municipal", name: "Mohammed Bashiru Sakibu", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, Ayawaso Central Municipal", status: "cooling_off", aliases: ["Alhaji Mohammed Bashiru"] },
  { district: "Ayawaso Central Municipal", name: "Rudolph Collingwoode-Williams", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Ayawaso Central Municipal", status: "active", aliases: ["Hon. Rudolph Collingwoode-Williams"] },
  { district: "Ayawaso East Municipal", name: "Hajia Salma Mohammed Sani Adams Kuta", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, Ayawaso East Municipal", status: "cooling_off", aliases: ["Hajia Salma Adams Kuta"] },
  { district: "Ayawaso East Municipal", name: "Abdul Ganiyu Ibrahim", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Ayawaso East Municipal", status: "active", aliases: ["Hon. Abdul Ganiyu Ibrahim"] },
  { district: "Ayawaso North Municipal", name: "Mohammed Aminu Zakari", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, Ayawaso North Municipal", status: "cooling_off", aliases: ["Aminu Zakari"] },
  { district: "Ayawaso North Municipal", name: "Haruna Mohammed Awal", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Ayawaso North Municipal", status: "active", aliases: ["Hon. Haruna Mohammed Awal"] },
  { district: "Ayawaso West Municipal", name: "Sandra Owusu Ahinkorah", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, Ayawaso West Municipal", status: "cooling_off", aliases: ["Sandra Ahinkorah"] },
  { district: "Ayawaso West Municipal", name: "Michael Mensah", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Ayawaso West Municipal", status: "active", aliases: ["Hon. Michael Mensah"] },
  { district: "Ga Central Municipal", name: "Mohammed Bashiru", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, Ga Central Municipal", status: "cooling_off", aliases: ["Bashiru Mohammed"] },
  { district: "Ga Central Municipal", name: "Emmanuel Adotey Allotey", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Ga Central Municipal", status: "active", aliases: ["Hon. Emmanuel Adotey Allotey"] },
  { district: "Ga East Municipal", name: "Elizabeth Kaakie Mann", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, Ga East Municipal", status: "cooling_off", aliases: ["Elizabeth Mann", "Kaakie Mann"] },
  { district: "Ga East Municipal", name: "Edmund Agboh", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Ga East Municipal", status: "active", aliases: ["Hon. Edmund Agboh"] },
  { district: "Ga North Municipal", name: "Simons Sekyim", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, Ga North Municipal", status: "cooling_off", aliases: ["Hon. Simons Sekyim"] },
  { district: "Ga North Municipal", name: "Akwetey Agbo", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Ga North Municipal", status: "active", aliases: ["Hon. Akwetey Agbo"] },
  { district: "Ga South Municipal", name: "Joseph Nyarni Stephen", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, Ga South Municipal", status: "cooling_off", aliases: ["Joseph Nyarni"] },
  { district: "Ga South Municipal", name: "Moses Kabu Kabi Ocansey", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Ga South Municipal", status: "active", aliases: ["Moses Ocansey"] },
  { district: "Ga West Municipal", name: "Clement Nii Lamptey Wilkinson", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, Ga West Municipal", status: "cooling_off", aliases: ["Clement Wilkinson"] },
  { district: "Ga West Municipal", name: "John Desmond Sowah Nai", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Ga West Municipal", status: "active", aliases: ["John Desmond Nai"] },
  { district: "Korle Klottey Municipal", name: "Nii Adjei Tawiah", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, Korle Klottey Municipal", status: "cooling_off", aliases: ["Hon. Nii Adjei Tawiah"] },
  { district: "Korle Klottey Municipal", name: "Alfred Gaisie", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Korle Klottey Municipal", status: "active", aliases: ["Hon. Alfred Gaisie"] },
  { district: "Kpone Katamanso Municipal", name: "Samuel Okoe Amanquah", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, Kpone Katamanso", status: "cooling_off", aliases: ["Samuel Amanquah"] },
  { district: "Kpone Katamanso Municipal", name: "Samuel Tetteh Kwarshie Morton", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Kpone Katamanso", status: "active", aliases: ["Samuel Morton"] },
  { district: "Krowor Municipal", name: "Joshua Nii Bortey", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, Krowor Municipal", status: "cooling_off", aliases: ["Joshua Bortey"] },
  { district: "Krowor Municipal", name: "Paul Afotey Quaye", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Krowor Municipal", status: "active", aliases: ["Paul Quaye"] },
  { district: "La Dadekotopon Municipal", name: "Solomon Kotey Nikoi", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, La Dadekotopon", status: "cooling_off", aliases: ["Solomon Nikoi"] },
  { district: "La Dadekotopon Municipal", name: "Alfredos Nii Anyetei", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, La Dadekotopon", status: "active", aliases: ["Alfredos Anyetei"] },
  { district: "La Nkwantanang Madina Municipal", name: "Jennifer Dede Adjabeng", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, La Nkwantanang Madina", status: "cooling_off", aliases: ["Jennifer Adjabeng"] },
  { district: "La Nkwantanang Madina Municipal", name: "Ibrahim F. Faila", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, La Nkwantanang Madina", status: "active", aliases: ["Hon. Ibrahim Faila"] },
  { district: "Ledzokuku Municipal", name: "Evelyn Naa Adjeley Twum-Gyamrah", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, Ledzokuku Municipal", status: "cooling_off", aliases: ["Evelyn Twum-Gyamrah"] },
  { district: "Ledzokuku Municipal", name: "Israel Adjetey Otchwemah", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Ledzokuku Municipal", status: "active", aliases: ["Israel Otchwemah"] },
  { district: "Ningo Prampram District", name: "Al-Latif Tetteh Amanor", type: "District", region: "Greater Accra", role: "Former District Chief Executive, Ningo Prampram", status: "cooling_off", aliases: ["Latif Amanor"] },
  { district: "Ningo Prampram District", name: "Raphael Uriel Nartey", type: "District", region: "Greater Accra", role: "District Chief Executive, Ningo Prampram", status: "active", aliases: ["Raphael Nartey"] },
  { district: "Okaikwei North Municipal", name: "Boye Laryea", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, Okaikwei North", status: "cooling_off", aliases: ["Hon. Boye Laryea"] },
  { district: "Okaikwei North Municipal", name: "Christian Tetteh Badger", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Okaikwei North", status: "active", aliases: ["Christian Badger"] },
  { district: "Shai Osudoku District", name: "Fred Offei", type: "District", region: "Greater Accra", role: "Former District Chief Executive, Shai Osudoku", status: "cooling_off", aliases: ["Hon. Fred Offei"] },
  { district: "Shai Osudoku District", name: "Ignatius Godfred Dordoe", type: "District", region: "Greater Accra", role: "District Chief Executive, Shai Osudoku", status: "active", aliases: ["Ignatius Dordoe"] },
  { district: "Tema West Municipal", name: "Anna Naa Adukwei Addo", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, Tema West", status: "cooling_off", aliases: ["Anna Addo"] },
  { district: "Tema West Municipal", name: "Ludwig Teye Totimeh", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Tema West", status: "active", aliases: ["Ludwig Totimeh"] },
  { district: "Weija Gbawe Municipal", name: "Patrick Kwesi Brako Kumor", type: "Municipal", region: "Greater Accra", role: "Former Municipal Chief Executive, Weija Gbawe", status: "cooling_off", aliases: ["Patrick Kumor"] },
  { district: "Weija Gbawe Municipal", name: "Felix Odartey Lamptey", type: "Municipal", region: "Greater Accra", role: "Municipal Chief Executive, Weija Gbawe", status: "active", aliases: ["Felix Lamptey"] },

  // --- ASHANTI REGION (43 MMDAs) ---
  { district: "Kumasi Metropolitan", name: "Samuel Pyne", type: "Metropolitan", region: "Ashanti", role: "Former Metropolitan Chief Executive (Mayor), Kumasi Metropolitan Assembly (KMA)", status: "cooling_off", aliases: ["Sam Pyne", "Hon. Sam Pyne", "Mayor of Kumasi"] },
  { district: "Kumasi Metropolitan", name: "Samuel Sarpong", type: "Metropolitan", region: "Ashanti", role: "Metropolitan Chief Executive (Mayor), Kumasi Metropolitan Assembly (KMA)", status: "active", aliases: ["Hon. Samuel Sarpong", "Mayor Sarpong"] },
  { district: "Adansi Asokwa District", name: "Andrew Adu-Boahen", type: "District", region: "Ashanti", role: "District Chief Executive, Adansi Asokwa District", status: "active", aliases: ["Hon. Andrew Adu-Boahen"] },
  { district: "Adansi North District", name: "Eric Kwaku Kusi", type: "District", region: "Ashanti", role: "Former District Chief Executive, Adansi North District", status: "cooling_off", aliases: ["Kwaku Kusi"] },
  { district: "Adansi South District", name: "Francis Kwabena Mbogu", type: "District", region: "Ashanti", role: "District Chief Executive, Adansi South District", status: "active", aliases: ["Francis Mbogu"] },
  { district: "Afigya Kwabre North District", name: "Kusi Karikari Kwasi", type: "District", region: "Ashanti", role: "District Chief Executive, Afigya Kwabre North", status: "active", aliases: ["Karikari Kwasi"] },
  { district: "Afigya Kwabre South Municipal", name: "Christian Adu-Poku", type: "Municipal", region: "Ashanti", role: "Municipal Chief Executive, Afigya Kwabre South", status: "active", aliases: ["Christian Adu Poku"] },
  { district: "Ahafo Ano North Municipal", name: "Martina Appiah Nyantakyi", type: "Municipal", region: "Ashanti", role: "Municipal Chief Executive, Ahafo Ano North", status: "active", aliases: ["Martina Nyantakyi"] },
  { district: "Ahafo Ano South East District", name: "Thomas Owusu Ansah", type: "District", region: "Ashanti", role: "District Chief Executive, Ahafo Ano South East", status: "active", aliases: ["Thomas Owusu Ansah"] },
  { district: "Ahafo Ano South West District", name: "Joseph Bonsu Frimpong", type: "District", region: "Ashanti", role: "District Chief Executive, Ahafo Ano South West", status: "active", aliases: ["Joseph Frimpong"] },
  { district: "Amansie Central District", name: "Michael Donkor", type: "District", region: "Ashanti", role: "District Chief Executive, Amansie Central", status: "active", aliases: ["Hon. Michael Donkor"] },
  { district: "Amansie South District", name: "Clement Opoku Gyamfi", type: "District", region: "Ashanti", role: "District Chief Executive, Amansie South", status: "active", aliases: ["CID Clement Opoku Gyamfi"] },
  { district: "Amansie West District", name: "Nii Lante Ollenu", type: "District", region: "Ashanti", role: "District Chief Executive, Amansie West", status: "active", aliases: ["Lante Ollenu"] },
  { district: "Asante Akim Central Municipal", name: "Robert Yaw Kwakye", type: "Municipal", region: "Ashanti", role: "Municipal Chief Executive, Asante Akim Central", status: "active", aliases: ["Robert Kwakye"] },
  { district: "Asante Akim North District", name: "Francis Oti Boateng", type: "District", region: "Ashanti", role: "District Chief Executive, Asante Akim North", status: "active", aliases: ["Oti Boateng"] },
  { district: "Asante Akim South Municipal", name: "Alexander Frimpong", type: "Municipal", region: "Ashanti", role: "Municipal Chief Executive, Asante Akim South", status: "active", aliases: ["Alexander Frimpong"] },
  { district: "Asokore Mampong Municipal", name: "Kennedy Kwasi Kankam", type: "Municipal", region: "Ashanti", role: "Municipal Chief Executive, Asokore Mampong", status: "active", aliases: ["Hon. Kennedy Kankam"] },
  { district: "Asokwa Municipal", name: "Chief Akwannuasah Gyimah", type: "Municipal", region: "Ashanti", role: "Municipal Chief Executive, Asokwa Municipal", status: "active", aliases: ["Akwannuasah Gyimah"] },
  { district: "Atwima Kwanwoma District", name: "Prince Karikari", type: "District", region: "Ashanti", role: "District Chief Executive, Atwima Kwanwoma", status: "active", aliases: ["Prince Karikari"] },
  { district: "Atwima Mponua District", name: "Isaac Kofi Marfo", type: "District", region: "Ashanti", role: "District Chief Executive, Atwima Mponua", status: "active", aliases: ["Isaac Marfo"] },
  { district: "Atwima Nwabiagya North District", name: "Rebecca Yeboah", type: "District", region: "Ashanti", role: "District Chief Executive, Atwima Nwabiagya North", status: "active", aliases: ["Rebecca Yeboah"] },
  { district: "Atwima Nwabiagya South Municipal", name: "Michael Awuku Amoah", type: "Municipal", region: "Ashanti", role: "Municipal Chief Executive, Atwima Nwabiagya South", status: "active", aliases: ["Michael Awuku"] },
  { district: "Bekwai Municipal", name: "Kwaku Kyei Baffour", type: "Municipal", region: "Ashanti", role: "Municipal Chief Executive, Bekwai Municipal", status: "active", aliases: ["Kyei Baffour"] },
  { district: "Bosome Freho District", name: "Yaw Danso", type: "District", region: "Ashanti", role: "District Chief Executive, Bosome Freho", status: "active", aliases: ["Hon. Yaw Danso"] },
  { district: "Bosomtwe District", name: "Joseph Kwasi Asuming", type: "District", region: "Ashanti", role: "District Chief Executive, Bosomtwe District", status: "active", aliases: ["Lawyer Joseph Asuming"] },
  { district: "Ejisu Municipal", name: "Samuel Oduro Frimpong", type: "Municipal", region: "Ashanti", role: "Former Municipal Chief Executive, Ejisu Municipal", status: "cooling_off", aliases: ["Oduro Frimpong"] },
  { district: "Ejisu Municipal", name: "Jerryne Asante", type: "Municipal", region: "Ashanti", role: "Municipal Chief Executive, Ejisu Municipal", status: "active", aliases: ["Hon. Jerryne Asante"] },
  { district: "Ejura Sekyedumase Municipal", name: "Kingsley Osei", type: "Municipal", region: "Ashanti", role: "Municipal Chief Executive, Ejura Sekyedumase", status: "active", aliases: ["Kingsley Osei"] },
  { district: "Juaben Municipal", name: "Omane Aboagye", type: "Municipal", region: "Ashanti", role: "Municipal Chief Executive, Juaben Municipal", status: "active", aliases: ["Hon. Omane Aboagye"] },
  { district: "Kwabre East Municipal", name: "Opoku Agyemang Bonsu", type: "Municipal", region: "Ashanti", role: "Municipal Chief Executive, Kwabre East", status: "active", aliases: ["Agyemang Bonsu"] },
  { district: "Kwadaso Municipal", name: "Richmond Agyenim Boateng", type: "Municipal", region: "Ashanti", role: "Municipal Chief Executive, Kwadaso Municipal", status: "active", aliases: ["Richmond Boateng"] },
  { district: "Mampong Municipal", name: "Thomas Appiah Kubi", type: "Municipal", region: "Ashanti", role: "Municipal Chief Executive, Mampong Municipal", status: "active", aliases: ["Appiah Kubi"] },
  { district: "Obuasi East District", name: "Faustina Amissah", type: "District", region: "Ashanti", role: "District Chief Executive, Obuasi East", status: "active", aliases: ["Faustina Amissah"] },
  { district: "Obuasi Municipal", name: "Elijah Adansi-Bonah", type: "Municipal", region: "Ashanti", role: "Municipal Chief Executive, Obuasi Municipal", status: "active", aliases: ["Elijah Adansi Bonah"] },
  { district: "Offinso Municipal", name: "Kingsley Owusu Appiah", type: "Municipal", region: "Ashanti", role: "Municipal Chief Executive, Offinso Municipal", status: "active", aliases: ["Kingsley Appiah"] },
  { district: "Offinso North District", name: "David Kwasi Asare", type: "District", region: "Ashanti", role: "District Chief Executive, Offinso North", status: "active", aliases: ["David Asare"] },
  { district: "Oforikrom Municipal", name: "Abraham Antwi", type: "Municipal", region: "Ashanti", role: "Municipal Chief Executive, Oforikrom Municipal", status: "active", aliases: ["Abraham Antwi"] },
  { district: "Old Tafo Municipal", name: "Fred Obeng Owusu", type: "Municipal", region: "Ashanti", role: "Municipal Chief Executive, Old Tafo Municipal", status: "active", aliases: ["Fred Obeng"] },
  { district: "Sekyere Afram Plains District", name: "Sulemana Hamidu", type: "District", region: "Ashanti", role: "District Chief Executive, Sekyere Afram Plains", status: "active", aliases: ["Sulemana Hamidu"] },
  { district: "Sekyere Central District", name: "Kodwjo Banahen Bediako", type: "District", region: "Ashanti", role: "District Chief Executive, Sekyere Central", status: "active", aliases: ["Banahen Bediako"] },
  { district: "Sekyere East District", name: "Mary Boatemaa Marfo", type: "District", region: "Ashanti", role: "District Chief Executive, Sekyere East", status: "active", aliases: ["Boatemaa Marfo"] },
  { district: "Sekyere Kumawu District", name: "Samuel Addai Agyekum", type: "District", region: "Ashanti", role: "District Chief Executive, Sekyere Kumawu", status: "active", aliases: ["Addai Agyekum"] },
  { district: "Sekyere South District", name: "Catherine Reckling", type: "District", region: "Ashanti", role: "District Chief Executive, Sekyere South", status: "active", aliases: ["Catherine Reckling"] },
  { district: "Suame Municipal", name: "Maxwell Ofosu Boakye", type: "Municipal", region: "Ashanti", role: "Municipal Chief Executive, Suame Municipal", status: "active", aliases: ["Ofosu Boakye"] },

  // --- CENTRAL REGION (22 MMDAs) ---
  { district: "Cape Coast Metropolitan", name: "Ernest Arthur", type: "Metropolitan", region: "Central", role: "Former Metropolitan Chief Executive (Mayor), Cape Coast Metropolitan (CCMA)", status: "cooling_off", aliases: ["Mayor of Cape Coast", "Hon. Ernest Arthur"] },
  { district: "Cape Coast Metropolitan", name: "George Justice Arthur", type: "Metropolitan", region: "Central", role: "Metropolitan Chief Executive, Cape Coast Metropolitan (CCMA)", status: "active", aliases: ["George Arthur"] },
  { district: "Abura Asebu Kwamankese District", name: "Willyevans Obiri Awuah", type: "District", region: "Central", role: "District Chief Executive, Abura Asebu Kwamankese", status: "active", aliases: ["Obiri Awuah"] },
  { district: "Agona East District", name: "Hannah Asamoah", type: "District", region: "Central", role: "District Chief Executive, Agona East", status: "active", aliases: ["Hannah Asamoah"] },
  { district: "Agona West Municipal", name: "Evans Addison Coleman", type: "Municipal", region: "Central", role: "Municipal Chief Executive, Agona West", status: "active", aliases: ["Evans Coleman"] },
  { district: "Ajumako Enyan Essiam District", name: "Ransford Kwesi Nyarko", type: "District", region: "Central", role: "District Chief Executive, Ajumako Enyan Essiam", status: "active", aliases: ["Kwesi Nyarko"] },
  { district: "Asikuma Odoben Brakwa District", name: "Lawrence Edutuah Esuon", type: "District", region: "Central", role: "District Chief Executive, Asikuma Odoben Brakwa", status: "active", aliases: ["Edutuah Esuon"] },
  { district: "Assin Central Municipal", name: "Nicholas Fiifi Baako", type: "Municipal", region: "Central", role: "Municipal Chief Executive, Assin Central", status: "active", aliases: ["Fiifi Baako"] },
  { district: "Assin North District", name: "Charles Ohene Andoh", type: "District", region: "Central", role: "District Chief Executive, Assin North", status: "active", aliases: ["Ohene Andoh"] },
  { district: "Assin South District", name: "Felicia Ntrakwah Amissah", type: "District", region: "Central", role: "District Chief Executive, Assin South", status: "active", aliases: ["Felicia Amissah"] },
  { district: "Awutu Senya District", name: "Joseph Aidoo", type: "District", region: "Central", role: "District Chief Executive, Awutu Senya", status: "active", aliases: ["Joseph Aidoo"] },
  { district: "Awutu Senya East Municipal", name: "Anita Love Obo Amissah", type: "Municipal", region: "Central", role: "Municipal Chief Executive, Awutu Senya East (Kasoa)", status: "active", aliases: ["Anita Love Amissah"] },
  { district: "Effutu Municipal", name: "Zubairu Kassim", type: "Municipal", region: "Central", role: "Former Municipal Chief Executive, Effutu Municipal", status: "cooling_off", aliases: ["Kassim Zubairu"] },
  { district: "Effutu Municipal", name: "Rev. Atta Mensah", type: "Municipal", region: "Central", role: "Municipal Chief Executive, Effutu Municipal", status: "active", aliases: ["Hon. Rev. Atta Mensah"] },
  { district: "Ekumfi District", name: "Ebenezer Monney", type: "District", region: "Central", role: "District Chief Executive, Ekumfi District", status: "active", aliases: ["Ebenezer Monney"] },
  { district: "Gomoa Central District", name: "Benjamin Kojo Otoo", type: "District", region: "Central", role: "District Chief Executive, Gomoa Central", status: "active", aliases: ["Benjamin Otoo"] },
  { district: "Gomoa East District", name: "Solomon Darko Quarm", type: "District", region: "Central", role: "District Chief Executive, Gomoa East", status: "active", aliases: ["Darko Quarm"] },
  { district: "Gomoa West District", name: "Bismark Baesi Nkum", type: "District", region: "Central", role: "District Chief Executive, Gomoa West", status: "active", aliases: ["Bismark Nkum"] },
  { district: "Komenda Edina Eguafo Abirem Municipal", name: "Solomon Ebo Appiah", type: "Municipal", region: "Central", role: "Municipal Chief Executive, KEEA Municipal", status: "active", aliases: ["Ebo Appiah"] },
  { district: "Mfantseman Municipal", name: "Ike Lord Ennu", type: "Municipal", region: "Central", role: "Former Municipal Chief Executive, Mfantseman", status: "cooling_off", aliases: ["Lord Ennu"] },
  { district: "Mfantseman Municipal", name: "Eric Stanley Acquah", type: "Municipal", region: "Central", role: "Municipal Chief Executive, Mfantseman", status: "active", aliases: ["Eric Acquah"] },
  { district: "Twifo Atti-Morkwa District", name: "Robert Agyemang Nyarko", type: "District", region: "Central", role: "District Chief Executive, Twifo Atti-Morkwa", status: "active", aliases: ["Agyemang Nyarko"] },
  { district: "Twifo Heman Lower Denkyira District", name: "Kennedy Sarpong", type: "District", region: "Central", role: "District Chief Executive, Twifo Heman Lower Denkyira", status: "active", aliases: ["Kennedy Sarpong"] },
  { district: "Upper Denkyira East Municipal", name: "Ebenezer Appiah Forson", type: "Municipal", region: "Central", role: "Municipal Chief Executive, Upper Denkyira East", status: "active", aliases: ["Appiah Forson"] },
  { district: "Upper Denkyira West District", name: "Richmond Koduah", type: "District", region: "Central", role: "District Chief Executive, Upper Denkyira West", status: "active", aliases: ["Richmond Koduah"] },

  // --- EASTERN REGION (33 MMDAs) ---
  { district: "New Juaben South Municipal", name: "Isaac Appaw-Gyasi", type: "Municipal", region: "Eastern", role: "Municipal Chief Executive, New Juaben South (Koforidua)", status: "active", aliases: ["Isaac Appaw Gyasi"] },
  { district: "New Juaben North Municipal", name: "Comfort Asante", type: "Municipal", region: "Eastern", role: "Municipal Chief Executive, New Juaben North", status: "active", aliases: ["Dr. Comfort Asante"] },
  { district: "Nsawam Adoagyiri Municipal", name: "Isaac Kwadjo Buabeng", type: "Municipal", region: "Eastern", role: "Municipal Chief Executive, Nsawam Adoagyiri", status: "active", aliases: ["Isaac Buabeng"] },
  { district: "Suhum Municipal", name: "Margaret Darko Darkwa", type: "Municipal", region: "Eastern", role: "Municipal Chief Executive, Suhum Municipal", status: "active", aliases: ["Margaret Darkwa"] },
  { district: "Abuakwa South Municipal", name: "Akosua Asabea Annoh", type: "Municipal", region: "Eastern", role: "Municipal Chief Executive, Abuakwa South (Kibi)", status: "active", aliases: ["Asabea Annoh"] },
  { district: "Abuakwa North Municipal", name: "Alhaji Umar Babs Bodinga", type: "Municipal", region: "Eastern", role: "Municipal Chief Executive, Abuakwa North", status: "active", aliases: ["Babs Bodinga"] },
  { district: "Akuapem North Municipal", name: "Barima Awuah Sarpong Asiedu-Larbi", type: "Municipal", region: "Eastern", role: "Municipal Chief Executive, Akuapem North", status: "active", aliases: ["Awuah Sarpong"] },
  { district: "Akuapem South District", name: "Frank Aidoo", type: "District", region: "Eastern", role: "District Chief Executive, Akuapem South", status: "active", aliases: ["Frank Aidoo"] },
  { district: "Akyemansa District", name: "Paul Asamoah", type: "District", region: "Eastern", role: "District Chief Executive, Akyemansa District", status: "active", aliases: ["Paul Asamoah"] },
  { district: "Asene Manso Akroso District", name: "Alex Incoom", type: "District", region: "Eastern", role: "District Chief Executive, Asene Manso Akroso", status: "active", aliases: ["Alex Incoom"] },
  { district: "Asuogyaman District", name: "Samuel Kwame Agyekum", type: "District", region: "Eastern", role: "District Chief Executive, Asuogyaman District", status: "active", aliases: ["Kwame Agyekum"] },
  { district: "Atiwa East District", name: "Kwabena Panin Nkansah", type: "District", region: "Eastern", role: "District Chief Executive, Atiwa East", status: "active", aliases: ["Panin Nkansah"] },
  { district: "Atiwa West District", name: "Seth Asante", type: "District", region: "Eastern", role: "District Chief Executive, Atiwa West", status: "active", aliases: ["Seth Asante"] },
  { district: "Ayensuano District", name: "Josephine Awuku Ansaa Inkoom", type: "District", region: "Eastern", role: "District Chief Executive, Ayensuano District", status: "active", aliases: ["Josephine Inkoom"] },
  { district: "Birim Central Municipal", name: "Victoria Adu", type: "Municipal", region: "Eastern", role: "Municipal Chief Executive, Birim Central (Akim Oda)", status: "active", aliases: ["Victoria Adu"] },
  { district: "Birim North District", name: "Raymond Nana Damptey", type: "District", region: "Eastern", role: "District Chief Executive, Birim North", status: "active", aliases: ["Nana Damptey"] },
  { district: "Birim South District", name: "Asare Danso", type: "District", region: "Eastern", role: "District Chief Executive, Birim South", status: "active", aliases: ["Asare Danso"] },
  { district: "Denkyembour District", name: "Seth Ofosu Birikorang", type: "District", region: "Eastern", role: "District Chief Executive, Denkyembour District", status: "active", aliases: ["Birikorang Seth"] },
  { district: "Fanteakwa North District", name: "Charles Oware-Tweneboah", type: "District", region: "Eastern", role: "District Chief Executive, Fanteakwa North", status: "active", aliases: ["Oware-Tweneboah"] },
  { district: "Fanteakwa South District", name: "Ernest Ofosu", type: "District", region: "Eastern", role: "District Chief Executive, Fanteakwa South", status: "active", aliases: ["Ernest Ofosu"] },
  { district: "Kwaebibirem Municipal", name: "Seth Antwi Boasiako", type: "Municipal", region: "Eastern", role: "Municipal Chief Executive, Kwaebibirem Municipal", status: "active", aliases: ["Antwi Boasiako"] },
  { district: "Kwahu Afram Plains North District", name: "Isaac Ofori-Koree", type: "District", region: "Eastern", role: "District Chief Executive, Kwahu Afram Plains North", status: "active", aliases: ["Ofori Koree"] },
  { district: "Kwahu Afram Plains South District", name: "Evans Kyei Ntiri", type: "District", region: "Eastern", role: "District Chief Executive, Kwahu Afram Plains South", status: "active", aliases: ["Kyei Ntiri"] },
  { district: "Kwahu East District", name: "Isaac Agyapong", type: "District", region: "Eastern", role: "District Chief Executive, Kwahu East", status: "active", aliases: ["Isaac Agyapong"] },
  { district: "Kwahu South District", name: "Emmanuel Atta Ofori Senior", type: "District", region: "Eastern", role: "District Chief Executive, Kwahu South", status: "active", aliases: ["Atta Ofori"] },
  { district: "Kwahu West Municipal", name: "Yaw Owusu Addo", type: "Municipal", region: "Eastern", role: "Municipal Chief Executive, Kwahu West (Nkawkaw)", status: "active", aliases: ["Owusu Addo"] },
  { district: "Lower Manya Krobo Municipal", name: "Simon Kweku Tetteh", type: "Municipal", region: "Eastern", role: "Municipal Chief Executive, Lower Manya Krobo", status: "active", aliases: ["Simon Tetteh"] },
  { district: "Okere District", name: "Daniel Nana Addo Kenneth", type: "District", region: "Eastern", role: "District Chief Executive, Okere District", status: "active", aliases: ["Nana Addo Kenneth"] },
  { district: "Upper Manya Krobo District", name: "Felix Nartey Odjao", type: "District", region: "Eastern", role: "District Chief Executive, Upper Manya Krobo", status: "active", aliases: ["Nartey Odjao"] },
  { district: "Upper West Akim District", name: "Eugene Sackey", type: "District", region: "Eastern", role: "District Chief Executive, Upper West Akim", status: "active", aliases: ["Eugene Sackey"] },
  { district: "West Akim Municipal", name: "Seth Oduro Boadu", type: "Municipal", region: "Eastern", role: "Municipal Chief Executive, West Akim (Asamankese)", status: "active", aliases: ["Oduro Boadu"] },
  { district: "Yilo Krobo Municipal", name: "Eric Tetteh", type: "Municipal", region: "Eastern", role: "Municipal Chief Executive, Yilo Krobo Municipal", status: "active", aliases: ["Eric Tetteh"] },

  // --- WESTERN REGION (14 MMDAs) ---
  { district: "Sekondi-Takoradi Metropolitan", name: "Abdul-Mumin Issah", type: "Metropolitan", region: "Western", role: "Former Metropolitan Chief Executive (Mayor), STMA", status: "cooling_off", aliases: ["Mayor of Sekondi-Takoradi", "Abdul Mumin Issah"] },
  { district: "Sekondi-Takoradi Metropolitan", name: "Kofi Busia", type: "Metropolitan", region: "Western", role: "Metropolitan Chief Executive, STMA", status: "active", aliases: ["Hon. Kofi Busia"] },
  { district: "Ahanta West Municipal", name: "John Agyare", type: "Municipal", region: "Western", role: "Municipal Chief Executive, Ahanta West", status: "active", aliases: ["John Agyare"] },
  { district: "Effia-Kwesimintsim Municipal", name: "Kojo Acquah", type: "Municipal", region: "Western", role: "Municipal Chief Executive, EKMA", status: "active", aliases: ["Kojo Acquah"] },
  { district: "Ellembelle District", name: "Kwasi Bonzoh", type: "District", region: "Western", role: "District Chief Executive, Ellembelle District", status: "active", aliases: ["Bonzoh Kwasi"] },
  { district: "Jomoro Municipal", name: "Louisa Iris Arde", type: "Municipal", region: "Western", role: "Municipal Chief Executive, Jomoro Municipal", status: "active", aliases: ["Iris Arde"] },
  { district: "Mpohor District", name: "Ignatius Asaah Mensah", type: "District", region: "Western", role: "District Chief Executive, Mpohor District", status: "active", aliases: ["Asaah Mensah"] },
  { district: "Nzema East Municipal", name: "Dorcas Elizabeth Amoah", type: "Municipal", region: "Western", role: "Municipal Chief Executive, Nzema East (Axim)", status: "active", aliases: ["Dorcas Amoah"] },
  { district: "Prestea Huni Valley Municipal", name: "Isaac Dasmani", type: "Municipal", region: "Western", role: "Municipal Chief Executive, Prestea Huni Valley", status: "active", aliases: ["Dr. Isaac Dasmani"] },
  { district: "Shama District", name: "Ebenezer Dadzie", type: "District", region: "Western", role: "District Chief Executive, Shama District", status: "active", aliases: ["Ebenezer Dadzie"] },
  { district: "Tarkwa Nsuaem Municipal", name: "Benjamin Kessie", type: "Municipal", region: "Western", role: "Municipal Chief Executive, Tarkwa Nsuaem", status: "active", aliases: ["Benjamin Kessie"] },
  { district: "Wassa Amenfi Central District", name: "Patrick Hockson Amponteng", type: "District", region: "Western", role: "District Chief Executive, Wassa Amenfi Central", status: "active", aliases: ["Patrick Amponteng"] },
  { district: "Wassa Amenfi East Municipal", name: "Frederick Korankye", type: "Municipal", region: "Western", role: "Municipal Chief Executive, Wassa Amenfi East", status: "active", aliases: ["Frederick Korankye"] },
  { district: "Wassa Amenfi West Municipal", name: "Lord Nana Tandoh", type: "Municipal", region: "Western", role: "Municipal Chief Executive, Wassa Amenfi West", status: "active", aliases: ["Nana Tandoh"] },
  { district: "Wassa East District", name: "Emmanuel Boakye", type: "District", region: "Western", role: "District Chief Executive, Wassa East", status: "active", aliases: ["Emmanuel Boakye"] },

  // --- WESTERN NORTH (9 MMDAs) ---
  { district: "Sefwi Wiawso Municipal", name: "Louis Owusu Agyapong", type: "Municipal", region: "Western North", role: "Municipal Chief Executive, Sefwi Wiawso", status: "active", aliases: ["Owusu Agyapong"] },
  { district: "Bibiani Anhwiaso Bekwai Municipal", name: "Alfred Amoah", type: "Municipal", region: "Western North", role: "Former Municipal Chief Executive, Bibiani Anhwiaso Bekwai", status: "cooling_off", aliases: ["Alfred Amoah"] },
  { district: "Bibiani Anhwiaso Bekwai Municipal", name: "Paul Andoh", type: "Municipal", region: "Western North", role: "Municipal Chief Executive, Bibiani Anhwiaso Bekwai", status: "active", aliases: ["Paul Andoh"] },
  { district: "Aowin Municipal", name: "Samuel Adu Gyamfi", type: "Municipal", region: "Western North", role: "Municipal Chief Executive, Aowin Municipal", status: "active", aliases: ["Adu Gyamfi"] },
  { district: "Bia East District", name: "Nicholas Niber", type: "District", region: "Western North", role: "District Chief Executive, Bia East", status: "active", aliases: ["Nicholas Niber"] },
  { district: "Bia West District", name: "Bernard Blay", type: "District", region: "Western North", role: "District Chief Executive, Bia West", status: "active", aliases: ["Bernard Blay"] },
  { district: "Bodi District", name: "Ignatius Akwasi Amankwah", type: "District", region: "Western North", role: "District Chief Executive, Bodi District", status: "active", aliases: ["Akwasi Amankwah"] },
  { district: "Juaboso District", name: "Godfred Kwabena Agyei Asante", type: "District", region: "Western North", role: "District Chief Executive, Juaboso District", status: "active", aliases: ["Kwabena Agyei Asante"] },
  { district: "Sefwi Akontombra District", name: "Yawson Amoah", type: "District", region: "Western North", role: "District Chief Executive, Sefwi Akontombra", status: "active", aliases: ["Yawson Amoah"] },
  { district: "Suaman District", name: "Philip Kwabena Boahen", type: "District", region: "Western North", role: "District Chief Executive, Suaman District", status: "active", aliases: ["Kwabena Boahen"] },

  // --- VOLTA REGION (18 MMDAs) ---
  { district: "Ho Municipal", name: "Dr. Divine Richard Bosson", type: "Municipal", region: "Volta", role: "Municipal Chief Executive, Ho Municipal Assembly", status: "active", aliases: ["Divine Bosson", "Hon. Divine Bosson"] },
  { district: "Adaklu District", name: "Phanuel Kadey Donkor", type: "District", region: "Volta", role: "Former District Chief Executive, Adaklu", status: "cooling_off", aliases: ["Kadey Donkor"] },
  { district: "Adaklu District", name: "Walter Wogbemase Zu", type: "District", region: "Volta", role: "District Chief Executive, Adaklu District", status: "active", aliases: ["Walter Zu"] },
  { district: "Afadzato South District", name: "Etornam Flolu", type: "District", region: "Volta", role: "District Chief Executive, Afadzato South", status: "active", aliases: ["James Etornam Flolu"] },
  { district: "Agotime Ziope District", name: "Emelia Emefa Adziamah", type: "District", region: "Volta", role: "District Chief Executive, Agotime Ziope", status: "active", aliases: ["Emefa Adziamah"] },
  { district: "Akatsi North District", name: "Peter Simon Ofosu", type: "District", region: "Volta", role: "District Chief Executive, Akatsi North", status: "active", aliases: ["Simon Ofosu"] },
  { district: "Akatsi South District", name: "Kofitsey Martin Nyahe", type: "District", region: "Volta", role: "District Chief Executive, Akatsi South", status: "active", aliases: ["Martin Nyahe"] },
  { district: "Anloga District", name: "Seth Yormewu", type: "District", region: "Volta", role: "District Chief Executive, Anloga District", status: "active", aliases: ["Seth Yormewu"] },
  { district: "Central Tongu District", name: "Thomas Moore Zonyrah", type: "District", region: "Volta", role: "Former District Chief Executive, Central Tongu", status: "cooling_off", aliases: ["Moore Zonyrah"] },
  { district: "Central Tongu District", name: "Addison Dodzi Mornyuie", type: "District", region: "Volta", role: "District Chief Executive, Central Tongu", status: "active", aliases: ["Dodzi Mornyuie"] },
  { district: "Ho West District", name: "Ernest Victor Apau", type: "District", region: "Volta", role: "District Chief Executive, Ho West", status: "active", aliases: ["Victor Apau"] },
  { district: "Hohoe Municipal", name: "Daniel Noble Awume", type: "Municipal", region: "Volta", role: "Municipal Chief Executive, Hohoe Municipal", status: "active", aliases: ["Noble Awume"] },
  { district: "Keta Municipal", name: "Emmanuel Gemegah", type: "Municipal", region: "Volta", role: "Municipal Chief Executive, Keta Municipal", status: "active", aliases: ["Emmanuel Gemegah"] },
  { district: "Ketu North Municipal", name: "Anthony Avorgbedor", type: "Municipal", region: "Volta", role: "Municipal Chief Executive, Ketu North", status: "active", aliases: ["Anthony Avorgbedor"] },
  { district: "Ketu South Municipal", name: "Maxwell Koffie Lugudor", type: "Municipal", region: "Volta", role: "Municipal Chief Executive, Ketu South (Aflao)", status: "active", aliases: ["Koffie Lugudor"] },
  { district: "Kpando Municipal", name: "Geoffrey Kodzo Badasu", type: "Municipal", region: "Volta", role: "Municipal Chief Executive, Kpando Municipal", status: "active", aliases: ["Geoffrey Badasu"] },
  { district: "North Dayi District", name: "Edmond Kudjo Attah", type: "District", region: "Volta", role: "District Chief Executive, North Dayi", status: "active", aliases: ["Kudjo Attah"] },
  { district: "North Tongu District", name: "Divine Osborn Fenu", type: "District", region: "Volta", role: "District Chief Executive, North Tongu", status: "active", aliases: ["Osborn Fenu"] },
  { district: "South Dayi District", name: "Godwin Kwame Dadzawa", type: "District", region: "Volta", role: "District Chief Executive, South Dayi", status: "active", aliases: ["Kwame Dadzawa"] },
  { district: "South Tongu District", name: "Seth Kwasi Agbi", type: "District", region: "Volta", role: "District Chief Executive, South Tongu (Sogakope)", status: "active", aliases: ["Kwasi Agbi"] },

  // --- OTI REGION (9 MMDAs) ---
  { district: "Krachi East Municipal", name: "Francis Kofi Okesu", type: "Municipal", region: "Oti", role: "Former Municipal Chief Executive, Krachi East (Dambai)", status: "cooling_off", aliases: ["Francis Okesu"] },
  { district: "Krachi East Municipal", name: "Charles Gyamfi Boateng", type: "Municipal", region: "Oti", role: "Municipal Chief Executive, Krachi East (Dambai)", status: "active", aliases: ["Charles Boateng"] },
  { district: "Krachi West Municipal", name: "Emmanuel Jalulah", type: "Municipal", region: "Oti", role: "Former Municipal Chief Executive, Krachi West", status: "cooling_off", aliases: ["Emmanuel Jalulah"] },
  { district: "Krachi West Municipal", name: "Prosper Kwaku Addo", type: "Municipal", region: "Oti", role: "Municipal Chief Executive, Krachi West", status: "active", aliases: ["Prosper Addo"] },
  { district: "Krachi Nchumuru District", name: "Nkrumah Ogyile", type: "District", region: "Oti", role: "District Chief Executive, Krachi Nchumuru", status: "active", aliases: ["Nkrumah Ogyile"] },
  { district: "Biakoye District", name: "Millicent Kabuki Carboo", type: "District", region: "Oti", role: "District Chief Executive, Biakoye District", status: "active", aliases: ["Kabuki Carboo"] },
  { district: "Guan District", name: "Marlon Anipa", type: "District", region: "Oti", role: "Former District Chief Executive, Guan District", status: "cooling_off", aliases: ["Marlon Anipa"] },
  { district: "Guan District", name: "Godfred Koku Kofie", type: "District", region: "Oti", role: "District Chief Executive, Guan District", status: "active", aliases: ["Koku Kofie"] },
  { district: "Jasikan Municipal", name: "Elizabeth Kessewah Anim", type: "Municipal", region: "Oti", role: "Municipal Chief Executive, Jasikan Municipal", status: "active", aliases: ["Kessewah Anim"] },
  { district: "Kadjebi District", name: "Wilson Agbanyo", type: "District", region: "Oti", role: "District Chief Executive, Kadjebi District", status: "active", aliases: ["Wilson Agbanyo"] },
  { district: "Nkwanta North District", name: "Jackson Jakayi", type: "District", region: "Oti", role: "District Chief Executive, Nkwanta North", status: "active", aliases: ["Jackson Jakayi"] },
  { district: "Nkwanta South Municipal", name: "Bright Lenwah", type: "Municipal", region: "Oti", role: "Former Municipal Chief Executive, Nkwanta South", status: "cooling_off", aliases: ["Bright Lenwah"] },
  { district: "Nkwanta South Municipal", name: "Joseph Antwi", type: "Municipal", region: "Oti", role: "Municipal Chief Executive, Nkwanta South", status: "active", aliases: ["Antwi Joseph"] },

  // --- NORTHERN REGION (16 MMDAs) ---
  { district: "Tamale Metropolitan", name: "Sule Salifu", type: "Metropolitan", region: "Northern", role: "Former Metropolitan Chief Executive (Mayor), TaMA", status: "cooling_off", aliases: ["Mayor of Tamale", "Hon. Sule Salifu"] },
  { district: "Tamale Metropolitan", name: "Alhaji Farouk Aliu Mahama", type: "Metropolitan", region: "Northern", role: "Metropolitan Chief Executive, Tamale Metropolitan", status: "active", aliases: ["Farouk Aliu Mahama"] },
  { district: "Gushegu Municipal", name: "Yaja Dawuni Robert", type: "Municipal", region: "Northern", role: "Municipal Chief Executive, Gushegu Municipal", status: "active", aliases: ["Dawuni Robert"] },
  { district: "Karaga District", name: "Iddrisu Mohammed Saani", type: "District", region: "Northern", role: "District Chief Executive, Karaga District", status: "active", aliases: ["Saani Mohammed"] },
  { district: "Kpandai District", name: "Emmanuel Attah Tatablata", type: "District", region: "Northern", role: "District Chief Executive, Kpandai District", status: "active", aliases: ["Attah Tatablata"] },
  { district: "Kumbungu District", name: "Hamza Abdul-Salam", type: "District", region: "Northern", role: "District Chief Executive, Kumbungu District", status: "active", aliases: ["Abdul-Salam Hamza"] },
  { district: "Mion District", name: "Mohammed Hashim Abdallah", type: "District", region: "Northern", role: "District Chief Executive, Mion District", status: "active", aliases: ["Hashim Abdallah"] },
  { district: "Nanton District", name: "Abdul-Rahaman Omar", type: "District", region: "Northern", role: "District Chief Executive, Nanton District", status: "active", aliases: ["Omar Abdul-Rahaman"] },
  { district: "Nanumba North Municipal", name: "Abdulai Yaqoub", type: "Municipal", region: "Northern", role: "Municipal Chief Executive, Nanumba North (Bimbilla)", status: "active", aliases: ["Abdulai Yaqoub"] },
  { district: "Nanumba South District", name: "Issifu Zakaria", type: "District", region: "Northern", role: "District Chief Executive, Nanumba South (Wulensi)", status: "active", aliases: ["Issifu Zakaria"] },
  { district: "Saboba District", name: "George K. Bingrini", type: "District", region: "Northern", role: "District Chief Executive, Saboba District", status: "active", aliases: ["George Bingrini"] },
  { district: "Sagnarigu Municipal", name: "Mohammed Ahmed Yakubu", type: "Municipal", region: "Northern", role: "Municipal Chief Executive, Sagnarigu Municipal", status: "active", aliases: ["Ahmed Yakubu"] },
  { district: "Savelugu Municipal", name: "Hajia Ayishetu Seidu", type: "Municipal", region: "Northern", role: "Municipal Chief Executive, Savelugu Municipal", status: "active", aliases: ["Hajia Ayishetu"] },
  { district: "Tatale Sanguli District", name: "Thomas Mbomba", type: "District", region: "Northern", role: "District Chief Executive, Tatale Sanguli", status: "active", aliases: ["Thomas Mbomba"] },
  { district: "Tolon District", name: "Seidu Braimah", type: "District", region: "Northern", role: "District Chief Executive, Tolon District", status: "active", aliases: ["Seidu Braimah"] },
  { district: "Yendi Municipal", name: "Alhaji Hammed Abubakari Yussuf", type: "Municipal", region: "Northern", role: "Municipal Chief Executive, Yendi Municipal", status: "active", aliases: ["Hammed Yussuf"] },
  { district: "Zabzugu District", name: "Ahmed Abukari Iddrisu", type: "District", region: "Northern", role: "District Chief Executive, Zabzugu District", status: "active", aliases: ["Abukari Iddrisu"] },

  // --- NORTH EAST REGION (6 MMDAs) ---
  { district: "Bunkpurugu Nakpanduri District", name: "Joseph Louknaan", type: "District", region: "North East", role: "District Chief Executive, Bunkpurugu Nakpanduri", status: "active", aliases: ["Joseph Louknaan"] },
  { district: "Chereponi District", name: "Hajia Zuweiratu Mada Nashiru", type: "District", region: "North East", role: "District Chief Executive, Chereponi District", status: "active", aliases: ["Hajia Zuweiratu"] },
  { district: "East Mamprusi Municipal", name: "Rashid Salifu", type: "Municipal", region: "North East", role: "Municipal Chief Executive, East Mamprusi (Nalerigu)", status: "active", aliases: ["Rashid Salifu"] },
  { district: "Mamprugu Moagduri District", name: "Abu Adam", type: "District", region: "North East", role: "District Chief Executive, Mamprugu Moagduri", status: "active", aliases: ["Abu Adam"] },
  { district: "West Mamprusi Municipal", name: "Aremeyaw Somo Lucky", type: "Municipal", region: "North East", role: "Municipal Chief Executive, West Mamprusi (Walewale)", status: "active", aliases: ["Somo Lucky"] },
  { district: "Yunyoo-Nasuan District", name: "Konlan Bitian", type: "District", region: "North East", role: "District Chief Executive, Yunyoo-Nasuan", status: "active", aliases: ["Konlan Bitian"] },

  // --- SAVANNAH REGION (7 MMDAs) ---
  { district: "Bole District", name: "Veronica Alele Heming", type: "District", region: "Savannah", role: "Former District Chief Executive, Bole District", status: "cooling_off", aliases: ["Veronica Heming"] },
  { district: "Bole District", name: "Abdulai Mahmud", type: "District", region: "Savannah", role: "District Chief Executive, Bole Bamboi District", status: "active", aliases: ["Hon. Abdulai Mahmud"] },
  { district: "Central Gonja District", name: "Salia Kamara", type: "District", region: "Savannah", role: "District Chief Executive, Central Gonja (Buipe)", status: "active", aliases: ["Salia Kamara"] },
  { district: "East Gonja Municipal", name: "Mohammed Tamimu", type: "Municipal", region: "Savannah", role: "Municipal Chief Executive, East Gonja (Salaga)", status: "active", aliases: ["Mohammed Tamimu"] },
  { district: "North East Gonja District", name: "Mohammed Musah Tindawu", type: "District", region: "Savannah", role: "District Chief Executive, North East Gonja", status: "active", aliases: ["Musah Tindawu"] },
  { district: "North Gonja District", name: "Adam Eliasu", type: "District", region: "Savannah", role: "District Chief Executive, North Gonja (Daboya)", status: "active", aliases: ["Adam Eliasu"] },
  { district: "Sawla-Tuna-Kalba District", name: "Barikisu Losina", type: "District", region: "Savannah", role: "District Chief Executive, Sawla-Tuna-Kalba", status: "active", aliases: ["Barikisu Losina"] },
  { district: "West Gonja Municipal", name: "Musah Karim Kusubari", type: "Municipal", region: "Savannah", role: "Municipal Chief Executive, West Gonja (Damongo)", status: "active", aliases: ["Karim Kusubari"] },

  // --- UPPER EAST REGION (15 MMDAs) ---
  { district: "Bolgatanga Municipal", name: "Rex Asanga", type: "Municipal", region: "Upper East", role: "Municipal Chief Executive, Bolgatanga Municipal", status: "active", aliases: ["Hon. Rex Asanga"] },
  { district: "Bawku Municipal", name: "Amadu Hamza", type: "Municipal", region: "Upper East", role: "Municipal Chief Executive, Bawku Municipal", status: "active", aliases: ["Amadu Hamza"] },
  { district: "Bawku West District", name: "Tahiru Issahaku Ahmed", type: "District", region: "Upper East", role: "District Chief Executive, Bawku West (Zebilla)", status: "active", aliases: ["Issahaku Ahmed"] },
  { district: "Binduri District", name: "Ayinga Abagre Yakubu", type: "District", region: "Upper East", role: "District Chief Executive, Binduri District", status: "active", aliases: ["Abagre Yakubu"] },
  { district: "Bolgatanga East District", name: "David Amoah", type: "District", region: "Upper East", role: "District Chief Executive, Bolgatanga East (Zuarungu)", status: "active", aliases: ["David Amoah"] },
  { district: "Bongo District", name: "Rita Atanga", type: "District", region: "Upper East", role: "District Chief Executive, Bongo District", status: "active", aliases: ["Rita Atanga"] },
  { district: "Builsa North Municipal", name: "David Afoko", type: "Municipal", region: "Upper East", role: "Municipal Chief Executive, Builsa North (Sandema)", status: "active", aliases: ["David Afoko"] },
  { district: "Builsa South District", name: "Daniel Kwame Gariba", type: "District", region: "Upper East", role: "District Chief Executive, Builsa South (Fumbisi)", status: "active", aliases: ["Daniel Gariba"] },
  { district: "Garu District", name: "Osman Musah", type: "District", region: "Upper East", role: "District Chief Executive, Garu District", status: "active", aliases: ["Osman Musah"] },
  { district: "Kassena Nankana Municipal", name: "Joseph Adongo", type: "Municipal", region: "Upper East", role: "Municipal Chief Executive, Kassena Nankana (Navrongo)", status: "active", aliases: ["Joseph Adongo"] },
  { district: "Kassena Nankana West District", name: "Gerard Ataogye", type: "District", region: "Upper East", role: "District Chief Executive, Kassena Nankana West (Paga)", status: "active", aliases: ["Gerard Ataogye"] },
  { district: "Nabdam District", name: "Agnes Anamoo", type: "District", region: "Upper East", role: "District Chief Executive, Nabdam District", status: "active", aliases: ["Agnes Anamoo"] },
  { district: "Pusiga District", name: "Zubeiru Abdulai", type: "District", region: "Upper East", role: "District Chief Executive, Pusiga District", status: "active", aliases: ["Zubeiru Abdulai"] },
  { district: "Talensi District", name: "Thomas Wuni Pearson Duanab", type: "District", region: "Upper East", role: "Former District Chief Executive, Talensi District", status: "cooling_off", aliases: ["Wuni Duanab"] },
  { district: "Talensi District", name: "John M. Naabwomya", type: "District", region: "Upper East", role: "District Chief Executive, Talensi District", status: "active", aliases: ["John Naabwomya"] },
  { district: "Tempane District", name: "Paul Azumah Abugre", type: "District", region: "Upper East", role: "District Chief Executive, Tempane District", status: "active", aliases: ["Paul Abugre"] },

  // --- UPPER WEST REGION (11 MMDAs) ---
  { district: "Wa Municipal", name: "Tahir Zubairu", type: "Municipal", region: "Upper West", role: "Municipal Chief Executive, Wa Municipal", status: "active", aliases: ["Hon. Tahir Zubairu", "Alhaji Tahir Zubairu"] },
  { district: "Daffiama Bussie Issa District", name: "Nadi Imoro Sanda", type: "District", region: "Upper West", role: "District Chief Executive, Daffiama Bussie Issa", status: "active", aliases: ["Imoro Sanda"] },
  { district: "Jirapa Municipal", name: "Nicholas Soyiri", type: "Municipal", region: "Upper West", role: "Municipal Chief Executive, Jirapa Municipal", status: "active", aliases: ["Nicholas Soyiri"] },
  { district: "Lambussie Karni District", name: "George Gerard Naluri", type: "District", region: "Upper West", role: "District Chief Executive, Lambussie Karni", status: "active", aliases: ["Gerard Naluri"] },
  { district: "Lawra Municipal", name: "Jacob Dery", type: "Municipal", region: "Upper West", role: "Municipal Chief Executive, Lawra Municipal", status: "active", aliases: ["Jacob Dery"] },
  { district: "Nadowli Kaleo District", name: "Katherine Lankono", type: "District", region: "Upper West", role: "District Chief Executive, Nadowli Kaleo", status: "active", aliases: ["Katherine Lankono"] },
  { district: "Nandom Municipal", name: "Raymond Nero", type: "Municipal", region: "Upper West", role: "Municipal Chief Executive, Nandom Municipal", status: "active", aliases: ["Raymond Nero"] },
  { district: "Sissala East Municipal", name: "Fuseini Yakubu Batong", type: "Municipal", region: "Upper West", role: "Municipal Chief Executive, Sissala East (Tumu)", status: "active", aliases: ["Yakubu Batong"] },
  { district: "Sissala West District", name: "Ayisha Batong Hor", type: "District", region: "Upper West", role: "District Chief Executive, Sissala West (Gwollu)", status: "active", aliases: ["Ayisha Batong"] },
  { district: "Wa East District", name: "Ewuntomah Mahama", type: "District", region: "Upper West", role: "District Chief Executive, Wa East", status: "active", aliases: ["Ewuntomah Mahama"] },
  { district: "Wa West District", name: "Vida Diorotey", type: "District", region: "Upper West", role: "District Chief Executive, Wa West (Wechiau)", status: "active", aliases: ["Vida Diorotey"] },

  // --- BONO REGION (12 MMDAs) ---
  { district: "Sunyani Municipal", name: "Ansu Kumi", type: "Municipal", region: "Bono", role: "Municipal Chief Executive, Sunyani Municipal Assembly", status: "active", aliases: ["Hon. Ansu Kumi"] },
  { district: "Banda District", name: "Peter Dongi", type: "District", region: "Bono", role: "District Chief Executive, Banda District", status: "active", aliases: ["Hon. Peter Dongi"] },
  { district: "Berekum East Municipal", name: "Kofi Adjei", type: "Municipal", region: "Bono", role: "Municipal Chief Executive, Berekum East", status: "active", aliases: ["Kofi Adjei"] },
  { district: "Berekum West District", name: "Dominic Oppong", type: "District", region: "Bono", role: "District Chief Executive, Berekum West (Jinijini)", status: "active", aliases: ["Dominic Oppong"] },
  { district: "Dormaa Central Municipal", name: "Iddrissa Quattara", type: "Municipal", region: "Bono", role: "Former Municipal Chief Executive, Dormaa Central", status: "cooling_off", aliases: ["Iddrissa Quattara"] },
  { district: "Dormaa Central Municipal", name: "Dominic Adomah Ameyaw", type: "Municipal", region: "Bono", role: "Municipal Chief Executive, Dormaa Central", status: "active", aliases: ["Dominic Ameyaw"] },
  { district: "Dormaa East District", name: "Emmanuel Kofi Agyeman", type: "District", region: "Bono", role: "District Chief Executive, Dormaa East (Wamfie)", status: "active", aliases: ["Kofi Agyeman"] },
  { district: "Dormaa West District", name: "Francis Kwadwo Oppong", type: "District", region: "Bono", role: "District Chief Executive, Dormaa West (Nkrankwanta)", status: "active", aliases: ["Kwadwo Oppong"] },
  { district: "Jaman North District", name: "Solomon Owusu", type: "District", region: "Bono", role: "District Chief Executive, Jaman North (Sampa)", status: "active", aliases: ["Solomon Owusu"] },
  { district: "Jaman South Municipal", name: "Andrews Bediako", type: "Municipal", region: "Bono", role: "Municipal Chief Executive, Jaman South (Drobo)", status: "active", aliases: ["Andrews Bediako"] },
  { district: "Sunyani West Municipal", name: "Evans Kusi Boadum", type: "Municipal", region: "Bono", role: "Municipal Chief Executive, Sunyani West (Odumase)", status: "active", aliases: ["Kusi Boadum"] },
  { district: "Tain District", name: "Lucy Acheampong", type: "District", region: "Bono", role: "District Chief Executive, Tain District (Nsawkaw)", status: "active", aliases: ["Lucy Acheampong"] },
  { district: "Wenchi Municipal", name: "Alexander Obour Damoah", type: "Municipal", region: "Bono", role: "Municipal Chief Executive, Wenchi Municipal", status: "active", aliases: ["Obour Damoah"] },

  // --- BONO EAST REGION (11 MMDAs) ---
  { district: "Techiman Municipal", name: "Benjamin Yaw Gyarko", type: "Municipal", region: "Bono East", role: "Municipal Chief Executive, Techiman Municipal", status: "active", aliases: ["Yaw Gyarko"] },
  { district: "Atebubu-Amantin Municipal", name: "Edward Owusu", type: "Municipal", region: "Bono East", role: "Former Municipal Chief Executive, Atebubu-Amantin", status: "cooling_off", aliases: ["Edward Owusu"] },
  { district: "Atebubu-Amantin Municipal", name: "Thomas Masud Musah Ayaaba", type: "Municipal", region: "Bono East", role: "Municipal Chief Executive, Atebubu-Amantin", status: "active", aliases: ["Thomas Musah Ayaaba"] },
  { district: "Kintampo North Municipal", name: "Isaac Baffoe Ameyaw", type: "Municipal", region: "Bono East", role: "Municipal Chief Executive, Kintampo North", status: "active", aliases: ["Baffoe Ameyaw"] },
  { district: "Kintampo South District", name: "Ophelia Amoah", type: "District", region: "Bono East", role: "District Chief Executive, Kintampo South (Jema)", status: "active", aliases: ["Ophelia Amoah"] },
  { district: "Nkoranza North District", name: "Peter Kwabena Osei", type: "District", region: "Bono East", role: "District Chief Executive, Nkoranza North (Busunya)", status: "active", aliases: ["Kwabena Osei"] },
  { district: "Nkoranza South Municipal", name: "Daniel Owusu Amponsah", type: "Municipal", region: "Bono East", role: "Municipal Chief Executive, Nkoranza South", status: "active", aliases: ["Owusu Amponsah"] },
  { district: "Pru East District", name: "Alhaji Abdul-Gafaru Mohammed", type: "District", region: "Bono East", role: "District Chief Executive, Pru East (Yeji)", status: "active", aliases: ["Abdul-Gafaru Mohammed"] },
  { district: "Pru West District", name: "David Manu", type: "District", region: "Bono East", role: "District Chief Executive, Pru West (Prang)", status: "active", aliases: ["David Manu"] },
  { district: "Sene East District", name: "Kofi Gyimah", type: "District", region: "Bono East", role: "District Chief Executive, Sene East (Kajaji)", status: "active", aliases: ["Kofi Gyimah"] },
  { district: "Sene West District", name: "John Kwadwo Nicholas Kwanyarko", type: "District", region: "Bono East", role: "District Chief Executive, Sene West (Kwame Danso)", status: "active", aliases: ["Nicholas Kwanyarko"] },
  { district: "Techiman North District", name: "Everson Addo-Donkor", type: "District", region: "Bono East", role: "District Chief Executive, Techiman North (Tuobodom)", status: "active", aliases: ["Addo-Donkor"] },

  // --- AHAFO REGION (6 MMDAs) ---
  { district: "Asunafo North Municipal", name: "Osei Yaw Boahen", type: "Municipal", region: "Ahafo", role: "Municipal Chief Executive, Asunafo North (Goaso)", status: "active", aliases: ["Yaw Boahen"] },
  { district: "Asunafo South District", name: "Frank Aduse Poku", type: "District", region: "Ahafo", role: "District Chief Executive, Asunafo South (Kukuom)", status: "active", aliases: ["Aduse Poku"] },
  { district: "Asutifi North District", name: "Anthony Mensah", type: "District", region: "Ahafo", role: "District Chief Executive, Asutifi North (Kenyasi)", status: "active", aliases: ["Anthony Mensah"] },
  { district: "Asutifi South District", name: "Robert Dwomoh Mensah", type: "District", region: "Ahafo", role: "District Chief Executive, Asutifi South (Hwidiem)", status: "active", aliases: ["Dwomoh Mensah"] },
  { district: "Tano North Municipal", name: "Ernest Kwarteng", type: "Municipal", region: "Ahafo", role: "Former Municipal Chief Executive, Tano North", status: "cooling_off", aliases: ["Ernest Kwarteng"] },
  { district: "Tano North Municipal", name: "Sampson Opoku Plus", type: "Municipal", region: "Ahafo", role: "Municipal Chief Executive, Tano North (Duayaw Nkwanta)", status: "active", aliases: ["Opoku Plus Sampson"] },
  { district: "Tano South Municipal", name: "Collins Offinam Takyi", type: "Municipal", region: "Ahafo", role: "Municipal Chief Executive, Tano South (Bechem)", status: "active", aliases: ["Offinam Takyi"] }
];

console.log(`Loaded ${regionalMinisters.length} Active Regional Ministers.`);
console.log(`Loaded ${formerRegionalMinisters.length} Former Regional Ministers.`);
console.log(`Loaded ${rawMmdces.length} MMDCE records.`);

// Build final list of PEP items with deterministic unique IDs
const allSubnational = [];

// 1. Regional Ministers
regionalMinisters.forEach(rm => {
  allSubnational.push({
    id: rm.id,
    name: rm.name,
    aliases: enrichAliases(rm.name),
    region: rm.region,
    role: rm.role,
    status: rm.status,
    term: rm.term,
    ref: "https://presidency.gov.gh"
  });
});

formerRegionalMinisters.forEach(frm => {
  allSubnational.push({
    id: frm.id,
    name: frm.name,
    aliases: enrichAliases(frm.name),
    region: frm.region,
    role: frm.role,
    status: frm.status,
    term: frm.term,
    ref: "https://presidency.gov.gh"
  });
});

// 2. MMDCEs
let mceCount = 1;
rawMmdces.forEach(m => {
  const paddedId = String(mceCount++).padStart(3, '0');
  allSubnational.push({
    id: `gh-pep-mmdce-${paddedId}`,
    name: m.name,
    aliases: enrichAliases(m.name, m.aliases || []),
    region: m.region,
    role: m.role,
    status: m.status,
    term: m.status === 'cooling_off' ? "2021-09-20 to 2025-01-10" : "2025-01-28 to present",
    ref: "https://ghanadistricts.com"
  });
});

console.log(`Total Compiled Sub-National PEPs: ${allSubnational.length}`);

// Convert to canonical sourcedata.Record format
const records = allSubnational.map(p => {
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
    aliases: p.aliases,
    source_reference: p.ref || "https://ghanadistricts.com",
    source_type: "pep_evidence",
    properties
  };
});

const finalFixture = {
  version: 2,
  source: "gh-pep-local-gov",
  parser: "gh-pep-v1",
  records
};

fs.writeFileSync("gh_pep_local_gov.json", JSON.stringify(finalFixture, null, 2), "utf8");
console.log(`SUCCESS! Saved gh_pep_local_gov.json with ${finalFixture.records.length} records.`);
