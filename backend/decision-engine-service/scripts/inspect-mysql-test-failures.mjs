import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';

for (const directory of process.argv.slice(2)) {
  const events = readFileSync(resolve(directory, 'raw.jsonl'), 'utf8').split(/\r?\n/).filter(Boolean).map(JSON.parse);
  const failed = events.filter(e => e.Action === 'fail' && e.Test && !e.Test.includes('/'));
  console.log(JSON.stringify({directory, failed: failed.map(e => ({package: e.Package, test: e.Test}))}));
  for (const failure of failed) {
    const output = events.filter(e => e.Package === failure.Package && e.Output && (e.Test === failure.Test || e.Test?.startsWith(failure.Test + '/'))).map(e => e.Output).join('');
    console.log(JSON.stringify({test: failure.Test, output: output.slice(-5000)}));
  }
}
