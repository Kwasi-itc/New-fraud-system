import assert from 'node:assert/strict';
import test from 'node:test';
import { mergeObservations } from '../lib/mmdce-observations.mjs';

const old = { id: 'gh-pep-mmdce-195', name: 'Emmanuel Gemegah', aliases: [], properties: {
  sourceStatus: ['Role: Former Municipal Chief Executive, Keta Municipal; Status: former'],
} };
const offices = new Map([[old.id, 'Keta Municipal']]);
const wisdom = { name: 'Wisdom Bondieu Seade', region: 'Volta', district: 'Keta Municipal',
  role: 'Municipal Chief Executive, Keta Municipal', source_reference: 'https://www.ghanadistricts.com/home/AllDLeaders/182' };
const capture = rows => ({ schema_version: 2, acquisition: 'live_html_observation',
  source_url: 'https://www.ghanadistricts.com/home/AllDistricts', complete_national_roster: false,
  pages_fetched: 1, artifact_sha256: 'a'.repeat(64), source_rows: rows.length, records: rows, rejected_rows: [],
});

test('adds the missing source-listed person without reusing the predecessor ID or inventing dates', () => {
  const result = mergeObservations([old], offices, [], capture([wisdom]));
  assert.deepEqual(result.records[0], old);
  const record = result.records[1];
  assert.equal(record.name, wisdom.name);
  assert.match(record.id, /^gh-pep-mmdce-live-[a-f0-9]{24}$/);
  assert.equal(record.properties.listedAt, undefined);
  assert.equal(record.properties.sanctionsImposed, undefined);
  assert.deepEqual(record.aliases, []);
  assert.ok(record.properties.programs.includes(wisdom.role));
  assert.equal(result.report.complete_national_roster, false);
});

test('rebuilds deterministically and retains observations absent from a later partial batch', () => {
  const first = mergeObservations([old], offices, [], capture([wisdom]));
  const again = mergeObservations([old], offices, first.records, capture([wisdom]));
  assert.deepEqual(again.records, first.records);
  assert.equal(again.report.added.length, 0);
  const later = mergeObservations([old], offices, first.records, capture([{ ...wisdom, name: 'Another Named Person' }]));
  assert.deepEqual(later.records.find(record => record.name === wisdom.name), first.records[1]);
});

test('matches explicit aliases only within the same assembly and preserves existing IDs', () => {
  const baseline = { ...old, name: 'Freeman Tsekpo', aliases: ['Hon. Freeman Tsekpo'] };
  const result = mergeObservations([baseline], new Map([[old.id, 'Ashaiman Municipal Assembly']]), [], capture([
    { ...wisdom, name: 'FREEMAN TSEKPO', district: 'Ashaiman Municipal', role: 'Municipal Chief Executive, Ashaiman Municipal' },
  ]));
  assert.equal(result.records.length, 1);
  assert.equal(result.report.matched[0].id, old.id);
  const otherOffice = mergeObservations([baseline], offices, [], capture([{ ...wisdom, name: 'Freeman Tsekpo',
    district: 'Ashaiman Municipal', role: 'Municipal Chief Executive, Ashaiman Municipal' }]));
  assert.equal(otherOffice.records.length, 2);
});

test('does not fuzzy-merge similarly spelled people', () => {
  const baseline = { ...old, name: 'Wisdom Seade' };
  assert.equal(mergeObservations([baseline], offices, [], capture([wisdom])).records.length, 2);
});

test('ambiguous baseline identities and duplicate observations stop the build', () => {
  assert.throws(() => mergeObservations([old, { ...old, id: 'another' }],
    new Map([[old.id, 'Keta Municipal'], ['another', 'Keta Municipal']]), [],
    capture([{ ...wisdom, name: old.name }])), /Ambiguous/);
  assert.throws(() => mergeObservations([old], offices, [], capture([wisdom, wisdom])), /Duplicate/);
});

test('rejects the old fake crawl format and unapproved source references', () => {
  assert.throws(() => mergeObservations([old], offices, [], { records: [wisdom], totalPages: 17 }), /validated/);
  assert.throws(() => mergeObservations([old], offices, [], capture([{ ...wisdom, source_reference: 'https://evil.example/' }])), /Invalid/);
});
