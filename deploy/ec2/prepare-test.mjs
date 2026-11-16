// Isolated transport fixtures only: no application database or credentials.
import { mkdirSync, readFileSync, writeFileSync, chmodSync } from 'node:fs';
import { renderGateway } from './configure.mjs';
mkdirSync('.local/ec2-tests', { recursive: true });
writeFileSync('.local/ec2-tests/nginx.conf', renderGateway(readFileSync('docker/gateway/nginx.conf', 'utf8'), 'fraud.example.test'));
const caddy = readFileSync('deploy/ec2/Caddyfile', 'utf8').replace('{', '{\n    local_certs\n    skip_install_trust');
writeFileSync('.local/ec2-tests/Caddyfile', caddy);
writeFileSync('.local/ec2-tests/postgres-password.txt', 'synthetic-contract-database-password-only\n', { mode: 0o600 });
chmodSync('.local/ec2-tests/postgres-password.txt', 0o600);
