import fs from 'node:fs';

// ============================================================================
// 1. GHANA EXECUTIVE PEPS (Current Administration vs Former Office Holders)
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
    role: "Senior Presidential Advisor, Former Campaign Coordinator, Former Vice Chancellor UPSA",
    term: "2025-01-07 to present",
    status: "active",
    familyMembers: ["Prof. Goski Alabi (Spouse)"],
    associates: ["President John Dramani Mahama (President)"],
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
    role: "Senior Diplomatic Advisor, Special Representative of the UN Secretary-General, Former Minister for Foreign Affairs",
    term: "2025-01-07 to present",
    status: "active",
    ref: "https://mfa.gov.gh"
  },
  {
    id: "gh-pep-exec-013",
    name: "Edward Omane Boamah",
    aliases: ["Dr. Edward Omane Boamah", "Omane Boamah"],
    role: "Director of Elections & IT (NDC), Senior Advisor on Health and Communications",
    term: "2023-08-24 to present",
    status: "active",
    ref: "https://presidency.gov.gh"
  },

  // --- Former Executive Leadership (Cooling-off Period under FATF & Act 1044 AML Rules) ---
  {
    id: "gh-pep-exec-014",
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
    id: "gh-pep-exec-015",
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
    id: "gh-pep-exec-016",
    name: "Akosua Frema Osei-Opare",
    aliases: ["Frema Osei-Opare"],
    role: "Former Chief of Staff at the Presidency (2017–2025)",
    term: "2017-01-07 to 2025-01-06",
    status: "cooling_off",
    associates: ["Nana Addo Dankwa Akufo-Addo (Former President)"],
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-exec-017",
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
    id: "gh-pep-exec-018",
    name: "Mohammed Amin Adam",
    aliases: ["Dr. Mohammed Amin Adam", "Anta"],
    role: "Former Minister for Finance, Former Minister of State at Finance (2024–2025)",
    term: "2024-02-14 to 2025-01-06",
    status: "cooling_off",
    ref: "https://mofep.gov.gh"
  },
  {
    id: "gh-pep-exec-019",
    name: "Godfred Yeboah Dame",
    aliases: ["Godfred Dame"],
    role: "Former Attorney-General and Minister for Justice (2021–2025)",
    term: "2021-01-21 to 2025-01-06",
    status: "cooling_off",
    ref: "https://mojagd.gov.gh"
  },
  {
    id: "gh-pep-exec-020",
    name: "Dominic Bingham Aduna Nitiwul",
    aliases: ["Dominic Nitiwul"],
    role: "Former Minister for Defence, Member of Parliament for Bimbilla (2017–2025)",
    term: "2017-01-27 to 2025-01-06",
    status: "cooling_off",
    ref: "https://gafonline.mil.gh"
  },
  {
    id: "gh-pep-exec-021",
    name: "Henry Quartey",
    aliases: ["Hon. Henry Quartey"],
    role: "Former Minister for the Interior, Former Greater Accra Regional Minister (2021–2025)",
    term: "2021-01-21 to 2025-01-06",
    status: "cooling_off",
    ref: "https://mint.gov.gh"
  },
  {
    id: "gh-pep-exec-022",
    name: "Shirley Ayorkor Botchwey",
    aliases: ["Ayorkor Botchwey"],
    role: "Commonwealth Secretary-General, Former Minister for Foreign Affairs (2017–2025)",
    term: "2017-01-27 to 2025-01-06",
    status: "cooling_off",
    ref: "https://mfa.gov.gh"
  },
  {
    id: "gh-pep-exec-023",
    name: "Samuel Abu Jinapor",
    aliases: ["Abu Jinapor"],
    role: "Former Minister for Lands and Natural Resources, MP for Damongo (2021–2025)",
    term: "2021-03-05 to 2025-01-06",
    status: "cooling_off",
    familyMembers: ["John Abdulai Jinapor (Brother / MP Yapei-Kusawgu)"],
    ref: "https://mlnr.gov.gh"
  },
  {
    id: "gh-pep-exec-024",
    name: "Matthew Opoku Prempeh",
    aliases: ["Dr. Matthew Opoku Prempeh", "Napo"],
    role: "Former Minister for Energy, Former Minister for Education (2017–2024)",
    term: "2017-01-27 to 2024-07-15",
    status: "cooling_off",
    ref: "https://energymin.gov.gh"
  },
  {
    id: "gh-pep-exec-025",
    name: "Ursula Owusu-Ekuful",
    aliases: ["Ursula Owusu"],
    role: "Former Minister for Communications and Digitalisation, Former MP for Ablekuma West (2017–2025)",
    term: "2017-01-27 to 2025-01-06",
    status: "cooling_off",
    familyMembers: ["Dr. Sam Ekuful (Spouse)"],
    ref: "https://moc.gov.gh"
  },
  {
    id: "gh-pep-exec-026",
    name: "Bryan Acheampong",
    aliases: ["Dr. Bryan Acheampong"],
    role: "Former Minister for Food and Agriculture, MP for Abetifi (2020–2025)",
    term: "2020-02-13 to 2025-01-06",
    status: "cooling_off",
    ref: "https://mofa.gov.gh"
  },
  {
    id: "gh-pep-exec-027",
    name: "Kojo Oppong Nkrumah",
    aliases: ["Oppong Nkrumah"],
    role: "Former Minister for Works and Housing, Former Minister for Information (2018–2025)",
    term: "2018-11-01 to 2025-01-06",
    status: "cooling_off",
    ref: "https://mwh.gov.gh"
  },
  {
    id: "gh-pep-exec-028",
    name: "Ambrose Dery",
    aliases: ["Ambrose Derry"],
    role: "Former Minister for the Interior, Former Minister of State at the Presidency (2017–2025)",
    term: "2017-01-27 to 2025-01-06",
    status: "cooling_off",
    ref: "https://mint.gov.gh"
  },
  {
    id: "gh-pep-exec-029",
    name: "Alan John Kwadwo Kyerematen",
    aliases: ["Alan Kyerematen", "Alan Cash"],
    role: "Leader of Movement for Change, Former Minister for Trade and Industry (2017–2023)",
    term: "2017-01-27 to 2023-01-06",
    status: "cooling_off",
    familyMembers: ["Patricia Christabel Kyerematen (Spouse)"],
    ref: "https://moti.gov.gh"
  },
  {
    id: "gh-pep-exec-030",
    name: "Francis Asenso-Boakye",
    aliases: ["Asenso-Boakye"],
    role: "Former Minister for Roads and Highways, Former Minister for Works and Housing (2021–2025)",
    term: "2021-03-05 to 2025-01-06",
    status: "cooling_off",
    ref: "https://mrh.gov.gh"
  },
  {
    id: "gh-pep-exec-031",
    name: "Cecilia Abena Dapaah",
    aliases: ["Cecilia Dapaah"],
    role: "Former Minister for Sanitation and Water Resources, Subject of OSP Anti-Corruption Investigation (2018–2023)",
    term: "2018-08-01 to 2023-07-22",
    status: "cooling_off",
    familyMembers: ["Daniel Osei Kuffour (Spouse)"],
    ref: "https://osp.gov.gh"
  }
];

