import assert from 'node:assert/strict';
import {mkdirSync, readFileSync, writeFileSync} from 'node:fs';
import {spawnSync} from 'node:child_process';
import {dirname, resolve} from 'node:path';
import {fileURLToPath, pathToFileURL} from 'node:url';

// Run all live suites sequentially: their fixtures are new, but DDL is shared.
// Deletes and deliberate drift touch only fixtures allocated by each suite.
const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const startedAt = new Date().toISOString();
const outputIndex = process.argv.indexOf('--output');
const output = resolve(root, outputIndex < 0 ? `evidence/mysql-endpoint-audit/${startedAt.replaceAll(':', '-')}` : process.argv[outputIndex + 1]);
mkdirSync(output, {recursive: true});
const status = readFileSync(resolve(root, 'MYSQL_ENDPOINT_STATUS.md'), 'utf8');
const operations = [...status.matchAll(/^- \[x\] (GET|POST|PUT|PATCH|DELETE) (\/\S+)/gm)].map(([, method, path]) => ({
  operation: `${method} ${path}`, method,
  pattern: new RegExp('^' + path.split('/').map(part => /^\{\w+\}$/.test(part) ? '[^/]+' : part.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('/') + '$'),
}));
assert.equal(operations.length, 43, 'Reconcile the documented operation inventory before running');
assert.equal(new Set(operations.map(o => o.operation)).size, operations.length, 'Duplicate documented operation');
const suites = [
  ['baseline', 'mysql-endpoint-baseline.mjs'],
  ['mutations', 'mysql-mutation-smoke.mjs'],
  ['partial-physical', 'mysql-partial-endpoints-smoke.mjs', '--physical'],
  ['unique-text', 'mysql-unique-text-smoke.mjs'],
  ['text-constraints', 'mysql-text-constraints-smoke.mjs'],
  ['field-delete-enum', 'mysql-field-delete-enum-smoke.mjs'],
  ['display', 'mysql-display-smoke.mjs'],
  ['portable', 'mysql-portable-smoke.mjs'],
  ['table-delete-enum-create', 'mysql-table-delete-enum-create-smoke.mjs'],
  ['remaining', 'mysql-remaining-endpoints-smoke.mjs'],
];
const results = [];
for (const [name, script, ...args] of suites) {
  console.log(`Running ${name}`);
  const trace = resolve(output, `${name}.jsonl`);
  writeFileSync(trace, '');
  const start = performance.now();
  const child = spawnSync(process.execPath, ['--import', pathToFileURL(resolve(root, 'scripts/mysql-endpoint-trace.mjs')).href, resolve(root, 'scripts', script), ...args], {
    cwd: root, encoding: 'utf8', timeout: 600000, maxBuffer: 16 << 20,
    env: {...process.env, DATA_MODEL_ENDPOINT_TRACE: trace},
  });
  const log = (child.stdout ?? '') + (child.stderr ?? '') + (child.error ? `\n${child.error.stack}\n` : '');
  writeFileSync(resolve(output, `${name}.log`), log);
  const requests = readFileSync(trace, 'utf8').trim().split('\n').filter(Boolean).map(line => JSON.parse(line));
  const passed = child.status === 0 && !child.error;
  results.push({name, script, args, passed, exitCode: child.status, elapsedMs: Math.round(performance.now() - start), requests});
  console.log(`${passed ? 'PASS' : 'FAIL'} ${name}: ${requests.length} HTTP responses/transport errors`);
  if (!passed) console.error(log.slice(-6000));
}
const coverage = operations.map(({operation, method, pattern}) => {
  const matching = results.flatMap(suite => suite.requests.filter(r => r.method === method && pattern.test(r.path)).map(r => ({...r, suite: suite.name, suitePassed: suite.passed})));
  const successful = matching.filter(r => r.suitePassed && r.status >= 200 && r.status < 300);
  return {operation, covered: successful.length > 0, successfulRequests: successful.length,
    suites: [...new Set(successful.map(r => r.suite))],
    observedStatuses: [...new Set(matching.map(r => r.status ?? r.error))]};
});
const report = {
  startedAt, finishedAt: new Date().toISOString(), origin: process.env.DATA_MODEL_LOCAL_ORIGIN ?? 'http://localhost:8080',
  passed: results.every(r => r.passed) && coverage.every(r => r.covered),
  suitesPassed: results.filter(r => r.passed).length, suitesTotal: results.length,
  operationsCovered: coverage.filter(r => r.covered).length, operationsTotal: coverage.length,
  requestCount: results.reduce((sum, r) => sum + r.requests.length, 0),
  suites: results.map(({requests, ...suite}) => ({...suite, requestCount: requests.length})), coverage,
  limitations: ['Local administrator runtime only', 'Queue-state assertions require the MySQL worker paused; worker execution has separate live acceptance', 'No production authorization or representative load acceptance'],
};
writeFileSync(resolve(output, 'report.json'), JSON.stringify(report, null, 2) + '\n');
console.log(`${report.suitesPassed}/${report.suitesTotal} suites; ${report.operationsCovered}/${report.operationsTotal} operations; ${report.requestCount} requests. Report: ${resolve(output, 'report.json')}`);
process.exitCode = report.passed ? 0 : 1;
