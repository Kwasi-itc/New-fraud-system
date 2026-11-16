import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
from unittest.mock import patch

repo = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location('setup_mac', repo / 'docker/mysql-dev/setup-mac.py')
setup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(setup)
docker = shutil.which('docker')
services = ('data-model-service', 'ingestion-service', 'decision-engine-service',
            'auth-service', 'case-manager-service', 'screening-service')

with tempfile.TemporaryDirectory(prefix='mysql-mac-validation-') as directory:
    fixture = Path(directory)
    files = ['docker-compose.yml', 'compose.mysql-dev.yaml',
             'backend/auth-service/compose.provider-limits.yaml',
             'backend/auth-service/scripts/setup-local-provider.mjs',
             'frontend/docker-compose-mysql.yml']
    for name in ('configure-mysql-login.mjs', 'prepare-mysql.mjs', 'configure-mysql-storage.mjs'):
        files.append('frontend/scripts/' + name)
    for service in services:
        files.append(f'backend/{service}/docker-compose-mysql.yml')
    for service in services[3:]:
        files.append(f'backend/{service}/scripts/prepare-mysql.mjs')
    for name in files:
        target = fixture / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(repo / name, target)
    local = fixture / '.local/mysql-dev'
    local.mkdir(parents=True, mode=0o700)
    (local / 'ingestion-runtime-token.txt').write_text('a' * 64)
    (local / 'decision-runtime-token.txt').write_text('c' * 64)
    (local / 'root-password.txt').write_text('b' * 64)
    (local / 'compose.env').write_text('MYSQL_DEV_PORT=3307\n')
    bin_dir = fixture / 'bin'
    bin_dir.mkdir()
    stub = bin_dir / 'docker'
    stub.write_text('#!/bin/sh\n[ "$1 $2 $3" = "network inspect fraud-mysql-dev_default" ] || exit 99\n'
                    "printf '%s\\n' '[{\"IPAM\":{\"Config\":[{\"Subnet\":\"172.23.0.0/16\"}]}}]'\n")
    stub.chmod(0o755)
    env = dict(os.environ, PATH=str(bin_dir) + os.pathsep + os.environ['PATH'])
    commands = [fixture / 'backend/auth-service/scripts/setup-local-provider.mjs']
    commands += [fixture / f'backend/{service}/scripts/prepare-mysql.mjs' for service in services[3:]]
    for command in commands:
        subprocess.run(['node', str(command)], cwd=fixture, env=env, check=True, capture_output=True)
    retained = {path.relative_to(fixture): path.read_bytes()
                for path in (fixture / '.local').rglob('*') if path.is_file()}
    for command in commands:
        subprocess.run(['node', str(command)], cwd=fixture, env=env, check=True, capture_output=True)
    assert all((fixture / path).read_bytes() == value for path, value in retained.items())
    auth = json.loads((local / 'auth-runtime/config.json').read_text())
    assert auth['AccountHost'] == '172.23.0.0/255.255.0.0'
    assert auth['Provider']['BackchannelOrigin'] == 'http://host.docker.internal:8090'
    assert (local / 'auth-public.pem').read_bytes() == (local / 'case-runtime/auth-public.pem').read_bytes()
    assert (local / 'auth-public.pem').read_bytes() == (local / 'screening-runtime/auth-public.pem').read_bytes()
    print('Actual Node preparation helpers: initial setup and repeat-run preservation passed')

    wrapper = fixture / 'configure-login-fixture.mjs'
    wrapper.write_text('''
import assert from 'node:assert/strict';
import {readFileSync,writeFileSync,existsSync} from 'node:fs';
const path=new URL('./client.json',import.meta.url);
globalThis.fetch=async(url,options={})=>{
 const address=new URL(url);
 if(address.pathname==='/realms/master/protocol/openid-connect/token'){
  assert.equal(options.method,'POST');assert.equal(options.body.get('grant_type'),'password');
  return Response.json({access_token:'fixture-admin'});
 }
 assert.equal(options.headers.Authorization,'Bearer fixture-admin');
 if(address.pathname==='/admin/realms/fraud/clients'&&address.searchParams.get('clientId')==='fraud-auth'){
  return Response.json([JSON.parse(readFileSync(path,'utf8'))]);
 }
 assert.equal(address.pathname,'/admin/realms/fraud/clients/existing-client');
 assert.equal(options.method,'PUT');
 const updated=JSON.parse(options.body);assert.equal(updated.attributes.keep,'preserved');
 assert.ok(updated.redirectUris.includes('http://localhost:3000/api/auth/callback'));
 writeFileSync(path,options.body);
 return new Response(null,{status:204});
};
await import('./frontend/scripts/configure-mysql-login.mjs');
''')
    (fixture / 'client.json').write_text(json.dumps({'id': 'existing-client',
        'redirectUris': ['http://localhost:3000/api/auth/callback'],
        'attributes': {'keep': 'preserved'}}))
    credentials = {path.relative_to(local): path.read_bytes()
                   for path in local.glob('*.txt')}
    for _ in range(2):
        subprocess.run(['node', str(wrapper)], cwd=fixture, check=True, capture_output=True)
        subprocess.run(['node', str(fixture / 'frontend/scripts/prepare-mysql.mjs')],
                       cwd=fixture, check=True, capture_output=True)
    client = json.loads((fixture / 'client.json').read_text())
    assert client['redirectUris'].count('http://localhost:3002/api/auth/callback') == 1
    updated_auth = json.loads((local / 'auth-runtime/config.json').read_text())
    assert updated_auth['Provider']['RedirectURL'] == 'http://localhost:3002/api/auth/callback'
    assert updated_auth['InvitationOrigin'] == 'http://localhost:3002'
    assert all((local / name).read_bytes() == content for name, content in credentials.items())
    frontend_env = (local / 'frontend.env').read_text()
    assert 'APP_ORIGIN=http://localhost:3002' in frontend_env
    assert 'LOCAL_MYSQL_NO_AUTH' not in frontend_env and 'NEXT_PUBLIC_' not in frontend_env
    assert (local / 'frontend.env').stat().st_mode & 0o777 == 0o600
    stub.write_text('''#!/usr/bin/env python3
import json,sys
names={'fraud-mysql-dev-mysql-1','fraud-mysql-dev-mysql-reference-1'}
a=sys.argv[1:]
if len(a)==2 and a[0]=='inspect' and a[1] in names:
 print(json.dumps([{'Config':{'Image':'mysql:8.4.8','Labels':{'com.docker.compose.project':'fraud-mysql-dev'},'Env':['MYSQL_DATABASE=fraud']}}]))
elif a[:1]==['exec'] and a[1] in names and a[2:4]==['sh','-c']:
 assert len(a)==5 and 'SET PERSIST log_bin_trust_function_creators=ON' in a[4]
else:sys.exit(99)
''')
    subprocess.run(['node', str(fixture / 'frontend/scripts/configure-mysql-storage.mjs')],
                   cwd=fixture, env=env, check=True, capture_output=True)
    print('Frontend Node helpers: callback registration, repeat runs, credential preservation and storage command contracts passed')

    def config_only(*args, capture=False):
        args = list(args)
        args[0] = docker
        if 'up' in args:
            args = args[:args.index('up')] + ['config', '--quiet']
        result = subprocess.run(args, cwd=fixture, check=True, text=True, capture_output=True)
        return result.stdout.strip() if capture else None

    with patch.object(setup, 'REPO', fixture), patch.object(setup, 'LOCAL', local), \
         patch.object(setup, 'run', side_effect=config_only):
        setup.start_provider()
    provider = json.loads((local / 'provider.compose.json').read_text())
    assert set(provider['services']) == {'postgres', 'keycloak-db-init', 'keycloak'}
    print('Extracted Keycloak Compose configuration: passed; no container operations executed')

    compose_env = dict(os.environ, INGESTION_TENANT_ID='5c1ea673-693c-4bcb-adc8-eb3db2f2f868',
                       INGESTION_OBJECT_TYPE='transactions',
                       DECISION_TENANT_ID='5c1ea673-693c-4bcb-adc8-eb3db2f2f868',
                       DECISION_MYSQL_SUBNET='172.23.0.0/16')
    for service in services:
        subprocess.run([docker, 'compose', '--project-name', 'fraud-mysql-dev', '--profile', 'install',
                        '--env-file', str(local / 'compose.env'),
                        '-f', str(fixture / f'backend/{service}/docker-compose-mysql.yml'),
                        'config', '--quiet'], cwd=fixture, env=compose_env, check=True, capture_output=True)
    print('All six MySQL Compose configurations: passed')
    subprocess.run([docker, 'compose', '--project-name', 'fraud-mysql-dev',
                    '--env-file', str(local / 'compose.env'), '-f',
                    str(fixture / 'frontend/docker-compose-mysql.yml'), 'config', '--quiet'],
                   cwd=fixture, check=True, capture_output=True)
    print('Authenticated frontend Compose configuration: passed')