// ============================================================================
// 2. GHANA PARLIAMENT PEPS (9th Parliament of the 4th Republic)
// ============================================================================
const parliamentPeps = [
  // --- Speaker & Parliamentary Officers of the 9th Parliament ---
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

  // --- Majority (NDC) Leadership & Whips of the 9th Parliament ---
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
    name: "Emmanuel Armah-Kofi Buah",
    aliases: ["Emmanuel Armah Kofi Buah", "Armah Kofi Buah"],
    role: "Deputy Majority Leader of Parliament, Member of Parliament for Ellembelle",
    constituency: "Ellembelle",
    term: "2023-01-24 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-006",
    name: "Kwame Governs Agbodza",
    aliases: ["Kwame Agbodza", "Governs Agbodza"],
    role: "Majority Chief Whip of Parliament, Member of Parliament for Adaklu",
    constituency: "Adaklu",
    term: "2023-01-24 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-007",
    name: "Ahmed Ibrahim",
    aliases: ["Hon. Ahmed Ibrahim"],
    role: "First Deputy Majority Whip, Member of Parliament for Banda",
    constituency: "Banda",
    term: "2013-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-008",
    name: "Comfort Doyoe Cudjoe-Ghansah",
    aliases: ["Comfort Doyoe", "Hon. Comfort Doyoe Cudjoe"],
    role: "Second Deputy Majority Whip, Member of Parliament for Ada",
    constituency: "Ada",
    term: "2013-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },

  // --- Minority (NPP) Leadership & Whips of the 9th Parliament ---
  {
    id: "gh-pep-parl-009",
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
    id: "gh-pep-parl-010",
    name: "Frank Annoh-Dompreh",
    aliases: ["Annoh-Dompreh", "Frank Annoh Dompreh"],
    role: "Deputy Minority Leader of Parliament, Member of Parliament for Nsawam-Adoagyiri",
    constituency: "Nsawam-Adoagyiri",
    term: "2021-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-011",
    name: "Lydia Seyram Alhassan",
    aliases: ["Lydia Alhassan", "Maa Lydia"],
    role: "Minority Chief Whip of Parliament, Member of Parliament for Ayawaso West Wuogon",
    constituency: "Ayawaso West Wuogon",
    term: "2024-02-23 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-012",
    name: "Vincent Ekow Assafuah",
    aliases: ["Vincent Assafuah"],
    role: "Deputy Minority Whip, Member of Parliament for Old Tafo",
    constituency: "Old Tafo",
    term: "2021-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-013",
    name: "Dakoa Newman",
    aliases: ["Hon. Dakoa Newman"],
    role: "Deputy Minority Whip, Member of Parliament for Okaikwei South",
    constituency: "Okaikwei South",
    term: "2021-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },

  // --- Active Members of Parliament (9th Parliament) ---
  {
    id: "gh-pep-parl-014",
    name: "Haruna Iddrisu",
    aliases: ["Hon. Haruna Iddrisu"],
    role: "Member of Parliament for Tamale South, Former Minority Leader",
    constituency: "Tamale South",
    term: "2005-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-015",
    name: "Samuel Okudzeto Ablakwa",
    aliases: ["Okudzeto Ablakwa", "Samuel Ablakwa"],
    role: "Member of Parliament for North Tongu, Chairman of Government Assurances Committee",
    constituency: "North Tongu",
    term: "2013-01-07 to present",
    status: "active",
    familyMembers: ["Nuhela Seidu (Spouse)"],
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-016",
    name: "Mohammed Mubarak Muntaka",
    aliases: ["Muntaka Mubarak", "Mubarak Muntaka"],
    role: "Member of Parliament for Asawase, Former Minority Chief Whip",
    constituency: "Asawase",
    term: "2005-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-017",
    name: "Zanetor Agyeman-Rawlings",
    aliases: ["Dr. Zanetor Rawlings", "Zanetor Rawlings", "Dr. Zanetor Agyeman-Rawlings"],
    role: "Member of Parliament for Klottey-Korle",
    constituency: "Klottey-Korle",
    term: "2017-01-07 to present",
    status: "active",
    familyMembers: ["Nana Konadu Agyeman-Rawlings (Mother / Former First Lady)", "Yaa Asantewaa Rawlings (Sister)", "Amina Rawlings (Sister)", "Kimathi Rawlings (Brother)"],
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-018",
    name: "James Gyakye Quayson",
    aliases: ["Gyakye Quayson", "James Quayson"],
    role: "Member of Parliament for Assin North",
    constituency: "Assin North",
    term: "2021-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-019",
    name: "Isaac Adongo",
    aliases: ["Hon. Isaac Adongo"],
    role: "Member of Parliament for Bolgatanga Central, Ranking Member on Finance Committee",
    constituency: "Bolgatanga Central",
    term: "2017-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-020",
    name: "John Abdulai Jinapor",
    aliases: ["John Jinapor"],
    role: "Member of Parliament for Yapei-Kusawgu, Chairman/Ranking Member Mines and Energy",
    constituency: "Yapei-Kusawgu",
    term: "2017-01-07 to present",
    status: "active",
    familyMembers: ["Samuel Abu Jinapor (Brother / Former Minister)"],
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-021",
    name: "Samuel Nartey George",
    aliases: ["Sam George", "Sam Nartey George"],
    role: "Member of Parliament for Ningo-Prampram, Ranking Member Communications",
    constituency: "Ningo-Prampram",
    term: "2017-01-07 to present",
    status: "active",
    familyMembers: ["Vera George (Spouse)"],
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-022",
    name: "Francis-Xavier Kojo Sosu",
    aliases: ["Francis-Xavier Sosu", "Xavier Sosu"],
    role: "Member of Parliament for Madina, Constitutional and Legal Affairs Committee",
    constituency: "Madina",
    term: "2021-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-023",
    name: "Alhassan Sayibu Suhuyini",
    aliases: ["Alhassan Suhuyini"],
    role: "Member of Parliament for Tamale North",
    constituency: "Tamale North",
    term: "2017-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-024",
    name: "Rockson-Nelson Dafeamekpor",
    aliases: ["Rockson Dafeamekpor", "Rockson-Nelson Etse Dafeamekpor"],
    role: "Member of Parliament for South Dayi, Judiciary & Constitutional Committee",
    constituency: "South Dayi",
    term: "2017-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-025",
    name: "Collins Dauda",
    aliases: ["Alhaji Collins Dauda"],
    role: "Member of Parliament for Asutifi South",
    constituency: "Asutifi South",
    term: "2009-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-026",
    name: "Eric Opoku",
    aliases: ["Hon. Eric Opoku"],
    role: "Member of Parliament for Asunafo South",
    constituency: "Asunafo South",
    term: "2013-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-027",
    name: "Peter Lanchene Toobu",
    aliases: ["Peter Toobu"],
    role: "Member of Parliament for Wa West, Defence and Interior Committee",
    constituency: "Wa West",
    term: "2021-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-028",
    name: "Gizella Tetteh-Agbotui",
    aliases: ["Gizella Tetteh"],
    role: "Member of Parliament for Awutu Senya West",
    constituency: "Awutu Senya West",
    term: "2021-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-029",
    name: "Farouk Aliu Mahama",
    aliases: ["Alhaji Farouk Aliu Mahama"],
    role: "Member of Parliament for Yendi, Former Board Chairman VIP Transport",
    constituency: "Yendi",
    term: "2021-01-07 to present",
    status: "active",
    familyMembers: ["Late Aliu Mahama (Father / Former Vice President)"],
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-030",
    name: "Patricia Appiagyei",
    aliases: ["Mama Pat", "Patricia Appiagyei"],
    role: "Member of Parliament for Asokwa",
    constituency: "Asokwa",
    term: "2013-01-07 to present",
    status: "active",
    familyMembers: ["Dr. K.K. Sarpong (Spouse / Former GNPC CEO)"],
    ref: "https://parliament.gh"
  },

  // --- Former Members of Parliament (Cooling-off Period under FATF & Act 1044 Rules) ---
  {
    id: "gh-pep-parl-031",
    name: "Osei Kyei-Mensah-Bonsu",
    aliases: ["Kyei-Mensah-Bonsu", "Osei Kyei Mensah Bonsu"],
    role: "Former Majority Leader of Parliament, Former MP for Suame",
    constituency: "Suame",
    term: "1997-01-07 to 2025-01-06",
    status: "cooling_off",
    familyMembers: ["Irene Kyei-Mensah-Bonsu (Spouse)"],
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-032",
    name: "Kennedy Ohene Agyapong",
    aliases: ["Kennedy Agyapong", "Ken Agyapong"],
    role: "Former Member of Parliament for Assin Central",
    constituency: "Assin Central",
    term: "2001-01-07 to 2025-01-06",
    status: "cooling_off",
    familyMembers: ["Stella Wilson Agyapong (Spouse)"],
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-033",
    name: "Joseph Osei-Owusu",
    aliases: ["Joe Wise", "Joseph Osei Owusu"],
    role: "Former First Deputy Speaker of Parliament, Former MP for Bekwai",
    constituency: "Bekwai",
    term: "2009-01-07 to 2025-01-06",
    status: "cooling_off",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-034",
    name: "Sarah Adwoa Safo",
    aliases: ["Adwoa Safo"],
    role: "Former Member of Parliament for Dome-Kwabenya, Former Minister for Gender",
    constituency: "Dome-Kwabenya",
    term: "2013-01-07 to 2025-01-06",
    status: "cooling_off",
    familyMembers: ["Apostle Kwadwo Safo Kantanka (Father)", "Hanny Mouhtiseb (Spouse)"],
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-parl-035",
    name: "Kobina Tahir Hammond",
    aliases: ["K.T. Hammond", "KT Hammond"],
    role: "Former Member of Parliament for Adansi Asokwa, Former Minister for Trade",
    constituency: "Adansi Asokwa",
    term: "2001-01-07 to 2025-01-06",
    status: "cooling_off",
    ref: "https://parliament.gh"
  }
];

// ============================================================================
// 3. GHANA SOE, REGULATORS & JUDICIARY PEPS (Current vs Outgoing Leadership)
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
    name: "Mariama Owusu",
    aliases: ["Justice Mariama Owusu"],
    role: "Justice of the Supreme Court of Ghana",
    term: "2019-12-17 to present",
    status: "active",
    ref: "https://judicial.gov.gh"
  },
  {
    id: "gh-pep-soe-005",
    name: "Avril Lovelace-Johnson",
    aliases: ["Justice Avril Lovelace-Johnson"],
    role: "Justice of the Supreme Court of Ghana",
    term: "2019-12-17 to present",
    status: "active",
    ref: "https://judicial.gov.gh"
  },
  {
    id: "gh-pep-soe-006",
    name: "Emmanuel Yonny Kulendi",
    aliases: ["Justice Yonny Kulendi", "Yonny Kulendi"],
    role: "Justice of the Supreme Court of Ghana",
    term: "2020-05-26 to present",
    status: "active",
    ref: "https://judicial.gov.gh"
  },
  {
    id: "gh-pep-soe-007",
    name: "Issifu Omoro Tanko Amadu",
    aliases: ["Justice Tanko Amadu", "Tanko Amadu"],
    role: "Justice of the Supreme Court of Ghana",
    term: "2020-05-26 to present",
    status: "active",
    ref: "https://judicial.gov.gh"
  },
  {
    id: "gh-pep-soe-008",
    name: "Henrietta Joy Abena Nyarko Mensa-Bonsu",
    aliases: ["Justice Henrietta Mensa-Bonsu", "Prof. Henrietta Mensa-Bonsu"],
    role: "Justice of the Supreme Court of Ghana",
    term: "2020-05-26 to present",
    status: "active",
    ref: "https://judicial.gov.gh"
  },

  // --- Independent Constitutional Bodies & Anti-Corruption ---
  {
    id: "gh-pep-soe-009",
    name: "Kissi Agyebeng",
    aliases: ["Special Prosecutor Kissi Agyebeng"],
    role: "Special Prosecutor of the Republic of Ghana, Office of the Special Prosecutor (OSP)",
    term: "2021-08-05 to present",
    status: "active",
    ref: "https://osp.gov.gh"
  },
  {
    id: "gh-pep-soe-010",
    name: "Joseph Whittal",
    aliases: ["Commissioner Joseph Whittal"],
    role: "Commissioner, Commission on Human Rights and Administrative Justice (CHRAJ)",
    term: "2016-12-20 to present",
    status: "active",
    ref: "https://chraj.gov.gh"
  },
  {
    id: "gh-pep-soe-011",
    name: "Jean Adukwei Mensa",
    aliases: ["Jean Mensa", "Mrs. Jean Mensa"],
    role: "Chairperson, Electoral Commission of Ghana",
    term: "2018-07-23 to present",
    status: "active",
    familyMembers: ["Dr. Charles Mensa (Spouse)"],
    ref: "https://ec.gov.gh"
  },
  {
    id: "gh-pep-soe-012",
    name: "Johnson Akuamoah Asiedu",
    aliases: ["Akuamoah Asiedu"],
    role: "Auditor-General of the Republic of Ghana, Ghana Audit Service",
    term: "2021-08-27 to present",
    status: "active",
    ref: "https://ghaudit.org"
  },

  // --- Central Bank & Financial Intelligence ---
  {
    id: "gh-pep-soe-013",
    name: "Ernest Kwamina Yedu Addison",
    aliases: ["Dr. Ernest Addison", "Dr. Ernest K. Y. Addison"],
    role: "Governor, Bank of Ghana (Central Bank)",
    term: "2017-03-30 to present",
    status: "active",
    familyMembers: ["Philip Addison (Brother)"],
    ref: "https://bog.gov.gh"
  },
  {
    id: "gh-pep-soe-014",
    name: "Maxwell Opoku-Afari",
    aliases: ["Dr. Maxwell Opoku-Afari"],
    role: "First Deputy Governor, Bank of Ghana",
    term: "2017-08-07 to present",
    status: "active",
    ref: "https://bog.gov.gh"
  },
  {
    id: "gh-pep-soe-015",
    name: "Elsie Addo Awadzi",
    aliases: ["Mrs. Elsie Addo Awadzi"],
    role: "Second Deputy Governor, Bank of Ghana",
    term: "2018-02-14 to present",
    status: "active",
    ref: "https://bog.gov.gh"
  },
  {
    id: "gh-pep-soe-016",
    name: "Kwabena Osei-Bonsu",
    aliases: ["K. Osei-Bonsu"],
    role: "Chief Executive Officer, Financial Intelligence Centre (FIC Ghana)",
    term: "2020-01-01 to present",
    status: "active",
    ref: "https://fic.gov.gh"
  },

  // --- Security Chiefs & Armed Forces ---
  {
    id: "gh-pep-soe-017",
    name: "Thomas Oppong-Peprah",
    aliases: ["Lt. Gen. Thomas Oppong-Peprah"],
    role: "Chief of Defence Staff (CDS), Ghana Armed Forces",
    term: "2024-02-01 to present",
    status: "active",
    ref: "https://gafonline.mil.gh"
  },
  {
    id: "gh-pep-soe-018",
    name: "George Akuffo Dampare",
    aliases: ["Dr. George Akuffo Dampare", "IGP Dampare"],
    role: "Inspector-General of Police (IGP), Ghana Police Service",
    term: "2021-08-01 to present",
    status: "active",
    familyMembers: ["Anita Dampare (Spouse)"],
    ref: "https://police.gov.gh"
  },
  {
    id: "gh-pep-soe-019",
    name: "Bismarck Kwasi Onwona",
    aliases: ["Maj. Gen. Bismarck Onwona"],
    role: "Chief of Army Staff, Ghana Armed Forces",
    term: "2024-02-01 to present",
    status: "active",
    ref: "https://gafonline.mil.gh"
  },
  {
    id: "gh-pep-soe-020",
    name: "Issah Adam Yakubu",
    aliases: ["Rear Admiral Issah Yakubu"],
    role: "Chief of Naval Staff, Ghana Navy",
    term: "2021-02-05 to present",
    status: "active",
    ref: "https://gafonline.mil.gh"
  },
  {
    id: "gh-pep-soe-021",
    name: "Kwame Asuah Takyi",
    aliases: ["Comptroller-General Kwame Takyi"],
    role: "Comptroller-General, Ghana Immigration Service (GIS)",
    term: "2017-03-01 to present",
    status: "active",
    ref: "https://gis.gov.gh"
  },

  // --- State-Owned Enterprises (Current & Acting Transition CEOs) ---
  {
    id: "gh-pep-soe-022",
    name: "David Asamoah",
    aliases: ["David Boadi Asamoah", "Ing. David Asamoah"],
    role: "Acting Managing Director, Electricity Company of Ghana (ECG)",
    term: "2024-09-26 to present",
    status: "active",
    ref: "https://ecg.com.gh"
  },
  {
    id: "gh-pep-soe-023",
    name: "Daniel Wilson Addo",
    aliases: ["Daniel Addo"],
    role: "Managing Director, Consolidated Bank Ghana (CBG)",
    term: "2018-08-01 to present",
    status: "active",
    ref: "https://cbg.com.gh"
  },
  {
    id: "gh-pep-soe-024",
    name: "Patrick Kuma-Aboagye",
    aliases: ["Dr. Patrick Kuma-Aboagye"],
    role: "Director-General, Ghana Health Service (GHS)",
    term: "2019-11-25 to present",
    status: "active",
    ref: "https://ghs.gov.gh"
  },

  // --- State-Owned Enterprises (Former CEOs in Statutory Cooling-off Period) ---
  {
    id: "gh-pep-soe-025",
    name: "Julie Essiam",
    aliases: ["Commissioner-General Julie Essiam"],
    role: "Former Commissioner-General, Ghana Revenue Authority (GRA) (2024–2025)",
    term: "2024-03-26 to 2025-01-06",
    status: "cooling_off",
    ref: "https://gra.gov.gh"
  },
  {
    id: "gh-pep-soe-026",
    name: "Ammishaddai Owusu-Amoah",
    aliases: ["Rev. Dr. Ammishaddai Owusu-Amoah"],
    role: "Former Commissioner-General, Ghana Revenue Authority (GRA) (2019–2024)",
    term: "2019-10-01 to 2024-03-26",
    status: "cooling_off",
    ref: "https://gra.gov.gh"
  },
  {
    id: "gh-pep-soe-027",
    name: "Opoku-Ahweneeh Danquah",
    aliases: ["Opoku Danquah"],
    role: "Former Chief Executive Officer, Ghana National Petroleum Corporation (GNPC) (2022–2024)",
    term: "2022-04-21 to 2024-05-01",
    status: "cooling_off",
    ref: "https://gnpcghana.com"
  },
  {
    id: "gh-pep-soe-028",
    name: "Kofi Kodua Sarpong",
    aliases: ["Dr. K.K. Sarpong"],
    role: "Former Chief Executive Officer, Ghana National Petroleum Corporation (GNPC) (2017–2022)",
    term: "2017-01-27 to 2022-04-21",
    status: "cooling_off",
    familyMembers: ["Patricia Appiagyei (Spouse / MP Asokwa)"],
    ref: "https://gnpcghana.com"
  },
  {
    id: "gh-pep-soe-029",
    name: "Emmanuel Antwi-Darkwa",
    aliases: ["Ing. Emmanuel Antwi-Darkwa"],
    role: "Former Chief Executive, Volta River Authority (VRA) (2017–2025)",
    term: "2017-02-06 to 2025-01-06",
    status: "cooling_off",
    ref: "https://vra.com"
  },
  {
    id: "gh-pep-soe-030",
    name: "Samuel Dubik Masubir Mahama",
    aliases: ["Dubik Mahama", "Samuel Mahama"],
    role: "Former Managing Director, Electricity Company of Ghana (ECG) (2022–2024)",
    term: "2022-05-13 to 2024-09-25",
    status: "cooling_off",
    ref: "https://ecg.com.gh"
  },
  {
    id: "gh-pep-soe-031",
    name: "Edwin Alfred Provencal",
    aliases: ["Edwin Provencal"],
    role: "Former Managing Director, Bulk Energy Storage and Transportation (BOST) (2019–2025)",
    term: "2019-08-26 to 2025-01-06",
    status: "cooling_off",
    ref: "https://bost.com.gh"
  },
  {
    id: "gh-pep-soe-032",
    name: "Kofi Bosompem Osafo-Maafo",
    aliases: ["Kofi Osafo-Maafo"],
    role: "Former Director-General, Social Security and National Insurance Trust (SSNIT) (2024–2025)",
    term: "2024-04-08 to 2025-01-06",
    status: "cooling_off",
    familyMembers: ["Yaw Osafo-Maafo (Father / Former Senior Minister)"],
    ref: "https://ssnit.org.gh"
  },
  {
    id: "gh-pep-soe-033",
    name: "Michael Achagwe Luguje",
    aliases: ["Michael Luguje"],
    role: "Former Director-General, Ghana Ports and Harbours Authority (GPHA) (2018–2025)",
    term: "2018-07-02 to 2025-01-06",
    status: "cooling_off",
    ref: "https://ghanaports.gov.gh"
  },
  {
    id: "gh-pep-soe-034",
    name: "Mustapha Abdul-Hamid",
    aliases: ["Dr. Mustapha Abdul-Hamid"],
    role: "Former Chief Executive, National Petroleum Authority (NPA) (2021–2025)",
    term: "2021-07-01 to 2025-01-06",
    status: "cooling_off",
    ref: "https://npa.gov.gh"
  },
  {
    id: "gh-pep-soe-035",
    name: "Joe Anokye",
    aliases: ["Joe Anokye"],
    role: "Former Director-General, National Communications Authority (NCA) (2017–2025)",
    term: "2017-01-15 to 2025-01-06",
    status: "cooling_off",
    ref: "https://nca.org.gh"
  },
  {
    id: "gh-pep-soe-036",
    name: "Joseph Boahen Aidoo",
    aliases: ["Hon. Joseph Boahen Aidoo"],
    role: "Former Chief Executive, Ghana Cocoa Board (COCOBOD) (2017–2024)",
    term: "2017-01-27 to 2024-12-31",
    status: "cooling_off",
    ref: "https://cocobod.gh"
  },
  {
    id: "gh-pep-soe-037",
    name: "Martin Kwaku Ayisi",
    aliases: ["Martin Ayisi"],
    role: "Former Chief Executive Officer, Minerals Commission (2021–2025)",
    term: "2021-08-01 to 2025-01-06",
    status: "cooling_off",
    ref: "https://mincom.gov.gh"
  },
  {
    id: "gh-pep-soe-038",
    name: "Michael Oquaye Jnr",
    aliases: ["Amb. Michael Oquaye Jnr"],
    role: "Former Chief Executive Officer, Ghana Free Zones Authority (GFZA) (2021–2025)",
    term: "2021-04-01 to 2025-01-06",
    status: "cooling_off",
    familyMembers: ["Prof. Aaron Mike Oquaye (Father / Former Speaker of Parliament)"],
    ref: "https://gfza.gov.gh"
  },
  {
    id: "gh-pep-soe-039",
    name: "Reginald Yofi Grant",
    aliases: ["Yofi Grant"],
    role: "Former Chief Executive Officer, Ghana Investment Promotion Centre (GIPC) (2017–2025)",
    term: "2017-02-20 to 2025-01-06",
    status: "cooling_off",
    ref: "https://gipc.gov.gh"
  }
];

