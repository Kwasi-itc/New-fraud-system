import {existsSync, readFileSync, writeFileSync} from 'node:fs';
import {resolve} from 'node:path';

if (!process.argv[2]) throw new Error('Provide the evidence directory printed by test-mysql.sh');
const directories = process.argv.slice(2).map(path => resolve(path));
const directory = directories.at(-1);
const attempts = directories.map(directory => ({
  directory,
  events: readFileSync(resolve(directory, 'raw.jsonl'), 'utf8').split(/\r?\n/).filter(Boolean).map(JSON.parse),
  selection: readFileSync(resolve(directory, 'selection.txt'), 'utf8').trim(),
  testsExit: Number(readFileSync(resolve(directory, 'tests.exit'), 'utf8')),
  vetExit: Number(readFileSync(resolve(directory, 'vet.exit'), 'utf8')),
}));
// Track complete top-level tests, never partial subtest runs. This supports a
// timed-out package finishing in a later shard while preserving its earlier
// results. An independently collected full inventory must account for every
// top-level test before acceptance can pass.
const lastPackageAttempt = new Map();
const lastTopAttempt = new Map();
attempts.forEach((attempt, index) => {
  for (const event of attempt.events) {
    if (!['pass', 'fail', 'skip'].includes(event.Action)) continue;
    if (!event.Test) lastPackageAttempt.set(event.Package, index);
    else if (!event.Test.includes('/')) lastTopAttempt.set(event.Package + ':' + event.Test, index);
  }
});
const events = attempts.flatMap((attempt, index) => attempt.events.filter(e => e.Test
  ? lastTopAttempt.get(e.Package + ':' + e.Test.split('/')[0]) === index
  : lastPackageAttempt.get(e.Package) === index));
const inventoryDirectory = directories.findLast(directory => existsSync(resolve(directory, 'inventory.jsonl')));
if (!inventoryDirectory) throw new Error('A complete Go test inventory is required; use the current test-mysql.sh runner');
const inventoryEvents = readFileSync(resolve(inventoryDirectory, 'inventory.jsonl'), 'utf8').trim().split(/\r?\n/).map(JSON.parse);
const expectedTests = [...new Set(inventoryEvents.flatMap(e => (e.Output ?? '').split('\n').filter(line => /^Test\w+$/.test(line)).map(test => e.Package + ':' + test)))];
const passes = events.filter(e => e.Test && e.Action === 'pass');
const top = passes.filter(e => !e.Test.includes('/'));
// These require separate explicit resources/profiles and are not regression
// acceptance. Keep each skip visible rather than presenting it as a pass.
const optional = new Set(['TestConfiguredMMDBCanResolvePublicIP', 'TestManualEvaluationLoadAcceptance', 'TestManualHistoryLoadAcceptance', 'TestMySQLLogicalRestoreRetainsDecisionsJobsAndCaseReceipts', 'TestCaseWorkflowCrashSubprocess']);
const skipped = events.filter(e => e.Test && e.Action === 'skip').map(e => ({
  package: e.Package, test: e.Test, optional: optional.has(e.Test),
  output: events.filter(x => x.Package === e.Package && x.Test === e.Test && x.Output).map(x => x.Output).join('').trim(),
}));
const report = {
  reportedAt: new Date().toISOString(), selection: attempts.map(a => a.selection),
  toolchain: readFileSync(resolve(directory, 'toolchain.txt'), 'utf8').trim(),
  testsExit: events.some(e => e.Action === 'fail') ? 1 : 0,
  vetExit: attempts.at(-1).vetExit,
  fullModuleRun: attempts.some(a => a.selection.endsWith(' ./...')),
  expectedTopLevelTests: expectedTests.length,
  missingTests: expectedTests.filter(test => !lastTopAttempt.has(test)),
  attempts: attempts.map(a => ({directory: a.directory, selection: a.selection, testsExit: a.testsExit, vetExit: a.vetExit, failed: a.events.filter(e => e.Action === 'fail').map(e => ({package: e.Package, test: e.Test ?? null}))})),
  unexplainedRunFailures: attempts.filter(a => a.testsExit !== 0 && !a.events.some(e => !e.Test && e.Action === 'fail')).map(a => a.directory),
  topLevelPassed: top.length, testAndSubtestPasses: passes.length,
  packagesPassed: events.filter(e => !e.Test && e.Action === 'pass').map(e => e.Package),
  failed: events.filter(e => e.Action === 'fail').map(e => ({package: e.Package, test: e.Test ?? null})),
  skipped, unexpectedSkips: skipped.filter(e => !e.optional),
  raceWarnings: events.filter(e => e.Output?.includes('WARNING: DATA RACE')).length,
  workerTests: top.filter(e => /Worker|Lease|Retry|Rescue|Dispatch|Cancel|Redrive|Schedule|Calendar|Receipt|Outbox|Claim|Heartbeat|Shutdown|Scope|Workflow|Screening|Scoring/.test(e.Test)).map(e => ({package: e.Package, test: e.Test})),
};
report.regressionPassed = report.fullModuleRun && expectedTests.length > 0 && report.missingTests.length === 0 && report.unexplainedRunFailures.length === 0 && report.testsExit === 0 && report.vetExit === 0 && top.length > 0 && report.failed.length === 0 && report.unexpectedSkips.length === 0 && report.raceWarnings === 0;
report.allTestsExecuted = skipped.length === 0;
writeFileSync(resolve(directory, 'report.json'), JSON.stringify(report, null, 2) + '\n');
console.log(JSON.stringify(report, null, 2));
process.exitCode = report.regressionPassed ? 0 : 1;
