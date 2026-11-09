import {existsSync, readFileSync, writeFileSync} from 'node:fs';
import {resolve} from 'node:path';

const directories = process.argv.slice(2).map(path => resolve(path));
const directory = directories.at(-1);
if (!process.argv[2]) throw new Error('Provide the evidence directory printed by test-mysql.sh');
const attempts = directories.map(directory => ({directory,
  selection: existsSync(resolve(directory, 'selection.txt')) ? readFileSync(resolve(directory, 'selection.txt'), 'utf8').trim() : 'all',
  testsExit: Number(readFileSync(resolve(directory, 'tests.exit'), 'utf8')),
  vetExit: Number(readFileSync(resolve(directory, 'vet.exit'), 'utf8')),
  events: readFileSync(resolve(directory, 'raw.jsonl'), 'utf8').split(/\r?\n/).filter(Boolean).map(JSON.parse),
}));
// A repair replaces the complete top-level outcome, including its subtests.
// Preserve every failed attempt and require a complete independently listed
// inventory before accepting a merged report.
const lastTop = new Map(), lastPackage = new Map();
attempts.forEach((attempt, index) => {
  for (const event of attempt.events) {
    if (!['pass', 'fail', 'skip'].includes(event.Action)) continue;
    if (!event.Test) lastPackage.set(event.Package, index);
    else if (!event.Test.includes('/')) lastTop.set(event.Package + ':' + event.Test, index);
  }
});
const events = attempts.flatMap((attempt, index) => attempt.events.filter(event => event.Test
  ? lastTop.get(event.Package + ':' + event.Test.split('/')[0]) === index
  : lastPackage.get(event.Package) === index));
const inventoryDirectory = directories.findLast(directory => existsSync(resolve(directory, 'inventory.jsonl')));
const inventory = inventoryDirectory ? readFileSync(resolve(inventoryDirectory, 'inventory.jsonl'), 'utf8').split(/\r?\n/).filter(Boolean).map(JSON.parse) : [];
const expected = [...new Set(inventory.flatMap(e => (e.Output ?? '').split('\n').filter(line => /^Test\w+$/.test(line)).map(test => e.Package + ':' + test)))];
const failed = events.filter(e => e.Action === 'fail').map(e => ({package: e.Package, test: e.Test ?? null}));
const skipped = events.filter(e => e.Action === 'skip' && e.Test).map(e => ({package: e.Package, test: e.Test}));
const tests = events.filter(e => e.Test && e.Action === 'pass');
const top = tests.filter(e => !e.Test.includes('/'));
const suite = prefix => top.filter(e => e.Test.startsWith(prefix)).map(e => e.Test);
const report = {
  reportedAt: new Date().toISOString(),
  selection: existsSync(resolve(directory, 'selection.txt')) ? readFileSync(resolve(directory, 'selection.txt'), 'utf8').trim() : 'all',
  testsExit: Number(readFileSync(resolve(directory, 'tests.exit'), 'utf8')),
  vetExit: Number(readFileSync(resolve(directory, 'vet.exit'), 'utf8')),
  packagesPassed: events.filter(e => !e.Test && e.Action === 'pass').map(e => e.Package),
  packagesWithoutTests: events.filter(e => !e.Test && e.Action === 'skip').map(e => e.Package),
  topLevelPassed: top.length, testAndSubtestPasses: tests.length, failed, skipped,
  processTests: suite('TestProcess'), sharedOwnerTests: suite('TestFullShared'), parityTests: suite('TestLiveParity'),
  raceWarnings: events.filter(e => e.Output?.includes('WARNING: DATA RACE')).length,
  expectedTopLevelTests: expected.length,
  missingTests: expected.filter(test => !lastTop.has(test)),
  attempts: attempts.map(({events, ...attempt}) => attempt),
  fullSuiteRun: attempts.some(attempt => attempt.selection === 'all'),
};
report.passed = report.testsExit === 0 && report.vetExit === 0 && top.length > 0 && failed.length === 0 && skipped.length === 0 && report.raceWarnings === 0;
if (directories.length > 1) report.passed &&= report.fullSuiteRun && expected.length > 0 && report.missingTests.length === 0;
writeFileSync(resolve(directory, 'report.json'), JSON.stringify(report, null, 2) + '\n');
console.log(JSON.stringify(report, null, 2));
process.exitCode = report.passed ? 0 : 1;
