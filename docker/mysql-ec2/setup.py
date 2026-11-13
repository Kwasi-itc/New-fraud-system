#!/usr/bin/env python3
"""EC2 HTTP test deployment with an external Aurora MySQL 8.4 writer.

Run on the Docker host. This profile is for restricted testing, not production
activation. All application services share the fraud database. Keycloak uses its
own database on the same Aurora writer. Only gateway port 8088 is published.
Runtime credentials remain in private state
files so restarts and encrypted invitation delivery retain their keys.
"""

import argparse
import base64
import fcntl
import getpass
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import socket
import ssl
import subprocess
import sys
import tempfile
import time
from urllib import error, parse, request
import uuid

REPO = Path(__file__).resolve().parents[2]
PROJECT = "fraud-ec2-test"
DEFAULT_PORTS = {"gateway": 8088}
SERVICES = {"data-model": "data-model-service", "ingestion": "ingestion-service",
            "decisions": "decision-engine-service", "screening": "screening-service",
            "cases": "case-manager-service", "auth": "auth-service"}
BACKEND_ROLES = ["data-model-author", "data-model-service", "data-model-worker",
                 "ingestion-service", "ingestion-worker", "decision-engine-service",
                 "decision-engine-worker", "decision-local-providers", "auth-service",
                 "auth-worker", "case-manager-service", "case-manager-worker",
                 "screening-service", "screening-worker"]
SECRET_FILES = ["data-model-password", "data-model-service-token", "ingestion-api-password",
                "ingestion-worker-password", "ingestion-runtime-token", "decision-api-password",
                "decision-worker-password", "decision-runtime-token", "decision-review-session",
                "decision-provider-password", "decision-provider-token", "auth-api-password",
                "auth-worker-password", "auth-proxy-token", "auth-oidc-client-secret",
                "case-api-password", "case-worker-password", "case-service-token",
                "case-publisher-token", "screening-api-password", "screening-worker-password",
                "screening-service-token", "screening-operator-token", "screening-intake-token",
                "keycloak-admin-password", "keycloak-db-password", "reference-root-password"]


def run(*args, capture=False):
    result = subprocess.run(args, cwd=REPO, check=True, text=True,
                            stdout=subprocess.PIPE if capture else None)
    return result.stdout.strip() if capture else None


