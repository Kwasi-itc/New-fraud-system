// Keep the existing entry point; use Python's standard HTML parser.
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const script = fileURLToPath(new URL('./crawl_mmdces.py', import.meta.url));
const result = spawnSync(process.env.SCREENING_PYTHON || 'python', [script, ...process.argv.slice(2)], {
  stdio: 'inherit',
});
if (result.error) console.error(result.error.message);
process.exit(result.status ?? 1);
