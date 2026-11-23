import { createHash } from 'node:crypto';

const prefix = 'gh-pep-mmdce-live-';
const sourceURL = 'https://www.ghanadistricts.com/home/AllDistricts';
const normalize = value => value.normalize('NFKC').toLowerCase()
  .replace(/^(?:(?:hon|dr|rev|mr|mrs|ms|prof)\.?\s+)+/, '')
  .replace(/[^\p{L}\p{N}]+/gu, ' ').trim();
const officeKey = value => normalize(value).replace(/\bassembly\b/g, '').replace(/\s+/g, ' ').trim();

export function mergeObservations(baseline, offices, previous, capture) {
  if (capture.schema_version !== 2 || !['live_html_observation', 'retained_html_replay'].includes(capture.acquisition)
      || capture.source_url !== sourceURL || capture.complete_national_roster !== false
      || capture.pages_fetched !== 1 || !/^[a-f0-9]{64}$/.test(capture.artifact_sha256)
      || !Array.isArray(capture.records) || !capture.records.length || !Array.isArray(capture.rejected_rows)
      || capture.source_rows !== capture.records.length + capture.rejected_rows.length) {
    throw new Error('Expected a validated HTML observation manifest; run scripts/scrape-live-mmdces.mjs first');
  }
  const records = new Map(baseline.map(record => [record.id, structuredClone(record)]));
  // Visible batches are not withdrawal feeds. Preserve earlier observations.
  for (const record of previous) {
    if (record.id.startsWith(prefix) && !records.has(record.id)) records.set(record.id, structuredClone(record));
  }
  const index = new Map();
  for (const record of records.values()) {
    const district = offices.get(record.id) ?? record.properties.sourceStatus?.[0]?.match(/; Assembly: ([^;]+)/)?.[1];
    if (!district) continue;
    for (const name of [record.name, ...record.aliases]) {
      const key = `${officeKey(district)}|${normalize(name)}`;
      const ids = index.get(key) ?? new Set();
      ids.add(record.id);
      index.set(key, ids);
    }
  }
  const added = [], matched = [], seen = new Set();
  for (const row of capture.records) {
    if (![row.name, row.region, row.district, row.role, row.source_reference].every(value => typeof value === 'string' && value.trim())
        || /[;\r\n]/.test(row.district + row.region + row.role)
        || row.role !== `${row.district.match(/\b(Metropolitan|Municipal|District)\b/)?.[1]} Chief Executive, ${row.district}`
        || !/^https:\/\/(?:www\.)?ghanadistricts\.com\/home\/(?:AllDLeaders|DistrictLeaders)\/\d+$/i.test(row.source_reference)) {
      throw new Error('Invalid MMDCE observation');
    }
    const key = `${officeKey(row.district)}|${normalize(row.name)}`;
    if (seen.has(key)) throw new Error('Duplicate MMDCE observation');
    seen.add(key);
    const matches = index.get(key);
    if (matches?.size > 1) throw new Error(`Ambiguous person/assembly identity: ${row.name}`);
    if (matches?.size === 1) {
      matched.push({ id: [...matches][0], name: row.name, district: row.district });
      continue;
    }
    const id = prefix + createHash('sha256').update(key).digest('hex').slice(0, 24);
    if (records.has(id)) throw new Error('MMDCE identity hash conflict');
    records.set(id, {
      id, name: row.name, type: 'Person', aliases: [],
      source_reference: row.source_reference, source_type: 'pep_evidence',
      properties: {
        country: ['gh'], countryText: ['Ghana'],
        programs: ['Politically Exposed Person (PEP)', 'Sub-National Domestic PEP', row.role],
        sourceStatus: [`GhanaDistricts visible directory; Region: ${row.region}; Role: ${row.role}; Status: source_listed; Assembly: ${row.district}`],
        address: [`${row.region} Region, Ghana`],
      },
    });
    index.set(key, new Set([id]));
    added.push({ id, name: row.name, district: row.district });
  }
  const baseIds = new Set(baseline.map(record => record.id));
  const sorted = [...baseline.map(record => records.get(record.id)), ...[...records.values()]
    .filter(record => !baseIds.has(record.id)).sort((a, b) => a.id.localeCompare(b.id, 'en'))];
  return { records: sorted, report: {
    complete_national_roster: false, observed_at: capture.observed_at,
    source_url: sourceURL, artifact_sha256: capture.artifact_sha256,
    source_rows: capture.source_rows, named_observations: capture.records.length,
    rejected_rows: capture.rejected_rows, added, matched,
    retained_records: sorted.length - added.length,
    note: 'Visible observations supplement the curated baseline; absent people are retained. Baseline offices and legal labels are not nationally reverified by this fetch.',
  } };
}