// ============================================================================
// 4. GHANA PEP RELATIVES & CLOSE ASSOCIATES (RCAs)
// ============================================================================
const rcaList = [
  // --- Relatives and Associates of President John Dramani Mahama ---
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
  {
    id: "gh-pep-rca-006",
    name: "Shahid Mahama",
    aliases: ["Shahid Dramani Mahama"],
    role: "Son of President John Dramani Mahama",
    relationship: "Son / Child",
    relatedPep: "John Dramani Mahama (President of Ghana)",
    term: "2012-07-24 to present",
    status: "active",
    ref: "https://presidency.gov.gh"
  },

  // --- Relatives of Vice President Jane Naana Opoku-Agyemang ---
  {
    id: "gh-pep-rca-007",
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
    id: "gh-pep-rca-008",
    name: "Kwabena Opoku-Agyemang",
    aliases: ["Dr. Kwabena Opoku-Agyemang"],
    role: "Academic, Son of Vice President Jane Naana Opoku-Agyemang",
    relationship: "Son / Child",
    relatedPep: "Jane Naana Opoku-Agyemang (Vice President of Ghana)",
    term: "2025-01-07 to present",
    status: "active",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-rca-009",
    name: "Adwoa Opoku-Agyemang",
    aliases: ["Dr. Adwoa Opoku-Agyemang"],
    role: "Daughter of Vice President Jane Naana Opoku-Agyemang",
    relationship: "Daughter / Child",
    relatedPep: "Jane Naana Opoku-Agyemang (Vice President of Ghana)",
    term: "2025-01-07 to present",
    status: "active",
    ref: "https://presidency.gov.gh"
  },

  // --- Relatives and Close Associates of Former President Nana Akufo-Addo ---
  {
    id: "gh-pep-rca-010",
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
    id: "gh-pep-rca-011",
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
    id: "gh-pep-rca-012",
    name: "Edwina Nana Dokua Akufo-Addo",
    aliases: ["Dokua Akufo-Addo"],
    role: "Daughter of Former President Nana Akufo-Addo",
    relationship: "Daughter / Child",
    relatedPep: "Nana Addo Dankwa Akufo-Addo (Former President of Ghana)",
    term: "2017-01-07 to 2025-01-06",
    status: "cooling_off",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-rca-013",
    name: "Edward Akufo-Addo",
    aliases: ["Bumpty Akufo-Addo"],
    role: "Businessman, Brother of Former President Nana Akufo-Addo",
    relationship: "Brother",
    relatedPep: "Nana Addo Dankwa Akufo-Addo (Former President of Ghana)",
    term: "2017-01-07 to 2025-01-06",
    status: "cooling_off",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-rca-014",
    name: "Gabby Asare Otchere-Darko",
    aliases: ["Gabby Otchere-Darko", "Gabriel Asare Otchere-Darko"],
    role: "Senior Legal Practitioner, Close Associate and Nephew of Former President Nana Akufo-Addo",
    relationship: "Nephew / Close Associate / Legal Advisor",
    relatedPep: "Nana Addo Dankwa Akufo-Addo (Former President of Ghana)",
    term: "2017-01-07 to 2025-01-06",
    status: "cooling_off",
    ref: "https://presidency.gov.gh"
  },

  // --- Relatives and Close Associates of Former Vice President Mahamudu Bawumia ---
  {
    id: "gh-pep-rca-015",
    name: "Samira Bawumia",
    aliases: ["Samira Ramadan", "Hajia Samira Bawumia"],
    role: "Former Second Lady of Ghana, Spouse of Former Vice President Mahamudu Bawumia",
    relationship: "Spouse / Former Second Lady",
    relatedPep: "Mahamudu Bawumia (Former Vice President of Ghana)",
    term: "2017-01-07 to 2025-01-06",
    status: "cooling_off",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-rca-016",
    name: "Ahmed Ramadan",
    aliases: ["Alhaji Ahmed Ramadan"],
    role: "Former Ambassador to UAE, Father-in-law of Former Vice President Mahamudu Bawumia",
    relationship: "Father-in-law",
    relatedPep: "Mahamudu Bawumia (Former Vice President of Ghana)",
    term: "2017-01-07 to 2025-01-06",
    status: "cooling_off",
    ref: "https://presidency.gov.gh"
  },
  {
    id: "gh-pep-rca-017",
    name: "Abu Ramadan",
    aliases: ["Mohammed Abu Ramadan"],
    role: "Former Deputy NADMO Director, Brother-in-law of Former Vice President Mahamudu Bawumia",
    relationship: "Brother-in-law",
    relatedPep: "Mahamudu Bawumia (Former Vice President of Ghana)",
    term: "2017-01-07 to 2025-01-06",
    status: "cooling_off",
    ref: "https://presidency.gov.gh"
  },

  // --- Relatives and Associates of Former Finance Minister Ken Ofori-Atta ---
  {
    id: "gh-pep-rca-018",
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
    id: "gh-pep-rca-019",
    name: "Keli Gadzekpo",
    aliases: ["Kelly Gadzekpo"],
    role: "Co-founder of Databank, Close Business Associate of Former Finance Minister Ken Ofori-Atta",
    relationship: "Close Business Associate / Corporate Partner",
    relatedPep: "Kenneth Nana Yaw Ofori-Atta (Former Minister for Finance)",
    term: "2017-01-27 to 2024-02-14",
    status: "cooling_off",
    ref: "https://mofep.gov.gh"
  },

  // --- Relatives of Speaker and Parliamentary Leadership ---
  {
    id: "gh-pep-rca-020",
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
    id: "gh-pep-rca-021",
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
    id: "gh-pep-rca-022",
    name: "Nuhela Seidu",
    aliases: ["Nuhela Seidu Ablakwa"],
    role: "Legal Practitioner and Media Executive, Spouse of MP Samuel Okudzeto Ablakwa",
    relationship: "Spouse / Wife",
    relatedPep: "Samuel Okudzeto Ablakwa (Member of Parliament)",
    term: "2013-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },

  // --- Immediate Family of MP Zanetor Agyeman-Rawlings ---
  {
    id: "gh-pep-rca-023",
    name: "Nana Konadu Agyeman-Rawlings",
    aliases: ["Nana Konadu Agyeman Rawlings", "Former First Lady Nana Konadu"],
    role: "Former First Lady of Ghana, Mother of MP Zanetor Agyeman-Rawlings",
    relationship: "Mother / Former First Lady",
    relatedPep: "Zanetor Agyeman-Rawlings (Member of Parliament)",
    term: "2017-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-rca-024",
    name: "Yaa Asantewaa Rawlings",
    aliases: ["Yaa Rawlings"],
    role: "Sister of MP Zanetor Agyeman-Rawlings",
    relationship: "Sister",
    relatedPep: "Zanetor Agyeman-Rawlings (Member of Parliament)",
    term: "2017-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-rca-025",
    name: "Amina Rawlings",
    aliases: ["Amina Rawlings Esq."],
    role: "Legal Practitioner, Sister of MP Zanetor Agyeman-Rawlings",
    relationship: "Sister",
    relatedPep: "Zanetor Agyeman-Rawlings (Member of Parliament)",
    term: "2017-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },
  {
    id: "gh-pep-rca-026",
    name: "Kimathi Rawlings",
    aliases: ["Kimathi Rawlings"],
    role: "Brother of MP Zanetor Agyeman-Rawlings",
    relationship: "Brother",
    relatedPep: "Zanetor Agyeman-Rawlings (Member of Parliament)",
    term: "2017-01-07 to present",
    status: "active",
    ref: "https://parliament.gh"
  },

  // --- Relatives of Central Bank and Judicial Leadership ---
  {
    id: "gh-pep-rca-027",
    name: "Philip Addison",
    aliases: ["Lawyer Philip Addison"],
    role: "Senior Legal Practitioner, Brother of Bank of Ghana Governor Ernest Addison",
    relationship: "Brother",
    relatedPep: "Ernest Kwamina Yedu Addison (Governor, Bank of Ghana)",
    term: "2017-03-30 to present",
    status: "active",
    ref: "https://bog.gov.gh"
  },
  {
    id: "gh-pep-rca-028",
    name: "Francis Torkornoo",
    aliases: ["Mr. Francis Torkornoo"],
    role: "Spouse of Chief Justice Gertrude Torkornoo",
    relationship: "Spouse / Husband",
    relatedPep: "Gertrude Araba Esaaba Torkornoo (Chief Justice of Ghana)",
    term: "2023-06-12 to present",
    status: "active",
    ref: "https://judicial.gov.gh"
  },
  {
    id: "gh-pep-rca-029",
    name: "Yaw Osafo-Maafo",
    aliases: ["Yaw Osafo Maafo"],
    role: "Former Senior Minister / Presidential Advisor, Father of SSNIT Former Director-General Kofi Osafo-Maafo",
    relationship: "Father / Former Senior Minister",
    relatedPep: "Kofi Bosompem Osafo-Maafo (Former Director-General, SSNIT)",
    term: "2017-01-27 to 2024-04-08",
    status: "cooling_off",
    ref: "https://ssnit.org.gh"
  },
  {
    id: "gh-pep-rca-030",
    name: "Aaron Mike Oquaye",
    aliases: ["Prof. Mike Oquaye", "Former Speaker Mike Oquaye"],
    role: "Former Speaker of Parliament (2017–2021), Father of Former GFZA CEO Michael Oquaye Jnr",
    relationship: "Father / Former Speaker of Parliament",
    relatedPep: "Michael Oquaye Jnr (Former CEO, Ghana Free Zones Authority)",
    term: "2017-01-07 to 2021-01-06",
    status: "cooling_off",
    ref: "https://parliament.gh"
  }
];

// ============================================================================
// TRANSFORMATION TO VERSION 2 CONTROLLED SNAPSHOT
// (Only permitted keys in properties to ensure strict Go parser compliance)
// ============================================================================
function transformToSnapshot(items, sourceKey, label, isRcaDataset = false) {
  const records = items.map(p => {
    const isCoolingOff = p.status === "cooling_off";
    
    // Programs array: category + explicit role / relationship
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

    // SourceStatus: readable human summary of role, term, status, and related persons
    let statusDesc = isRcaDataset
      ? `Ghana RCA; Relationship: ${p.relationship}; Connected PEP: ${p.relatedPep}; Role: ${p.role}; Term: ${p.term}; Status: ${p.status || "active"}`
      : `Ghana PEP; Role: ${p.role}; Term: ${p.term}; Status: ${p.status || "active"}`;

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
      listedAt: [p.term.slice(0, 10)],
      sourceStatus: [statusDesc],
      sanctionsImposed: [
        isRcaDataset
          ? `Enhanced Due Diligence (EDD) Required; AML Act 1044 Relative or Close Associate (RCA) Screening [${p.relationship} of ${p.relatedPep}]`
          : (isCoolingOff
              ? "Enhanced Due Diligence (EDD) Required; AML Act 1044 Statutory Cooling-Off Period for Former Office Holder"
              : "Enhanced Due Diligence (EDD) Required; AML Act 1044 Active PEP Screening")
      ]
    };

    if (p.constituency) {
      properties.address = [`Constituency: ${p.constituency}, Ghana`];
    } else {
      properties.address = ["Ghana"];
    }

    return {
      id: p.id,
      name: p.name,
      type: "Person",
      aliases: p.aliases || [],
      source_reference: p.ref,
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

// Generate the 4 Ghana PEP & RCA fixture files
const execFixture = transformToSnapshot(executivePeps, "gh-pep-executive", "Ghana Executive PEPs", false);
fs.writeFileSync("gh_pep_executive.json", JSON.stringify(execFixture, null, 2), "utf8");
console.log(`Generated gh_pep_executive.json (${execFixture.records.length} records)`);

const parlFixture = transformToSnapshot(parliamentPeps, "gh-pep-parliament", "Ghana Parliament PEPs", false);
fs.writeFileSync("gh_pep_parliament.json", JSON.stringify(parlFixture, null, 2), "utf8");
console.log(`Generated gh_pep_parliament.json (${parlFixture.records.length} records)`);

const soeFixture = transformToSnapshot(soeJudiciaryPeps, "gh-pep-soe", "Ghana SOE & Judiciary PEPs", false);
fs.writeFileSync("gh_pep_soe.json", JSON.stringify(soeFixture, null, 2), "utf8");
console.log(`Generated gh_pep_soe.json (${soeFixture.records.length} records)`);

const rcaFixture = transformToSnapshot(rcaList, "gh-pep-rca", "Ghana PEP Relatives and Associates", true);
fs.writeFileSync("gh_pep_rca.json", JSON.stringify(rcaFixture, null, 2), "utf8");
console.log(`Generated gh_pep_rca.json (${rcaFixture.records.length} records)`);