def atomic_text(path, value, mode=0o600):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    fd, temporary = tempfile.mkstemp(prefix=".setup-", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            os.fchmod(stream.fileno(), mode)
            stream.write(value)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def write_json(path, value, mode=0o600):
    atomic_text(path, json.dumps(value, indent=2) + "\n", mode)


def retained_secret(state, name):
    path = state / (name + ".txt")
    if not path.exists():
        atomic_text(path, secrets.token_hex(32), 0o644)
    value = path.read_text()
    if not re.fullmatch(r"[a-f0-9]{64}", value):
        raise ValueError(f"Invalid retained {name}; restore the original secret rather than rotating silently")
    path.chmod(0o644)
    return value


def selected_ports(values):
    ports = DEFAULT_PORTS.copy()
    seen = set()
    for value in values:
        name, separator, port = value.partition("=")
        if not separator or name not in ports or name in seen or not port.isdecimal():
            raise ValueError("Use --port NAME=NUMBER once per service; names: " + ", ".join(ports))
        ports[name] = int(port)
        seen.add(name)
    if any(not 1024 <= value <= 65535 for value in ports.values()):
        raise ValueError("Published ports must be in 1024..65535")
    return ports


def origin(public_ip, ports, name=None):
    return f"http://{public_ip}:{ports['gateway']}"


def provider_origin(public_ip, ports):
    return origin(public_ip, ports) + "/identity"


def local_service(ports, name):
    return f"http://127.0.0.1:{ports['gateway']}/services/{name}"


def deployment_identity(args, ports):
    return {"version": 2, "public_ip": args.public_ip, "db_host": args.db_host,
            "db_port": args.db_port, "db_user": args.db_user,
            "db_client_ip": args.db_client_ip, "gateway_port": ports["gateway"],
            "provider_database": "aurora-mysql/keycloak"}


def verify_identity(state, identity):
    file = state / "deployment.json"
    if file.exists() and json.loads(file.read_text()) != identity:
        raise ValueError("Deployment identity differs from retained state. Legacy separate-port/PostgreSQL deployments require an explicit realm and application-identity migration; no configuration was rewritten or services stopped.")


def client_ip(address, port):
    # Inspect the host's selected outbound source address; no application query
    # or packet is sent. Docker's normal bridge egress uses this EC2 address.
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as connection:
        connection.connect((address, port))
        return connection.getsockname()[0]


def prepare_files(args, state, ports):
    state.mkdir(parents=True, exist_ok=True, mode=0o700)
    state.chmod(0o700)
    for directory in ("plans", "auth-runtime", "case-runtime", "screening-runtime",
                      "decision-runtime", "ingestion-runtime"):
        (state / directory).mkdir(exist_ok=True, mode=0o755)
        (state / directory).chmod(0o755)
    password_file = state / "root-password.txt"
    if args.db_password_file:
        password = args.db_password_file.read_text()
    elif args.db_password_stdin:
        password = sys.stdin.readline().removesuffix("\n").removesuffix("\r")
    elif password_file.is_file():
        password = password_file.read_text()
    else:
        password = getpass.getpass("Aurora migration account password: ")
    if not password or len(password.encode()) > 4096 or any(c in password for c in "\r\n\x00"):
        raise ValueError("A nonempty single-line database password is required")
    if password_file.exists() and password_file.read_text() != password:
        raise ValueError("Supplied DB password differs from retained credentials; reconcile root-password.txt explicitly")
    if not password_file.exists():
        atomic_text(password_file, password, 0o644)
    password_file.chmod(0o644)
    generated = {name: retained_secret(state, name) for name in SECRET_FILES}
    invitation = state / "auth-invitation-key.txt"
    if not invitation.exists():
        atomic_text(invitation, base64.b64encode(secrets.token_bytes(32)).decode(), 0o644)
    if not re.fullmatch(r"[A-Za-z0-9+/]{43}=", invitation.read_text()):
        raise ValueError("Invalid retained invitation encryption key")
    invitation.chmod(0o644)
    ca = state / "db-ca.pem"
    if args.db_ca_file:
        atomic_text(ca, args.db_ca_file.read_text(), 0o644)
    elif not ca.exists():
        with request.urlopen("https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem", timeout=30) as response:
            data = response.read((1 << 20) + 1)
        if len(data) > 1 << 20 or b"BEGIN CERTIFICATE" not in data:
            raise ValueError("Invalid AWS database CA bundle")
        atomic_text(ca, data.decode("ascii"), 0o644)
    ssl.create_default_context(cafile=str(ca))
    # Use Node's standard crypto implementation, preserving existing key pairs.
    key_script = """const {existsSync,readFileSync,writeFileSync}=require('node:fs');
const {generateKeyPairSync,createPublicKey}=require('node:crypto');
const p=process.argv[1],q=process.argv[2];
if(existsSync(p)!==existsSync(q))throw Error('Restore the complete original signing key pair');
if(!existsSync(p)){const k=generateKeyPairSync('rsa',{modulusLength:2048,privateKeyEncoding:{type:'pkcs8',format:'pem'},publicKeyEncoding:{type:'spki',format:'pem'}});writeFileSync(p,k.privateKey,{flag:'wx',mode:0o644});writeFileSync(q,k.publicKey,{flag:'wx',mode:0o644});}
if(createPublicKey(readFileSync(p)).export({type:'spki',format:'pem'})!==readFileSync(q,'utf8'))throw Error('Signing keys do not match');"""
    run("node", "-e", key_script, str(state / "auth-private.pem"), str(state / "auth-public.pem"))
    (state / "auth-private.pem").chmod(0o644)
    public = (state / "auth-public.pem").read_text()
    for name in ("case", "screening"):
        atomic_text(state / f"{name}-runtime/auth-public.pem", public, 0o644)
    host = args.db_client_ip + "/255.255.255.255"
    write_json(state / "ec2.json", {"Address": f"{args.db_host}:{args.db_port}",
               "AdminUser": args.db_user, "CAFile": "/ec2/db-ca.pem", "AccountHost": host,
               "ReferencePasswordFile": "/run/secrets/ec2_reference_password",
               "Origins": [origin(args.public_ip, ports)], "Gateway": True}, 0o644)
    callback = origin(args.public_ip, ports, "frontend") + "/api/auth/callback"
    issuer = origin(args.public_ip, ports, "auth")
    provider = {"Environment": "development", "IssuerURL": provider_origin(args.public_ip, ports) + "/realms/fraud",
                "BackchannelOrigin": "http://keycloak:8080", "ClientID": "fraud-auth",
                "ClientSecret": "", "RedirectURL": callback}
    auth = {"Address": "mysql:3306", "Database": "fraud", "AccountHost": host,
            "AppOrigin": issuer, "InvitationOrigin": origin(args.public_ip, ports, "frontend"),
            "Issuer": issuer, "Audience": "fraud-platform", "KeyID": "ec2-test-v1",
            "DefaultTenantID": "00000000-0000-0000-0000-000000000000", "Provider": provider,
            "SMTP": {"Address": "auth-mail:1025", "From": "auth@test.invalid", "LocalCapture": True}}
    write_json(state / "auth-runtime/config.json", auth, 0o644)
    common = {"Address": "mysql:3306", "Database": "fraud", "AccountHost": host,
              "Issuer": issuer, "Audience": "fraud-platform", "KeyID": "ec2-test-v1"}
    write_json(state / "case-runtime/config.json", dict(common, ServiceTenants=[], ServiceTenantMode="registered", PublisherURL=""), 0o644)
    write_json(state / "screening-runtime/config.json", dict(common, Origin=origin(args.public_ip, ports, "screening"), CaseURL="http://case-manager-service:8086", IngestionURL="http://ingestion-service:8081"), 0o644)
    # These env files are private server configuration, never browser build args.
    settings = {"BACKEND_PROFILE": "mysql", "EC2_MYSQL_HTTP_TEST": "1",
                "APP_ORIGIN": origin(args.public_ip, ports, "frontend"),
                "AUTH_SERVICE_URL": "http://auth-service:8087", "DATA_MODEL_SERVICE_URL": "http://data-model-service:8080",
                "INGESTION_SERVICE_URL": "http://ingestion-service:8081", "DECISION_ENGINE_SERVICE_URL": "http://decision-engine-service:8082",
                "SCREENING_SERVICE_URL": "http://screening-service:8085", "CASE_MANAGER_SERVICE_URL": "http://case-manager-service:8086"}
    atomic_text(state / "frontend.env", "".join(f"{key}={value}\n" for key, value in settings.items()))
    keycloak = {"KC_BOOTSTRAP_ADMIN_USERNAME": "test-admin", "KC_BOOTSTRAP_ADMIN_PASSWORD": generated["keycloak-admin-password"],
                "KC_DB_PASSWORD": generated["keycloak-db-password"], "FRAUD_OIDC_CLIENT_SECRET": generated["auth-oidc-client-secret"],
                "FRAUD_AUTH_REDIRECT_URI": callback}
    atomic_text(state / "keycloak.env", "".join(f"{key}={value}\n" for key, value in keycloak.items()))


def compose_source(path):
    return json.loads(run("docker", "compose", "--profile", "install", "-f", str(path),
                          "config", "--no-interpolate", "--no-env-resolution", "--format", "json", capture=True))


def render(args, state, ports):
    model = compose_source(REPO / "backend/decision-engine-service/docker-compose-mysql.yml")
    model["services"].pop("mysql")
    for directory, names in [("auth-service", ["migrate-auth", "auth-service", "auth-worker", "auth-mail"]),
                             ("case-manager-service", ["migrate-case", "case-manager-service", "case-manager-worker"]),
                             ("screening-service", ["migrate-screening", "screening-service", "screening-worker"])]:
        other = compose_source(REPO / "backend" / directory / "docker-compose-mysql.yml")
        for name in names:
            model["services"][name] = other["services"][name]
        model.setdefault("secrets", {}).update(other.get("secrets", {}))
        model.setdefault("volumes", {}).update(other.get("volumes", {}))
    front = compose_source(REPO / "frontend/docker-compose-mysql.yml")
    model["services"].update(front["services"])
    model["secrets"].update(front["secrets"])
    model["name"] = PROJECT
    model["networks"] = {"default": {"name": PROJECT + "_default"}}
    model["volumes"] = {"auth_mail_data": {"name": PROJECT + "_mail"}}
    model["secrets"]["data_model_service_token"] = {"file": str(state / "data-model-service-token.txt")}
    model["secrets"]["frontend_data_model"] = {"file": str(state / "data-model-service-token.txt")}
    model["secrets"]["keycloak_mysql_password"] = {"file": str(state / "keycloak-db-password.txt")}
    model["secrets"]["ec2_reference_password"] = {"file": str(state / "reference-root-password.txt")}
    service_origins = {name: origin(args.public_ip, ports, key) for key, name in SERVICES.items()}
    roles = {"data-model-author": "data-model-service", "data-model-worker": "data-model-service",
             "migrate-data-model": "data-model-service", "ingestion-worker": "ingestion-service",
             "migrate-ingestion": "ingestion-service", "migrate-decisions": "decision-engine-service",
             "decision-engine-worker": "decision-engine-service", "decision-local-providers": "decision-engine-service",
             "auth-worker": "auth-service", "migrate-auth": "auth-service"}
    for name, service in model["services"].items():
        service.pop("ports", None)
        dependencies = service.get("depends_on", {})
        dependencies.pop("mysql", None)
        service["volumes"] = [volume for volume in service.get("volumes", [])
                              if volume.get("target") not in ("/target-socket", "/reference-socket", "/var/run/mysqld")]
        if name not in ("mysql-reference", "auth-mail", "frontend"):
            service.setdefault("environment", {})["EC2_MYSQL_TEST_CONFIG"] = "/ec2/ec2.json"
            service["environment"]["APP_ORIGIN"] = service_origins.get(roles.get(name, name), origin(args.public_ip, ports, "auth"))
            service["volumes"].extend([{"type": "bind", "source": str(state / "ec2.json"), "target": "/ec2/ec2.json", "read_only": True},
                                        {"type": "bind", "source": str(state / "db-ca.pem"), "target": "/ec2/db-ca.pem", "read_only": True}])
        if "build" in service:
            service["image"] = PROJECT + "-" + service["image"].split(":")[0] + ":local"
        if name.startswith("migrate-") or name in ("data-model-author", "data-model-worker"):
            service.setdefault("secrets", []).append({"source": "ec2_reference_password", "target": "ec2_reference_password"})
    # Roles sharing an installer image must use the same freshly built tag.
    image_groups = {"migrate-data-model": ["data-model-author", "data-model-service", "data-model-worker"],
                    "migrate-ingestion": ["ingestion-service", "ingestion-worker"],
                    "migrate-decisions": ["decision-engine-service", "decision-engine-worker", "decision-local-providers"],
                    "migrate-auth": ["auth-service", "auth-worker"], "migrate-case": ["case-manager-service", "case-manager-worker"],
                    "migrate-screening": ["screening-service", "screening-worker"]}
    for installer, consumers in image_groups.items():
        for consumer in consumers:
            model["services"][consumer]["image"] = model["services"][installer]["image"]
    model["services"]["data-model-service"].setdefault("secrets", []).append({"source": "data_model_service_token", "target": "data_model_service_token"})
    model["services"]["frontend"]["secrets"].append({"source": "frontend_data_model", "target": "frontend_data_model"})
    # Existing installers use a socket locally. This independent reference is
    # now reached over the private Compose network, with no published port.
    model["services"]["mysql-reference"]["environment"]["MYSQL_ROOT_HOST"] = "%"
    model["services"]["mysql-reference"]["environment"]["MYSQL_ROOT_PASSWORD_FILE"] = "/run/secrets/ec2_reference_password"
    model["services"]["mysql-reference"]["secrets"] = ["ec2_reference_password"]
    # Local development now persists its reference schema. EC2 reference capture
    # stays disposable and must not inherit that local volume or its ownership.
    reference = model["services"]["mysql-reference"]
    reference["volumes"] = [volume for volume in reference.get("volumes", [])
                            if volume.get("target") != "/var/lib/mysql"]
    reference["tmpfs"] = ["/var/lib/mysql"]
    model["services"]["mysql-reference"]["healthcheck"]["test"][1] = model["services"]["mysql-reference"]["healthcheck"]["test"][1].replace("mysql_root_password", "ec2_reference_password")
    model["services"]["keycloak"] = {
        "image": "quay.io/keycloak/keycloak:26.8.0", "command": ["start-dev", "--import-realm"],
        "env_file": [{"path": str(state / "keycloak.env"), "required": True}],
        "environment": {"KC_DB": "mysql", "KC_DB_URL": f"jdbc:mysql://{args.db_host}:{args.db_port}/keycloak?sslMode=VERIFY_IDENTITY&fallbackToSystemTrustStore=true", "KC_DB_USERNAME": "fraud_ec2_keycloak",
                        "KC_TRUSTSTORE_PATHS": "/ec2/db-ca.pem",
                        "KC_HOSTNAME": provider_origin(args.public_ip, ports), "KC_HTTP_RELATIVE_PATH": "/identity",
                        "KC_HTTP_MANAGEMENT_RELATIVE_PATH": "/", "KC_PROXY_HEADERS": "xforwarded",
                        "KC_HEALTH_ENABLED": "true", "KC_DB_POOL_INITIAL_SIZE": "1", "KC_DB_POOL_MIN_SIZE": "1", "KC_DB_POOL_MAX_SIZE": "5"},
        "volumes": [{"type": "bind", "source": str(REPO / "docker/keycloak/fraud-realm.json"), "target": "/opt/keycloak/data/import/fraud-realm.json", "read_only": True},
                    {"type": "bind", "source": str(state / "db-ca.pem"), "target": "/ec2/db-ca.pem", "read_only": True}],
        "mem_limit": "1g", "cpus": 1,
        "healthcheck": {"test": ["CMD", "/bin/bash", "-c", "exec 3<>/dev/tcp/127.0.0.1/9000; printf 'GET /health/ready HTTP/1.0\\r\\nHost: localhost\\r\\n\\r\\n' >&3; head -n 1 <&3 | grep -q '200'"],
                        "interval": "10s", "timeout": "5s", "start_period": "10m", "retries": 20}, "restart": "unless-stopped"}
    model["services"]["preflight"] = {
        "image": PROJECT + "-preflight:local", "profiles": ["install"],
        "build": {"context": str(REPO), "dockerfile": "docker/mysql-ec2/Dockerfile.preflight"},
        "environment": {"EC2_MYSQL_TEST_CONFIG": "/ec2/ec2.json"}, "secrets": ["mysql_root_password", "keycloak_mysql_password"],
        "volumes": [{"type": "bind", "source": str(state / "ec2.json"), "target": "/ec2/ec2.json", "read_only": True},
                    {"type": "bind", "source": str(state / "db-ca.pem"), "target": "/ec2/db-ca.pem", "read_only": True}],
        "read_only": True, "tmpfs": ["/tmp"]}
    # Rewrite only known local state paths. Source files and build contexts stay
    # in the checkout. No development database volume is reused.
    old = str(REPO / ".local/mysql-dev")
    def relocate(value):
        if isinstance(value, str):
            return str(state) + value[len(old):] if value.startswith(old + "/") else value
        if isinstance(value, list):
            return [relocate(item) for item in value]
        if isinstance(value, dict):
            return {key: relocate(item) for key, item in value.items()}
        return value
    model = relocate(model)
    gateway = compose_source(REPO / "docker/gateway.compose.yaml")["services"]["api-gateway"]
    gateway["ports"] = [{"target": 8080, "published": str(ports["gateway"]), "host_ip": "0.0.0.0", "protocol": "tcp"}]
    model["services"]["api-gateway"] = gateway
    # Interpolation remains data: Compose receives only these non-secret values.
    tenant = args.tenant_id or ""
    settings = {"INGESTION_TENANT_ID": tenant, "INGESTION_OBJECT_TYPE": args.object_type,
                "DECISION_TENANT_ID": tenant, "DECISION_MYSQL_SUBNET": args.db_client_ip + "/32"}
    atomic_text(state / "compose.env", "".join(f"{key}={value}\n" for key, value in settings.items()))
    write_json(state / "compose.json", model)
    return model


def compose(state, *args, capture=False):
    return run("docker", "compose", "--project-name", PROJECT, "--profile", "install",
               "--env-file", str(state / "compose.env"), "-f", str(state / "compose.json"), *args, capture=capture)


def api(url, method="GET", body=None, token=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    req = request.Request(url, method=method, headers=headers,
                          data=None if body is None else json.dumps(body).encode())
    try:
        with request.urlopen(req, timeout=90) as response:
            data = response.read()
            return json.loads(data) if data else None
    except error.HTTPError as exc:
        raise RuntimeError(f"{method} {parse.urlsplit(url).path} returned HTTP {exc.code}; response and credentials omitted") from None


def check_ports(ports):
    ids = run("docker", "ps", "--filter", "label=com.docker.compose.project=" + PROJECT, "-q", capture=True).split()
    owned = set()
    if ids:
        raw = run("docker", "inspect", "--format", "{{json .NetworkSettings.Ports}}", *ids, capture=True)
        for line in raw.splitlines():
            for bindings in (json.loads(line) or {}).values():
                owned.update(int(item["HostPort"]) for item in bindings or [])
    for name, port in ports.items():
        if port in owned:
            continue
        try:
            with socket.socket() as listener:
                listener.bind(("0.0.0.0", port))
        except OSError:
            raise ValueError(f"Port {port} is occupied; choose --port {name}=OTHER_PORT") from None


def prepare_tenant(args, state, ports):
    base = local_service(ports, "data-model") + "/v1"
    token = (state / "data-model-service-token.txt").read_text()
    def model(path, method="GET", body=None):
        return api(base + path, method, body, token)
    if not args.tenant_id:
        tenants = model("/tenants").get("tenants") or []
        matches = [item for item in tenants if item.get("external_key") == "ec2-http-test"]
        if len(matches) > 1:
            raise ValueError("Ambiguous test tenant; supply --tenant-id explicitly")
        tenant = matches[0] if matches else model("/tenants", "POST", {"name": "EC2 HTTP test", "external_key": "ec2-http-test"})["tenant"]
        args.tenant_id = tenant["id"]
    tenant = model("/tenants/" + args.tenant_id)["tenant"]
    if tenant["status"] == "pending":
        model("/tenants/" + args.tenant_id + "/provision", "POST", {})
    elif tenant["status"] != "active":
        raise ValueError("Selected tenant is not active")
    path = "/tenants/" + args.tenant_id + "/tables"
    matches = [item for item in model(path).get("tables") or [] if item["name"] == args.object_type and not item.get("archived")]
    if len(matches) > 1:
        raise ValueError("Ambiguous ingestion table")
    created = not matches
    table = model(path, "POST", {"name": args.object_type})["table"] if created else matches[0]
    if created or tenant.get("external_key") == "ec2-http-test":
        field_path = "/tables/" + table["id"] + "/fields"
        fields = model(field_path).get("fields") or []
        for name, data_type in [("amount", "float"), ("currency", "string")]:
            if not any(field["name"] == name and not field.get("archived") for field in fields):
                model(field_path, "POST", {"name": name, "data_type": data_type, "nullable": True})


def wait_model(state, ports):
    deadline = time.monotonic() + 90
    while True:
        try:
            api(local_service(ports, "data-model") + "/readyz")
            return
        except (OSError, RuntimeError):
            if time.monotonic() >= deadline:
                raise RuntimeError("Data-model API did not become ready within 90 seconds") from None
            time.sleep(2)


def provider_client(state, ports):
    base = f"http://127.0.0.1:{ports['gateway']}/identity"
    body = parse.urlencode({"grant_type": "password", "client_id": "admin-cli", "username": "test-admin",
                            "password": (state / "keycloak-admin-password.txt").read_text()}).encode()
    req = request.Request(base + "/realms/master/protocol/openid-connect/token", data=body,
                          headers={"Content-Type": "application/x-www-form-urlencoded"})
    with request.urlopen(req, timeout=20) as response:
        token = json.load(response)["access_token"]
    return base, token


def configure_provider(args, state, ports):
    base, token = provider_client(state, ports)
    path = base + "/admin/realms/fraud/clients"
    clients = api(path + "?clientId=fraud-auth", token=token)
    if len(clients) != 1:
        raise ValueError("Expected one fraud-auth Keycloak client")
    client = clients[0]
    credential = api(path + "/" + client["id"] + "/client-secret", token=token)
    if credential.get("value") != (state / "auth-oidc-client-secret.txt").read_text():
        raise ValueError("Keycloak client secret differs from retained auth credentials; reconcile explicitly")
    callback = origin(args.public_ip, ports, "frontend") + "/api/auth/callback"
    if callback not in client.get("redirectUris", []):
        client["redirectUris"] = client.get("redirectUris", []) + [callback]
        api(path + "/" + client["id"], "PUT", client, token)


def bootstrap_user(args, state, ports):
    if not args.bootstrap_user:
        return
    base, token = provider_client(state, ports)
    path = base + "/admin/realms/fraud/users"
    users = api(path + "?username=" + parse.quote(args.bootstrap_user) + "&exact=true&max=2", token=token)
    if len(users) > 1:
        raise ValueError("Ambiguous provider username")
    if not users:
        credential_file = state / "bootstrap-user-credentials.json"
        if credential_file.exists():
            credentials = json.loads(credential_file.read_text())
            if credentials["username"] != args.bootstrap_user:
                raise ValueError("Retained bootstrap credentials belong to a different username")
        else:
            credentials = {"username": args.bootstrap_user, "password": secrets.token_urlsafe(32)}
            write_json(credential_file, credentials)
        api(path, "POST", {"username": args.bootstrap_user, "email": args.bootstrap_email, "enabled": True,
                           "firstName": "EC2", "lastName": "Tester", "emailVerified": False,
                           "credentials": [{"type": "password", "value": credentials["password"], "temporary": False}]}, token)
        users = api(path + "?username=" + parse.quote(args.bootstrap_user) + "&exact=true&max=2", token=token)
    if len(users) != 1 or not users[0].get("enabled") or users[0].get("username", "").casefold() != args.bootstrap_user.casefold():
        raise ValueError("Bootstrap identity is unavailable or disabled")
    roles = args.bootstrap_roles.split(",")
    write_json(state / "auth-runtime/provision.json", {"TenantID": args.tenant_id,
               "Issuer": provider_origin(args.public_ip, ports) + "/realms/fraud", "Subject": users[0]["id"],
               "Name": args.bootstrap_user, "Roles": roles})
    compose(state, "run", "--rm", "--no-deps", "migrate-auth", "/app/install-auth", "--provision", "/runtime/provision.json")
    print("Bootstrap user ready. Newly generated credentials, if any, are in", state / "bootstrap-user-credentials.json")


def verify_gateway(args, state, ports):
    """Check actual host bindings and public URL contracts without user writes."""
    ids = compose(state, "ps", "--status", "running", "-q", capture=True).split()
    if not ids:
        raise RuntimeError("No EC2 runtime containers found")
    bindings = run("docker", "inspect", "--format", '{{index .Config.Labels "com.docker.compose.service"}}\t{{json .NetworkSettings.Ports}}', *ids, capture=True)
    published = []
    for line in bindings.splitlines():
        name, values = line.split("\t", 1)
        for target, mappings in (json.loads(values) or {}).items():
            published.extend((name, target, value["HostIp"], value["HostPort"]) for value in mappings or [])
    if published != [("api-gateway", "8080/tcp", "0.0.0.0", str(ports["gateway"]))]:
        raise RuntimeError("EC2 runtime must publish only the selected gateway port")
    base = f"http://127.0.0.1:{ports['gateway']}"
    for path in ["/login", *[f"/services/{name}/{endpoint}" for name in SERVICES for endpoint in ("readyz", "docs")]]:
        with request.urlopen(base + path, timeout=30) as response:
            if response.status != 200:
                raise RuntimeError("Gateway check failed: " + path)
    discovery = api(base + "/identity/realms/fraud/.well-known/openid-configuration")
    issuer = provider_origin(args.public_ip, ports) + "/realms/fraud"
    if discovery.get("issuer") != issuer or any(not discovery.get(name, "").startswith(issuer + "/") for name in ("authorization_endpoint", "token_endpoint", "jwks_uri", "end_session_endpoint")):
        raise RuntimeError("Keycloak discovery must use the public gateway identity context")
    api(base + "/identity/realms/fraud/protocol/openid-connect/certs")
    print("Verified one published port, frontend, six API references/readiness checks and Keycloak public discovery.")


def deploy(args, state, ports):
    compose(state, "config", "--quiet")
    if args.render_only:
        print("Configuration prepared at", state, "— no database was contacted and no containers were started.")
        return
    check_ports(ports)
    compose(state, "build", "preflight")
    compose(state, "run", "--rm", "--no-deps", "preflight")
    if args.check_only:
        print("Read-only Aurora preflight passed. No schema migration or application startup performed.")
        return
    compose(state, "build", "migrate-data-model", "migrate-ingestion", "migrate-decisions",
            "migrate-auth", "migrate-case", "migrate-screening", "frontend")
    # A failed preflight leaves any running deployment untouched.
    compose(state, "stop", "api-gateway", "frontend", "keycloak", *BACKEND_ROLES)
    compose(state, "run", "--rm", "--no-deps", "preflight", "--initialize", "--initialize-keycloak")
    compose(state, "up", "-d", "--wait", "mysql-reference", "auth-mail", "api-gateway")
    compose(state, "up", "-d", "--wait", "--wait-timeout", "900", "keycloak")
    configure_provider(args, state, ports)
    compose(state, "run", "--rm", "--no-deps", "migrate-data-model")
    compose(state, "up", "-d", "--no-deps", "--wait", "data-model-author", "data-model-service", "data-model-worker")
    wait_model(state, ports)
    prepare_tenant(args, state, ports)
    render(args, state, ports)
    for installer, services in [
        ("migrate-ingestion", ["ingestion-service", "ingestion-worker"]),
        ("migrate-decisions", ["decision-local-providers", "decision-engine-worker", "decision-engine-service"]),
        ("migrate-auth", ["auth-service", "auth-worker"]),
        ("migrate-case", ["case-manager-service", "case-manager-worker"]),
        ("migrate-screening", ["screening-service", "screening-worker"]),
    ]:
        compose(state, "run", "--rm", "--no-deps", installer)
        compose(state, "up", "-d", "--no-deps", "--wait", *services)
    compose(state, "stop", "screening-worker", "case-manager-service", "case-manager-worker")
    compose(state, "run", "--rm", "--no-deps", "migrate-case")
    compose(state, "run", "--rm", "--no-deps", "migrate-decisions", "/app/install-decisions", "--grant-local-browser")
    compose(state, "run", "--rm", "--no-deps", "data-model-author", "/app/mysql-local-author", "--sync-local-consumers")
    compose(state, "up", "-d", "--no-deps", "--wait", "case-manager-service", "case-manager-worker", "screening-worker")
    bootstrap_user(args, state, ports)
    compose(state, "up", "-d", "--no-deps", "--wait", "frontend")
    print("HTTP test frontend:", origin(args.public_ip, ports, "frontend"))
    verify_gateway(args, state, ports)
    print("Keycloak:", provider_origin(args.public_ip, ports), "(administrator test-admin; password in keycloak-admin-password.txt)")
    print("API references:", origin(args.public_ip, ports) + "/services/")
    print("Only gateway port", ports["gateway"], "is published; APIs, Keycloak, mailbox and reference MySQL remain internal.")
    print("State and restart credentials retained privately at", state)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--public-ip", required=True, help="EC2 IPv4 address used by browser clients")
    parser.add_argument("--db-host", required=True, help="Aurora writer DNS endpoint")
    parser.add_argument("--db-port", type=int, default=3306)
    parser.add_argument("--db-user", required=True, help="Migration administrator; application accounts are separate")
    password = parser.add_mutually_exclusive_group()
    password.add_argument("--db-password-file", type=Path)
    password.add_argument("--db-password-stdin", action="store_true")
    parser.add_argument("--db-ca-file", type=Path, help="AWS RDS CA bundle; otherwise downloaded once")
    parser.add_argument("--db-client-ip", help="Source IPv4 Aurora sees; defaults to the EC2 host's route address")
    parser.add_argument("--state-dir", type=Path, default=REPO / ".local/mysql-ec2")
    parser.add_argument("--port", action="append", default=[], metavar="NAME=NUMBER", help="Override any published port: " + ", ".join(DEFAULT_PORTS))
    parser.add_argument("--tenant-id", help="Existing active tenant; otherwise create/reuse the test tenant")
    parser.add_argument("--object-type", default="transactions")
    parser.add_argument("--bootstrap-user", help="Explicitly create/reuse and provision one test administrator")
    parser.add_argument("--bootstrap-email")
    parser.add_argument("--bootstrap-roles", default="tenant_admin,platform_admin,case_manager_admin")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--render-only", action="store_true", help="Prepare/validate configuration without contacting Aurora")
    mode.add_argument("--check-only", action="store_true", help="Run read-only Aurora checks; no migrations or service startup")
    args = parser.parse_args()
    args.public_ip = str(ipaddress.IPv4Address(args.public_ip))
    if not re.fullmatch(r"[A-Za-z0-9.-]+\.rds\.amazonaws\.com", args.db_host) or args.db_host.endswith(".") or not 1 <= args.db_port <= 65535:
        raise ValueError("A valid Aurora writer endpoint and port are required")
    if not re.fullmatch(r"[A-Za-z][A-Za-z0-9_]{0,31}", args.db_user):
        raise ValueError("Invalid database username")
    if not re.fullmatch(r"[a-z][a-z0-9_]{0,62}", args.object_type):
        raise ValueError("Invalid ingestion object type")
    if args.tenant_id:
        args.tenant_id = str(uuid.UUID(args.tenant_id))
        if uuid.UUID(args.tenant_id).int == 0:
            raise ValueError("Tenant UUID must be nonzero")
    known_roles = {"tenant_admin", "platform_admin", "platform_viewer", "platform_builder", "platform_publisher", "case_manager_admin", "case_investigator"}
    if not set(args.bootstrap_roles.split(",")) <= known_roles:
        raise ValueError("Unknown bootstrap role")
    if args.bootstrap_user and (not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._@+-]{0,63}", args.bootstrap_user) or not args.bootstrap_email or not re.fullmatch(r"[^\s@]+@[^\s@]+\.[^\s@]+", args.bootstrap_email)):
        raise ValueError("A valid --bootstrap-user and --bootstrap-email are required together")
    ports = selected_ports(args.port)
    if not shutil.which("docker") or not shutil.which("node"):
        raise RuntimeError("Docker Compose and Node.js are required on the Docker host")
    if not args.render_only and run("docker", "info", "--format", "{{.OSType}}", capture=True) != "linux":
        raise RuntimeError("A Linux Docker daemon is required")
    args.db_client_ip = str(ipaddress.IPv4Address(args.db_client_ip or client_ip(args.db_host, args.db_port)))
    state = args.state_dir.expanduser().resolve()
    if state == (REPO / ".local/mysql-dev").resolve():
        raise ValueError("Use a separate state directory; the local development installation must remain separate")
    state.mkdir(parents=True, exist_ok=True, mode=0o700)
    state.chmod(0o700)
    with (state / "setup.lock").open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise RuntimeError("Another EC2 setup is already running") from None
        identity = deployment_identity(args, ports)
        verify_identity(state, identity)
        write_json(state / "deployment.json", identity)
        prepare_files(args, state, ports)
        render(args, state, ports)
        deploy(args, state, ports)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as exc:
        print("EC2 test setup failed:", exc, file=sys.stderr)
        sys.exit(1)
    except KeyboardInterrupt:
        print("Setup interrupted; state retained for recovery.", file=sys.stderr)
        sys.exit(1)
