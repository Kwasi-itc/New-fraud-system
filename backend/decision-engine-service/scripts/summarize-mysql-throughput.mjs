import {readFileSync, writeFileSync} from 'node:fs';
import {resolve} from 'node:path';

if (!process.argv[2]) throw new Error('Provide the passing native optimization profile directory');
const directory = resolve(process.argv[2]);
const interpolated = readFileSync(resolve(directory, 'parameter-interpolation.txt'), 'utf8').trim() === '1';
for (const name of ['tests.exit', 'vet.exit']) {
  if (readFileSync(resolve(directory, name), 'utf8').trim() !== '0') throw new Error(`${name} must be zero`);
}
const events = readFileSync(resolve(directory, 'raw.jsonl'), 'utf8').split(/\r?\n/).filter(Boolean).map(JSON.parse);
const test = 'TestMySQLAggregateOptimizationMeasurements';
if (events.findLast(e => e.Test === test && ['pass', 'fail', 'skip'].includes(e.Action))?.Action !== 'pass') throw new Error('Complete optimization profile must pass');
const output = events.filter(e => e.Test === test && e.Output).map(e => e.Output).join('');
if (!output.includes('MySQL fixture version 8.4.8')) throw new Error('Pinned native MySQL version evidence required');
const marker = 'AGGREGATE_OPTIMIZATION_EVIDENCE ';
const line = output.split('\n').findLast(line => line.includes(marker));
if (!line) throw new Error('Optimization measurements missing');
const workload = JSON.parse(line.slice(line.indexOf(marker) + marker.length));
const stats = values => {
  if (!values?.length || values.some(n => !Number.isFinite(n) || n < 0)) throw new Error('Invalid timing samples');
  const sorted = [...values].sort((a, b) => a - b);
  const middle = sorted.length / 2;
  return {samples: sorted.length, medianMS: sorted.length % 2 ? sorted[Math.floor(middle)] : (sorted[middle - 1] + sorted[middle]) / 2, p95MS: sorted[Math.ceil(sorted.length * .95) - 1]};
};
const report = {
  recordedAt: new Date().toISOString(),
  database: 'MySQL 8.4.8',
  scope: 'Disposable native consumer-contract fixture; complete aggregate reader operations, not HTTP, ingestion throughput or production capacity',
  rows: workload.rows,
  distribution: '50,000 records over one hour; approximately 10% share a hot account; account/time index; half-open window with raw boundary fragments',
  expectedCountSumCount: workload.expected,
  paths: Object.fromEntries(Object.entries(workload.paths_ms).map(([name, values]) => [name, stats(values)])),
  budgets: workload.budgets.map(b => ({...b, control_read_ms: stats(b.control_read_ms), allResultsValid: b.errors === 0 && b.aggregate_demands === 96 && b.control_errors === 0})),
  sqlTransport: interpolated ? 'Explicit UTF-8 parameter interpolation, matching the local decision runtime' : 'Prepared parameter transport',
  limits: 'Eight native fixture SQL connections; 2/4/8 aggregate admission slots; 2-second admission/query deadline; 150-microsecond collection window',
  limitations: [
    'Ten warm sequential samples per path after three warmups; shared development hardware can vary',
    'Fixture authorization is a consumer contract; live HTTP authorization, decision persistence and effect delivery are measured separately',
    'Summary schema/backfill here is a contract fixture; atomic replay/correction/rollback maintenance is independently tested in the actual ingestion owner',
    'Raw historical SQL remains authoritative for unsupported shapes, floats, distinct counts, stale versions, long groups and windows without an eligible interior',
    'No 500 TPS or end-to-end p95 <= 50 ms claim follows from these measurements',
  ],
};
writeFileSync(resolve(directory, 'throughput-summary.json'), JSON.stringify(report, null, 2) + '\n');
console.log(JSON.stringify(report, null, 2));
