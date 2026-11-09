import {readFileSync, writeFileSync} from 'node:fs';
import {resolve} from 'node:path';

const directory = resolve(process.argv[2] ?? '');
if (!process.argv[2]) throw new Error('Provide a passing native MySQL evidence directory');
for (const name of ['tests.exit', 'vet.exit']) {
  if (readFileSync(resolve(directory, name), 'utf8').trim() !== '0') throw new Error(`${name} must be zero`);
}
const events = readFileSync(resolve(directory, 'raw.jsonl'), 'utf8').split(/\r?\n/).filter(Boolean).map(JSON.parse);
const test = 'TestMySQLVelocityHistoryPlansAndExactResults';
const completion = events.findLast(event => event.Test === test && ['pass', 'fail', 'skip'].includes(event.Action));
if (completion?.Action !== 'pass') throw new Error('Complete native velocity workload must pass');
const output = events.filter(event => event.Test === test && event.Output).map(event => event.Output).join('');
const marker = 'VELOCITY_EVIDENCE ';
const line = output.split('\n').findLast(line => line.includes(marker));
if (!line) throw new Error('Native query-plan evidence missing');
const workload = JSON.parse(line.slice(line.indexOf(marker) + marker.length));
const statistics = values => {
  if (values.length !== 10 || values.some(value => !Number.isFinite(value) || value < 0)) throw new Error('Expected ten retained timing samples');
  const sorted = [...values].sort((a, b) => a - b);
  return {samples: values.length, medianMS: (sorted[4] + sorted[5]) / 2, p95MS: sorted[9]};
};
const tablePlan = plan => {
  const table = plan?.query_block?.table;
  if (!table) throw new Error('Single-table aggregate plan missing');
  return {access: table.access_type, key: table.key ?? null, estimatedRows: table.rows_examined_per_scan, keyParts: table.used_key_parts ?? []};
};
const report = {
  recordedAt: new Date().toISOString(),
  scope: 'Disposable MySQL consumer-contract fixture; SQL and reader timings, not HTTP or ingestion throughput',
  rows: workload.rows,
  comparison: 'Same rows, composite prefix index, parameters and half-open windows; legacy cast predicate versus indexable equality plus complete binary residual',
  queries: workload.samples.map(sample => ({
    name: sample.name,
    before: {...tablePlan(sample.BeforePlan), ...statistics(sample.BeforeMS)},
    after: {...tablePlan(sample.AfterPlan), ...statistics(sample.AfterMS)},
    reader: statistics(sample.ReaderMS),
    beforeAnalyze: sample.BeforeAnalyze,
    afterAnalyze: sample.AfterAnalyze,
  })),
  limitations: [
    'Ten warm sequential samples after three warmups per SQL variant; no concurrency or sustained capacity claim',
    'Independent deterministic oracle checks counts, sums, distinct values and actual AST fraud-rule hits',
    'Physical table and index are consumer-contract fixtures, not evidence of owner publication or live ingestion performance',
    'A hot account with a broad 24-hour window still reads thousands of matching historical records',
    '500 TPS and end-to-end p95 <= 50 ms are not established by this workload',
  ],
};
writeFileSync(resolve(directory, 'velocity-summary.json'), JSON.stringify(report, null, 2) + '\n');
console.log(JSON.stringify(report, null, 2));
