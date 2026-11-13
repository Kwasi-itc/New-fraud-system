"""Offline deployment contracts; never connects to an application database."""
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("ec2setup", Path(__file__).with_name("setup.py"))
setup = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(setup)


class SetupTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="ec2-setup-test-")
        self.addCleanup(self.temporary.cleanup)
        self.state = Path(self.temporary.name) / "state"
        self.state.mkdir()
        self.args = SimpleNamespace(public_ip="192.0.2.10", db_host="test.cluster-test.us-east-1.rds.amazonaws.com",
            db_port=3306, db_user="admin", db_client_ip="10.0.1.10", tenant_id=None, object_type="transactions",
            render_only=False, check_only=False, db_password_file=None,
            db_password_stdin=False, db_ca_file=None)

    def test_port_overrides_and_collisions(self):
        ports = setup.selected_ports(["gateway=18088"])
        self.assertEqual(ports, {"gateway": 18088})
        for values in (["frontend=8080"], ["gateway=1"], ["bad=3000"], ["gateway=3003", "gateway=3004"], ["gateway=oops"]):
            with self.assertRaises(ValueError):
                setup.selected_ports(values)

    def test_private_atomic_files_preserve_credentials(self):
        setup.atomic_text(self.state / "private", "secret")
        self.assertEqual((self.state / "private").stat().st_mode & 0o777, 0o600)
        first = setup.retained_secret(self.state, "token")
        self.assertEqual(first, setup.retained_secret(self.state, "token"))
        self.assertEqual(list(self.state.glob(".setup-*")), [])
        (self.state / "token.txt").write_text("invalid")
        with self.assertRaises(ValueError):
            setup.retained_secret(self.state, "token")

    def test_prepare_preserves_passwords_keys_and_secrets(self):
        ca = self.state.parent / "ca.pem"
        subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
            "-subj", "/CN=offline-test", "-keyout", str(self.state.parent / "key.pem"), "-out", str(ca)],
            check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        self.args.db_ca_file = ca
        with patch.object(setup.getpass, "getpass", return_value=" spaces $ and ' symbols "):
            setup.prepare_files(self.args, self.state, setup.DEFAULT_PORTS)
        names = ["root-password.txt", "auth-private.pem", "auth-public.pem", "auth-invitation-key.txt",
                 *[name + ".txt" for name in setup.SECRET_FILES]]
        saved = {name: (self.state / name).read_bytes() for name in names}
        setup.prepare_files(self.args, self.state, setup.DEFAULT_PORTS)
        self.assertEqual(saved, {name: (self.state / name).read_bytes() for name in names})
        self.assertEqual(self.state.stat().st_mode & 0o777, 0o700)
        self.assertEqual((self.state / "root-password.txt").read_text(), " spaces $ and ' symbols ")
        self.assertNotIn("spaces", (self.state / "frontend.env").read_text())
        self.assertEqual(list(self.state.rglob(".setup-*")), [])

    def test_cli_render_validates_complete_configuration_without_daemon(self):
        ca = self.state.parent / "ca.pem"
        subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
            "-subj", "/CN=offline-test", "-keyout", str(self.state.parent / "key.pem"), "-out", str(ca)],
            check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        command = [sys.executable, str(Path(__file__).with_name("setup.py")), "--public-ip", self.args.public_ip,
            "--db-host", self.args.db_host, "--db-user", "admin", "--db-client-ip", self.args.db_client_ip,
            "--db-password-stdin", "--db-ca-file", str(ca), "--state-dir", str(self.state),
            "--port", "gateway=18088", "--render-only"]
        for _ in range(2):
            completed = subprocess.run(command, input="example-migration-password\n", capture_output=True, text=True, check=True)
            self.assertIn("no database was contacted", completed.stdout)
            self.assertNotIn("example-migration-password", completed.stdout + completed.stderr)
        self.assertEqual((self.state / "root-password.txt").read_text(), "example-migration-password")
        self.assertEqual(list(self.state.rglob(".setup-*")), [])

    def test_interrupted_test_model_completes_missing_fields(self):
        (self.state / "data-model-service-token.txt").write_text("token")
        self.args.tenant_id = "tenant"
        calls = []
        def fake(url, method="GET", body=None, token=None):
            calls.append((url, method, body))
            if method == "POST":
                return {"field": {"id": "new"}}
            if url.endswith("/tenants/tenant"):
                return {"tenant": {"status": "active", "external_key": "ec2-http-test"}}
            if url.endswith("/tables"):
                return {"tables": [{"id": "table", "name": "transactions"}]}
            if url.endswith("/fields"):
                return {"fields": [{"name": "amount"}]}
            self.fail(url)
        with patch.object(setup, "api", side_effect=fake):
            setup.prepare_tenant(self.args, self.state, setup.DEFAULT_PORTS)
        writes = [call for call in calls if call[1] == "POST"]
        self.assertEqual(len(writes), 1)
        self.assertEqual(writes[0][2]["name"], "currency")

    def test_failed_preflight_cannot_stop_or_migrate_services(self):
        calls = []
        def compose(state, *args, **kwargs):
            calls.append(args)
            if args == ("run", "--rm", "--no-deps", "preflight"):
                raise RuntimeError("preflight failed")
        with patch.object(setup, "compose", side_effect=compose), patch.object(setup, "check_ports"):
            with self.assertRaises(RuntimeError):
                setup.deploy(self.args, self.state, setup.DEFAULT_PORTS)
        self.assertFalse(any(call[0] in ("stop", "up") or "--initialize" in call for call in calls))

    def test_check_only_is_read_only(self):
        self.args.check_only = True
        with patch.object(setup, "compose") as compose, patch.object(setup, "check_ports"):
            setup.deploy(self.args, self.state, setup.DEFAULT_PORTS)
        self.assertFalse(any(call.args[1] in ("stop", "up") or "--initialize" in call.args for call in compose.call_args_list))

    def test_real_compose_render_has_no_local_application_database(self):
        ports = setup.selected_ports(["gateway=18088"])
        model = setup.render(self.args, self.state, ports)
        self.assertNotIn("mysql", model["services"])
        self.assertNotIn("mysql_dev_data", model["volumes"])
        self.assertNotIn("ports", model["services"]["mysql-reference"])
        self.assertEqual(model["services"]["mysql-reference"]["tmpfs"], ["/var/lib/mysql"])
        self.assertFalse(any(volume.get("target") == "/var/lib/mysql" for volume in model["services"]["mysql-reference"].get("volumes", [])))
        self.assertEqual(model["services"]["mysql-reference"]["environment"]["MYSQL_ROOT_HOST"], "%")
        self.assertEqual(model["services"]["mysql-reference"]["secrets"], ["ec2_reference_password"])
        self.assertEqual(model["services"]["mysql-reference"]["environment"]["MYSQL_ROOT_PASSWORD_FILE"], "/run/secrets/ec2_reference_password")
        health = model["services"]["keycloak"]["healthcheck"]["test"][3]
        self.assertIn("HTTP/1.0\\r\\nHost:", health)
        self.assertNotIn("\\\\r", health)
        published = [(name, service["ports"]) for name, service in model["services"].items() if service.get("ports")]
        self.assertEqual(published, [("api-gateway", [{"target": 8080, "published": "18088", "host_ip": "0.0.0.0", "protocol": "tcp"}])])
        self.assertNotIn("provider-postgres", model["services"])
        self.assertNotIn("provider_data", model["volumes"])
        provider = model["services"]["keycloak"]["environment"]
        self.assertEqual(provider["KC_DB"], "mysql")
        self.assertEqual(provider["KC_DB_USERNAME"], "fraud_ec2_keycloak")
        self.assertIn("sslMode=VERIFY_IDENTITY", provider["KC_DB_URL"])
        self.assertEqual(provider["KC_TRUSTSTORE_PATHS"], "/ec2/db-ca.pem")
        self.assertEqual(provider["KC_HOSTNAME"], "http://192.0.2.10:18088/identity")
        self.assertEqual(provider["KC_HTTP_RELATIVE_PATH"], "/identity")
        self.assertEqual(provider["KC_HTTP_MANAGEMENT_RELATIVE_PATH"], "/")
        for secret in model["secrets"].values():
            self.assertTrue(secret["file"].startswith(str(self.state) + "/"))
        for name, service in model["services"].items():
            if name not in ("mysql-reference", "auth-mail", "frontend", "keycloak", "api-gateway"):
                self.assertEqual(service["environment"]["EC2_MYSQL_TEST_CONFIG"], "/ec2/ec2.json")
            self.assertFalse(any(v.get("target") in ("/target-socket", "/reference-socket") for v in service.get("volumes", [])))
        subprocess.run(["docker", "compose", "--profile", "install", "--env-file", str(self.state / "compose.env"),
            "-f", str(self.state / "compose.json"), "config", "--no-env-resolution", "--quiet"], check=True)
        self.assertNotIn(str(setup.REPO / ".local/mysql-dev"), json.dumps(model))

    def test_legacy_identity_is_rejected_without_rewriting_state(self):
        legacy = {"public_ip": self.args.public_ip, "auth_port": 8087, "keycloak_port": 8090}
        setup.write_json(self.state / "deployment.json", legacy)
        before = (self.state / "deployment.json").read_bytes()
        with self.assertRaisesRegex(ValueError, "explicit realm"):
            setup.verify_identity(self.state, setup.deployment_identity(self.args, setup.DEFAULT_PORTS))
        self.assertEqual((self.state / "deployment.json").read_bytes(), before)

    def test_gateway_identity_and_issuer_are_stable_on_restarts(self):
        identity = setup.deployment_identity(self.args, setup.DEFAULT_PORTS)
        setup.write_json(self.state / "deployment.json", identity)
        setup.verify_identity(self.state, identity)
        self.assertEqual(setup.provider_origin(self.args.public_ip, setup.DEFAULT_PORTS), "http://192.0.2.10:8088/identity")
        with self.assertRaises(ValueError):
            setup.verify_identity(self.state, setup.deployment_identity(self.args, {"gateway": 18088}))

    def test_deploy_initializes_provider_before_keycloak_and_verifies_gateway(self):
        calls = []
        def compose(state, *args, **kwargs):
            calls.append(args)
        with patch.object(setup, "compose", side_effect=compose), patch.object(setup, "check_ports"), patch.object(setup, "configure_provider"), patch.object(setup, "wait_model"), patch.object(setup, "prepare_tenant"), patch.object(setup, "render"), patch.object(setup, "bootstrap_user"), patch.object(setup, "verify_gateway") as verify:
            setup.deploy(self.args, self.state, setup.DEFAULT_PORTS)
        initialize = next(i for i, args in enumerate(calls) if "--initialize-keycloak" in args)
        provider = next(i for i, args in enumerate(calls) if args[0] == "up" and "keycloak" in args)
        self.assertLess(initialize, provider)
        self.assertTrue(any(args[0] == "up" and "api-gateway" in args for args in calls))
        self.assertFalse(any("provider-postgres" in args for args in calls))
        verify.assert_called_once()

    def test_gateway_verifier_rejects_extra_runtime_ports_before_http_requests(self):
        bindings = 'api-gateway\t{"8080/tcp":[{"HostIp":"0.0.0.0","HostPort":"8088"}]}\nkeycloak\t{"8080/tcp":[{"HostIp":"0.0.0.0","HostPort":"8090"}]}'
        with patch.object(setup, "compose", return_value="gateway provider"), patch.object(setup, "run", return_value=bindings), patch.object(setup.request, "urlopen") as http:
            with self.assertRaisesRegex(RuntimeError, "only the selected gateway"):
                setup.verify_gateway(self.args, self.state, setup.DEFAULT_PORTS)
        http.assert_not_called()

    def test_gateway_verifier_requires_identity_urls_on_the_public_gateway(self):
        issuer = "http://192.0.2.10:8088/identity/realms/fraud"
        discovery = {"issuer": issuer, **{key: issuer + "/endpoint" for key in ("authorization_endpoint", "token_endpoint", "jwks_uri", "end_session_endpoint")}}
        bindings = 'api-gateway\t{"8080/tcp":[{"HostIp":"0.0.0.0","HostPort":"8088"}]}'
        with patch.object(setup, "compose", return_value="gateway"), patch.object(setup, "run", return_value=bindings), patch.object(setup.request, "urlopen") as http, patch.object(setup, "api", return_value=discovery):
            http.return_value.__enter__.return_value.status = 200
            setup.verify_gateway(self.args, self.state, setup.DEFAULT_PORTS)
            self.assertEqual(http.call_count, 13)
            discovery["token_endpoint"] = "http://192.0.2.10:8090/realms/fraud/token"
            with self.assertRaisesRegex(RuntimeError, "public gateway identity"):
                setup.verify_gateway(self.args, self.state, setup.DEFAULT_PORTS)


if __name__ == "__main__":
    unittest.main()
