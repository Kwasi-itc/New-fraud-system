import assert from 'node:assert/strict';
import {readFileSync, writeFileSync} from 'node:fs';
import {resolve} from 'node:path';

const directory = resolve(process.argv[2]);
assert.match(process.argv[3] ?? '', /^\d+$/, 'Supply the actual worker test container exit code');
const exitCode = Number(process.argv[3]);
const raw = readFileSync(resolve(directory, 'component-tests.jsonl'), 'utf8').replace(/^\uFEFF/, '');
const events = raw.split(/\r?\n/).filter(line => line.startsWith('{')).map(line => JSON.parse(line));
const completed = events.filter(e => e.Test && ['pass', 'fail', 'skip'].includes(e.Action));
const native = 'TestPhysicalCreationAllocated/service_allocated_native_index_worker';
const critical = [
  'TestIndexDeliveryStore', 'TestIndexDeliveryLostCommitAcknowledgement',
  'TestLeasedIndexExecution', 'TestMySQLDeliveryPolling', 'TestMySQLDeliveryMaintenance',
  'TestDeliveryRetryErrorHistory', 'TestDeliveryInspection', 'TestDeliveryRedrive',
  'TestPublishedNativeIndexCompletion', 'TestPhysicalReconciliationPages',
  'TestRecoveryCommandMySQL', 'TestInspectionCommandMySQL', 'TestRedriveCommandMySQL',
  'TestLeaseSupervisor', 'TestDeliveryPoller', 'TestMaintenanceScheduler',
  native, ...['_audit_failure', '_denied', '_expired', '_text', '_foreign_tenant',
    '_other_pool', '_completion_denied', '_publication_expired', '_navigation', '_repair', '_decision'].map(suffix => native + suffix),
];
const criticalResults = critical.map(test => ({test, passed: completed.some(e => e.Test === test && e.Action === 'pass')}));
const failed = completed.filter(e => e.Action === 'fail').map(e => ({package: e.Package, test: e.Test}));
const skipped = completed.filter(e => e.Action === 'skip').map(e => ({package: e.Package, test: e.Test}));
const componentPassed = exitCode === 0 && failed.length === 0 && skipped.length === 0 &&
  criticalResults.every(r => r.passed) && !raw.includes('WARNING: DATA RACE');
const runtime = JSON.parse(readFileSync(resolve(directory, 'runtime-probe.json'), 'utf8'));
const report = {
  componentPassed, testContainerExitCode: exitCode,
  topLevelTestsPassed: completed.filter(e => e.Action === 'pass' && !e.Test.includes('/')).length,
  testsAndSubtestsPassed: completed.filter(e => e.Action === 'pass').length,
  packageRunsPassed: events.filter(e => e.Action === 'pass' && !e.Test).length,
  failed, skipped, criticalResults,
  raceWarnings: raw.includes('WARNING: DATA RACE'),
  liveRuntimePassed: runtime.passed,
  liveJob: runtime.job, liveJobStatus: runtime.jobStatus, physicalIndexRows: runtime.physicalIndexRows,
  workersComplete: false,
  openWork: [
    'MySQL queue execution host, lease supervision and maintenance are not deployed',
    'String search, navigation and repair execution strategies are unsupported',
    'Trusted work/reference loading, runtime identity, reviewed grants and host resource settings need composition',
    'Real process interruption/restart and full host acceptance remain unverified',
  ],
  scope: 'Focused worker component acceptance. The decision journey uses disposable MySQL target objects and PostgreSQL reference objects. This does not include the full schema-manifest regression.',
};
writeFileSync(resolve(directory, 'report.json'), JSON.stringify(report, null, 2) + '\n');
console.log(JSON.stringify({componentPassed, topLevelTestsPassed: report.topLevelTestsPassed,
  testsAndSubtestsPassed: report.testsAndSubtestsPassed, failed, skipped,
  missingCritical: criticalResults.filter(r => !r.passed), liveRuntimePassed: runtime.passed, workersComplete: false}, null, 2));
process.exitCode = componentPassed ? 0 : 1;
