import {readFileSync, writeFileSync} from 'node:fs';
import {resolve} from 'node:path';

const report = JSON.parse(readFileSync(resolve(process.argv[2], 'report.json'), 'utf8'));
const names = [...new Set(report.missingTests.map(name => name.slice(name.lastIndexOf(':') + 1)))];
if (!names.length || names.some(name => !/^Test\w+$/.test(name))) throw new Error('A nonempty bounded test inventory is required');
const destination = resolve(process.argv[3]);
writeFileSync(destination, '^(' + names.join('|') + ')$\n');
console.log(JSON.stringify({expected: report.expectedTopLevelTests, complete: report.topLevelPassed, remaining: names.length, destination}));
