import {readFileSync} from 'node:fs';
import {spawnSync} from 'node:child_process';

const previous = JSON.parse(readFileSync('evidence/mysql-integration/2026-11-06T15-25-25Z/report.json', 'utf8'));
const directories = [...previous.attempts.map(attempt => attempt.directory),
  'evidence/mysql-integration/2026-11-06T21-53-50Z',
  'evidence/mysql-integration/2026-11-06T22-10-30Z',
  'evidence/mysql-integration/2026-11-07T07-30-55Z'];
const result = spawnSync(process.execPath, ['scripts/summarize-mysql-tests.mjs', ...directories], {encoding: 'utf8', maxBuffer: 16 << 20});
if (result.error) throw result.error;
if (result.status !== 0) throw new Error(result.stderr || result.stdout);
const report = JSON.parse(result.stdout);
console.log(JSON.stringify({regressionPassed: report.regressionPassed, topLevelPassed: report.topLevelPassed,
  testAndSubtestPasses: report.testAndSubtestPasses, expectedTopLevelTests: report.expectedTopLevelTests,
  missing: report.missingTests.length, failures: report.failed.length, skips: report.skipped.length, raceWarnings: report.raceWarnings}));
