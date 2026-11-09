import {spawnSync} from 'node:child_process';

const directories = ['2026-11-06T22-09-14Z', '2026-11-07T07-26-37Z', '2026-11-07T07-38-40Z', '2026-11-07T08-06-30Z'].map(name => 'evidence/mysql-integration/' + name);
const result = spawnSync(process.execPath, ['scripts/summarize-mysql-tests.mjs', ...directories], {encoding: 'utf8', maxBuffer: 8 << 20});
if (result.error) throw result.error;
const report = JSON.parse(result.stdout);
console.log(JSON.stringify({passed: report.passed, topLevelPassed: report.topLevelPassed, testAndSubtestPasses: report.testAndSubtestPasses,
  expectedTopLevelTests: report.expectedTopLevelTests, missing: report.missingTests.length, failed: report.failed.length,
  skipped: report.skipped.length, raceWarnings: report.raceWarnings}));
process.exitCode = result.status;
