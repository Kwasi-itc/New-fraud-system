import argparse
import contextlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from urllib.error import HTTPError


SCRIPT = Path(__file__).resolve().parents[1] / 'setup-mac.py'
spec = importlib.util.spec_from_file_location('setup_mac', SCRIPT)
setup_mac = importlib.util.module_from_spec(spec)
spec.loader.exec_module(setup_mac)
TENANT = '5c1ea673-693c-4bcb-adc8-eb3db2f2f868'
TABLE = '12345678-1234-4234-8234-123456789abc'


class SetupTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.local = Path(self.directory.name)
        self.scope = patch.object(setup_mac, 'LOCAL', self.local)
        self.scope.start()
        self.addCleanup(self.scope.stop)
        self.calls = []
        self.tenant = None
        self.table = None
        self.fields = []
        self.writes = []
        self.args = argparse.Namespace(tenant_id=None, object_type=None, port=None,
                                       core_only=True, skip_provider_startup=False,
                                       frontend_only=False, skip_frontend=False)
        self.program_calls = []
        self.preparations = []
        self.provider_starts = []
        self.frontend_starts = []

    def model(self, path, method='GET', body=None):
        if method == 'POST':
            self.writes.append((path, body))
        if path == '/v1/tenants':
            if method == 'POST':
                self.tenant = dict(body, id=TENANT, status='pending')
                return {'tenant': self.tenant.copy()}
            return {'tenants': [self.tenant] if self.tenant else None}
        if path == f'/v1/tenants/{TENANT}':
            return {'tenant': self.tenant.copy()}
        if path.endswith('/provision'):
            self.tenant['status'] = 'active'
            return {}
        if path.endswith('/tables'):
            if method == 'POST':
                self.table = dict(body, id=TABLE, archived=False)
                return {'table': self.table}
            return {'tables': [self.table] if self.table else None}
        if path.endswith('/fields'):
            if method == 'POST':
                self.fields.append(body)
                return {}
            return {'fields': self.fields or None}
        raise AssertionError(path)

    def invoke(self, compose=None):
        def fake_run(*args, **kwargs):
            self.program_calls.append(args)
            if args[:3] == ('docker', 'network', 'inspect'):
                return '[{"IPAM":{"Config":[{"Subnet":"172.23.0.0/16"},{"Subnet":"fd00::/64"}]}}]'
            if args[:2] == ('docker', 'ps'):
                return 'auth-id auth-service\nscreen-id screening-worker\ncore-id ingestion-service'
            return ''
        with patch.object(setup_mac, 'model', side_effect=self.model), \
             patch.object(setup_mac, 'wait_api'), \
             patch.object(setup_mac, 'run', side_effect=fake_run), \
             patch.object(setup_mac, 'start_provider', side_effect=lambda: self.provider_starts.append(True)), \
             patch.object(setup_mac, 'setup_frontend', side_effect=lambda: self.frontend_starts.append(True)), \
             patch.object(setup_mac, 'prepare_service', side_effect=lambda service, files: self.preparations.append(service)), \
             patch.object(setup_mac, 'compose', side_effect=compose or (lambda *args, **kwargs: self.calls.append(args))):
            setup_mac.setup(self.args)

    def test_full_stack_install_order_and_case_refresh(self):
        self.args.core_only = False
        self.invoke()
        migrations = [call[-1] for call in self.calls if 'run' in call]
        self.assertEqual(migrations, ['migrate-ingestion', 'migrate-decisions', 'migrate-auth',
                                      'migrate-case', 'migrate-screening', 'migrate-case'])
        self.assertEqual(self.preparations, ['auth-service', 'case-manager-service',
                                             'screening-service', 'case-manager-service'])
        self.assertEqual(self.provider_starts, [True])
        self.assertEqual(self.frontend_starts, [True])
        self.assertIn(('docker', 'stop', 'auth-id', 'screen-id'), self.program_calls)
        # Added service stages must not recreate shared MySQL with root Compose defaults.
        self.assertFalse(any(call[0] in ('auth-service', 'case-manager-service', 'screening-service')
                             and 'mysql' in call for call in self.calls))

    def test_frontend_only_skips_core_installers_and_identity_creation(self):
        self.args.frontend_only = True
        self.invoke()
        self.assertEqual(self.frontend_starts, [True])
        self.assertEqual(self.calls, [])
        self.assertEqual(self.writes, [])
        self.assertEqual(self.provider_starts, [])
        self.assertEqual(list(self.local.iterdir()), [])

    def test_frontend_opt_out_keeps_six_backend_services(self):
        self.args.core_only = False
        self.args.skip_frontend = True
        self.invoke()
        self.assertEqual(self.frontend_starts, [])
        self.assertIn('screening-service', self.preparations)

    def test_provider_start_can_be_skipped(self):
        self.args.core_only = False
        self.args.skip_provider_startup = True
        self.invoke()
        self.assertEqual(self.provider_starts, [])
        self.assertIn('auth-service', self.preparations)

    def test_failed_auth_migration_prevents_dependent_installs(self):
        self.args.core_only = False
        def compose(*args, **kwargs):
            self.calls.append(args)
            if 'run' in args and args[-1] == 'migrate-auth':
                raise RuntimeError('auth migration failed')
        with self.assertRaisesRegex(RuntimeError, 'auth migration failed'):
            self.invoke(compose)
        self.assertNotIn('case-manager-service', self.preparations)
        self.assertNotIn('screening-service', self.preparations)
        self.assertEqual(self.frontend_starts, [])

    def frontend_fixture(self):
        names = ['compose.env', 'ingestion.env', 'decision.env', 'root-password.txt',
                 'auth-runtime/config.json', 'auth-proxy-token.txt', 'ingestion-runtime-token.txt',
                 'decision-runtime-token.txt', 'screening-service-token.txt', 'screening-operator-token.txt']
        repo = self.local / 'fixture-repo'
        (repo / '.local/auth-provider').mkdir(parents=True)
        (repo / '.local/auth-provider/keycloak.env').write_text('retained-provider-settings')
        for name in names:
            path = self.local / name
            path.parent.mkdir(exist_ok=True)
            path.write_text('retained-value')
        return repo

    def test_frontend_prepares_reloads_and_syncs_before_start(self):
        repo = self.frontend_fixture()
        events = []
        with patch.object(setup_mac, 'REPO', repo), \
             patch.object(setup_mac, 'wait_api'), \
             patch.object(setup_mac, 'run', side_effect=lambda *args, **kw: events.append(('run', args))), \
             patch.object(setup_mac, 'compose', side_effect=lambda *args, **kw: events.append(('compose', args))), \
             patch.object(setup_mac, 'frontend_compose', side_effect=lambda *args: events.append(('frontend', args))):
            setup_mac.setup_frontend()
        scripts = [Path(args[1]).name for kind, args in events if kind == 'run']
        self.assertEqual(scripts, ['configure-mysql-login.mjs', 'prepare-mysql.mjs', 'configure-mysql-storage.mjs'])
        grant = next(i for i, (_, args) in enumerate(events) if '--grant-local-browser' in args)
        reload = next(i for i, (_, args) in enumerate(events) if '--force-recreate' in args)
        sync = next(i for i, (_, args) in enumerate(events) if '--sync-local-consumers' in args)
        build = next(i for i, (kind, args) in enumerate(events) if kind == 'frontend' and 'build' in args)
        self.assertLess(grant, reload)
        self.assertLess(reload, sync)
        self.assertLess(sync, build)
        self.assertEqual(events[-1][0], 'frontend')
        self.assertIn('--wait', events[-1][1])
        self.assertFalse(any('mysql' in args or 'mysql-reference' in args for _, args in events))
        self.assertEqual((self.local / 'auth-proxy-token.txt').read_text(), 'retained-value')
        self.assertEqual((self.local / 'auth-proxy-token.txt').stat().st_mode & 0o777, 0o644)

    def test_frontend_failure_prevents_build_and_start(self):
        repo = self.frontend_fixture()
        def compose(*args, **kw):
            if '--sync-local-consumers' in args:
                raise RuntimeError('grant synchronization failed')
        with patch.object(setup_mac, 'REPO', repo), patch.object(setup_mac, 'wait_api'), \
             patch.object(setup_mac, 'run'), patch.object(setup_mac, 'compose', side_effect=compose), \
             patch.object(setup_mac, 'frontend_compose') as frontend:
            with self.assertRaisesRegex(RuntimeError, 'grant synchronization failed'):
                setup_mac.setup_frontend()
        frontend.assert_not_called()

    def test_frontend_missing_backend_files_rejects_before_mutations(self):
        with patch.object(setup_mac, 'run') as run, patch.object(setup_mac, 'compose') as compose:
            with self.assertRaisesRegex(ValueError, 'Complete backend setup first'):
                setup_mac.setup_frontend()
        run.assert_not_called()
        compose.assert_not_called()

    def test_frontend_compose_uses_shared_project_and_runtime_environment(self):
        with patch.object(setup_mac, 'run') as run:
            setup_mac.frontend_compose('build', 'frontend')
        args = run.call_args.args
        self.assertIn('fraud-mysql-dev', args)
        self.assertIn(str(self.local / 'compose.env'), args)
        self.assertIn(str(setup_mac.REPO / 'frontend/docker-compose-mysql.yml'), args)

    def test_readiness_accepts_auth_204_and_screening_empty_200(self):
        class Response(io.BytesIO):
            def __init__(self, status, data=b''):
                super().__init__(data)
                self.status = status
        with patch.object(setup_mac.request, 'urlopen', return_value=Response(204)):
            setup_mac.wait_api(8087, status=204, json_body=False)
        with patch.object(setup_mac.request, 'urlopen', return_value=Response(200)):
            setup_mac.wait_api(8085, json_body=False)
        with patch.object(setup_mac.request, 'urlopen', return_value=Response(200, b'{"status":"ready"}')):
            setup_mac.wait_api(8086)

    def test_prepare_permissions_keep_host_directory_private(self):
        (self.local / 'auth-runtime').mkdir(mode=0o700)
        secret = self.local / 'auth-private.pem'
        config = self.local / 'auth-runtime/config.json'
        secret.write_text('retained-key')
        config.write_text('{}')
        secret.chmod(0o600)
        with patch.object(setup_mac, 'run'):
            setup_mac.prepare_service('auth-service', ['auth-private.pem', 'auth-runtime/config.json'])
        self.assertEqual(self.local.stat().st_mode & 0o777, 0o700)
        self.assertEqual(secret.stat().st_mode & 0o777, 0o644)
        self.assertEqual(config.parent.stat().st_mode & 0o777, 0o755)
        self.assertEqual(secret.read_text(), 'retained-key')

    def test_provider_extraction_preserves_storage_without_legacy_apps(self):
        source = {'name': 'existing-provider-project', 'services': {
            'postgres': {'image': 'postgres:16'},
            'keycloak-db-init': {'depends_on': {'postgres': {'condition': 'service_healthy'}}},
            'keycloak': {'depends_on': {'keycloak-db-init': {'condition': 'service_completed_successfully'}},
                         'healthcheck': {'test': ['CMD', 'original-readiness-check'],
                                         'start_period': '90s', 'retries': 20}},
            'auth-service': {'environment': {'UNRELATED': '${MANDATORY:?missing}'}},
        }, 'volumes': {'postgres_data': {'name': 'existing-provider-project_postgres_data'}},
            'networks': {'default': {'name': 'existing-provider-network'}}}
        with patch.object(setup_mac, 'run', side_effect=[json.dumps(source), None]) as run:
            setup_mac.start_provider()
        provider = json.loads((self.local / 'provider.compose.json').read_text())
        self.assertEqual(set(provider['services']), {'postgres', 'keycloak-db-init', 'keycloak'})
        self.assertEqual(provider['volumes'], source['volumes'])
        self.assertEqual(provider['networks'], source['networks'])
        self.assertEqual(provider['name'], source['name'])
        self.assertIn('--no-interpolate', run.call_args_list[0].args)
        self.assertIn('--no-env-resolution', run.call_args_list[0].args)
        health = provider['services']['keycloak']['healthcheck']
        self.assertEqual(health['test'], ['CMD', 'original-readiness-check'])
        self.assertEqual(health['start_period'], '10m')
        self.assertEqual(health['retries'], 20)
        args = run.call_args_list[-1].args
        self.assertEqual(args[args.index('--wait-timeout') + 1], '900')

    def test_provider_without_readiness_check_is_rejected(self):
        source = {'name': 'provider', 'services': {
            'postgres': {}, 'keycloak-db-init': {}, 'keycloak': {},
        }}
        with patch.object(setup_mac, 'run', return_value=json.dumps(source)) as run:
            with self.assertRaisesRegex(ValueError, 'readiness check changed'):
                setup_mac.start_provider()
        self.assertEqual(run.call_count, 1)

    def test_new_profiles_and_mailbox_startup(self):
        with patch.object(setup_mac, 'run') as run:
            setup_mac.compose('auth-service', ['compose.env'], 'build', 'migrate-auth', install=True)
        self.assertIn('--profile', run.call_args.args)
        self.assertIn('install', run.call_args.args)
        with patch.object(setup_mac, 'compose') as compose:
            setup_mac.install_service('auth-service', 'migrate-auth', ('auth-service','auth-worker'), auxiliary=('auth-mail',))
        self.assertNotIn('auth-mail', compose.call_args_list[0].args)
        self.assertIn('auth-mail', compose.call_args_list[-1].args)

    def test_fresh_then_repeat_preserves_secrets_tenant_and_fields(self):
        self.invoke()
        originals = {p.name: p.read_bytes() for p in self.local.glob('*.txt')}
        self.assertEqual(len(originals), 11)
        self.assertEqual([f['name'] for f in self.fields], ['amount', 'currency'])
        self.assertEqual(self.tenant['status'], 'active')
        self.assertIn('DECISION_MYSQL_SUBNET=172.23.0.0/16', (self.local / 'decision.env').read_text())
        writes = len(self.writes)
        self.invoke()
        self.assertEqual(len(self.writes), writes)
        self.assertEqual({p.name: p.read_bytes() for p in self.local.glob('*.txt')}, originals)
        self.assertFalse(any('down' in call or '--volumes' in call for call in self.calls))

    def test_invalid_retained_secret_stops_before_docker(self):
        (self.local / 'root-password.txt').write_text('invalid')
        with self.assertRaisesRegex(ValueError, 'Invalid retained secret'):
            self.invoke()
        self.assertEqual(self.calls, [])
        self.assertEqual((self.local / 'root-password.txt').read_text(), 'invalid')

    def test_migration_failure_prevents_consumer_restart_and_retains_scope(self):
        def compose(*args):
            self.calls.append(args)
            if 'run' in args and 'migrate-ingestion' in args:
                raise RuntimeError('installer failed')
        with self.assertRaisesRegex(RuntimeError, 'installer failed'):
            self.invoke(compose)
        self.assertIn(TENANT, (self.local / 'ingestion.env').read_text())
        self.assertFalse(any('--wait' in call for call in self.calls))
        self.assertFalse((self.local / 'decision.env').exists())

    def test_existing_custom_model_is_not_augmented(self):
        self.args.tenant_id = TENANT
        self.tenant = {'id': TENANT, 'status': 'active', 'external_key': 'custom'}
        self.table = {'id': TABLE, 'name': 'transactions', 'archived': False}
        self.invoke()
        self.assertEqual(self.writes, [])

    def test_saved_port_preserved_and_explicit_override(self):
        (self.local / 'compose.env').write_text('MYSQL_DEV_PORT=3310\n')
        self.invoke()
        self.assertIn('3310', (self.local / 'compose.env').read_text())
        self.args.port = 3311
        self.invoke()
        self.assertIn('3311', (self.local / 'compose.env').read_text())

    def test_invalid_scope_is_rejected_before_docker(self):
        self.args.object_type = 'bad$(command)'
        with self.assertRaisesRegex(ValueError, 'Object type'):
            self.invoke()
        self.assertEqual(self.calls, [])
        self.assertEqual(list(self.local.glob('*.txt')), [])

    def test_env_file_is_data_and_duplicate_keys_rejected(self):
        path = self.local / 'test.env'
        path.write_text('VALUE=$(do-not-execute)\n')
        self.assertEqual(setup_mac.read_env(path)['VALUE'], '$(do-not-execute)')
        path.write_text('VALUE=one\nVALUE=two\n')
        with self.assertRaisesRegex(ValueError, 'duplicate'):
            setup_mac.read_env(path)

    def test_http_retries_only_503(self):
        failure = HTTPError('http://localhost', 503, 'busy', {}, None)
        with patch.object(setup_mac.request, 'urlopen', side_effect=failure) as call, \
             patch.object(setup_mac.time, 'sleep'):
            with self.assertRaisesRegex(RuntimeError, '503'):
                setup_mac.http('http://localhost', '/', 'POST', {})
            self.assertEqual(call.call_count, 8)
        failure = HTTPError('http://localhost', 403, 'denied', {}, None)
        with patch.object(setup_mac.request, 'urlopen', side_effect=failure) as call:
            with self.assertRaisesRegex(RuntimeError, '403'):
                setup_mac.http('http://localhost', '/', 'POST', {})
            self.assertEqual(call.call_count, 1)


if __name__ == '__main__':
    unittest.main()
