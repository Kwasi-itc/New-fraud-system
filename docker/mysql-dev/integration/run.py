#!/usr/bin/env python3
"""Run the existing MySQL behavioral audits and a cross-service pipeline test.

Requires Python 3, Node.js, Docker Desktop, and the running local MySQL stack.
No database/volume resets. Fresh fixtures and detailed evidence are retained.
Workers are temporarily paused/restarted and restored; run without other tests.
"""
import argparse
from datetime import datetime, timezone
import fcntl
import json
import os
from pathlib import Path
import re
import selectors
import shutil
import signal
import subprocess
import sys
import time
import uuid

REPO = Path(__file__).resolve().parents[3]
LOCAL = REPO / ".local/mysql-dev"
SERVICES = {
    "data-model": "data-model-service",
    "ingestion": "ingestion-service",
    "decision": "decision-engine-service",
}
WORKERS = ["data-model-worker", "ingestion-worker", "decision-engine-worker", "decision-local-providers"]


def inventory(path):
    """Read this repository's indented OpenAPI paths without YAML dependencies."""
    operations, route, in_paths = [], None, False
    for line in path.read_text(encoding="utf-8-sig").splitlines():
        if line == "paths:":
            in_paths = True
            continue
        if not in_paths:
            continue
        if line and not line.startswith((" ", "#")):
            break
        found = re.fullmatch(r"  (/[^:]+):\s*", line)
        if found:
            route = found[1]
        method = re.fullmatch(r"    (get|post|put|patch|delete|head|options):\s*", line)
        if route and method:
            operations.append(f"{method[1].upper()} {route}")
    if not operations or len(operations) != len(set(operations)):
        raise ValueError(f"Empty or duplicate OpenAPI operations in {path}")
    return operations


def coverage(name, expected, report):
    entries = report.get("matrix", []) if name == "decision" else report.get("coverage", [])
    mapping = {}
    for entry in entries:
        key = f"{entry['method']} {entry['path']}" if name == "decision" else entry["operation"]
        if key in mapping:
            raise ValueError(f"Duplicate coverage entry: {key}")
        mapping[key] = entry
    verified = []
    for operation in expected:
        entry = mapping.get(operation, {})
        proven = (entry.get("status") == "success_verified" and bool(entry.get("successfulSuites"))) if name == "decision" else (
            entry.get("covered") is True and entry.get("successfulRequests", 0) > 0 and bool(entry.get("suites")))
        if proven:
            verified.append(operation)
    return {"documented": len(expected), "verified": len(verified),
            "missing": sorted(set(expected) - set(verified)),
            "stale_inventory": sorted(set(mapping) - set(expected))}


def capture(*command, input=None):
    return subprocess.run(command, cwd=REPO, check=True, text=True,
                          input=input, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=120).stdout


def reference_ready():
    plan = json.loads((LOCAL / "plans/data_model.json").read_text())
    expected = len(plan["steps"])
    command = 'export MYSQL_PWD="$(cat /run/secrets/mysql_root_password)"; exec mysql --protocol=socket -uroot --database=fraud --batch --skip-column-names'
    try:
        value = capture("docker", "exec", "-i", "fraud-mysql-dev-mysql-reference-1", "sh", "-ec", command,
                        input="SELECT COUNT(*),SUM(state='applied') FROM fraud.data_model_schema_migrations;\n").strip()
        total, applied = (int(v) for v in value.split())
        if total == applied == expected and expected > 0:
            return
    except (OSError, ValueError, subprocess.SubprocessError):
        pass
    raise RuntimeError("Reference MySQL lacks the retained data-model migrations. Re-run migrate-data-model without deleting plans or application volumes.")


def inspect(service):
    raw = json.loads(capture("docker", "inspect", "fraud-mysql-dev-" + service + "-1"))[0]
    mode = next((v.split("=", 1)[1] for v in raw["Config"].get("Env", [])
                 if v.startswith("LOCAL_CALLBACK_MODE=")), "accept")
    return {"id": raw["Id"], "image": raw["Image"],
            "running": raw["State"]["Running"], "paused": raw["State"]["Paused"],
            "health": raw["State"].get("Health", {}).get("Status"), "callback_mode": mode}


def wait_healthy(service):
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        value = inspect(service)
        if value["running"] and not value["paused"] and value["health"] in (None, "healthy"):
            return value
        time.sleep(2)
    raise RuntimeError(f"{service} did not become healthy")


def terminate(child):
    if child.poll() is None:
        os.killpg(child.pid, signal.SIGTERM)
        try:
            child.wait(timeout=10)
        except subprocess.TimeoutExpired:
            os.killpg(child.pid, signal.SIGKILL)
            child.wait()


def execute(command, log, timeout=2400):
    """Stream child output, bound its runtime, and terminate its children too."""
    started = time.monotonic()
    next_update = started + 30
    child = subprocess.Popen(command, cwd=REPO, stdout=subprocess.PIPE,
                             stderr=subprocess.STDOUT, start_new_session=True)
    try:
        with log.open("wb") as stream, selectors.DefaultSelector() as selector:
            selector.register(child.stdout, selectors.EVENT_READ)
            while selector.get_map():
                for key, _ in selector.select(timeout=1):
                    chunk = os.read(key.fileobj.fileno(), 65536)
                    if not chunk:
                        selector.unregister(key.fileobj)
                        continue
                    stream.write(chunk)
                    stream.flush()
                    print(chunk.decode(errors="replace"), end="", flush=True)
                now = time.monotonic()
                if now - started > timeout:
                    raise TimeoutError(f"Stage exceeded {timeout}s; see {log}")
                if now >= next_update:
                    print(f"  Stage still running ({int(now - started)}s); log: {log}", flush=True)
                    next_update = now + 30
            return child.wait(timeout=10)
    finally:
        terminate(child)
        child.stdout.close()


def restore(before, output):
    errors = []
    for name, initial in before.items():
        try:
            current = inspect(name)
            if name == "decision-local-providers" and current["callback_mode"] != initial["callback_mode"]:
                overlay = output / "restore-provider.json"
                overlay.write_text(json.dumps({"services": {name: {"environment": {
                    "LOCAL_CALLBACK_MODE": initial["callback_mode"]}}}}))
                command = ["docker", "compose"]
                for env in ("compose.env", "ingestion.env", "decision.env"):
                    command.extend(["--env-file", str(LOCAL / env)])
                command.extend(["-f", str(REPO / "backend/decision-engine-service/docker-compose-mysql.yml"),
                                "-f", str(overlay), "up", "-d", "--no-deps", "--wait", name])
                capture(*command)
                current = inspect(name)
            container = "fraud-mysql-dev-" + name + "-1"
            if current["paused"]:
                capture("docker", "unpause", container)
            if not current["running"]:
                capture("docker", "start", container)
            wait_healthy(name)
        except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as exc:
            errors.append(f"{name}: {exc}")
    return errors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--list", action="store_true", help="List scope without Docker/API calls")
    parser.add_argument("--only", choices=[*SERVICES, "pipeline"], help="Run one stage; reports partial scope")
    args = parser.parse_args()
    inventories = {name: inventory(REPO / "backend" / service / "internal/httpapi/openapi.yaml")
                   for name, service in SERVICES.items()}
    if args.list:
        for name, operations in inventories.items():
            print(f"{name}: {len(operations)} documented operations")
        print("decision: additional mounted routes checked by its audit")
        print("pipeline: published model → ingestion → stored-record decisions → async/scheduled workers → history")
        return 0
    for program in ("node", "docker"):
        if not shutil.which(program):
            parser.error(f"{program} is required")
    if int(capture("node", "--version").lstrip("v").split(".")[0]) < 22:
        parser.error("Node.js 22+ is required by the live audits")
    output = LOCAL / "integration" / (datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ-") + uuid.uuid4().hex[:8])
    output.mkdir(parents=True)
    print(f"Evidence: {output}", flush=True)
    summary = {"scope": args.only or "all", "passed": False, "all_endpoints_verified": False,
               "stages": {}, "cleanup_errors": [], "inventories": inventories,
               "limitations": ["Local MySQL development profile; production identity/load/external providers are separate acceptance",
                               "Fresh fixtures retained; workers temporarily paused/restarted; no database/volume resets"]}
    before = {}
    with (LOCAL / "setup-mac.lock").open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            parser.error("Another setup or integration run holds the local stack lock")
        try:
            for service in ("mysql", "mysql-reference", "data-model-service", "ingestion-service", "decision-engine-service", *WORKERS):
                current = inspect(service)
                if not current["running"] or current["paused"] or current["health"] not in (None, "healthy"):
                    raise RuntimeError(f"{service} must already be running, unpaused and healthy")
                if service in WORKERS:
                    before[service] = current
            reference_ready()
            (output / "runtime-before.json").write_text(json.dumps(before, indent=2) + "\n")
            for name in [*SERVICES, "pipeline"]:
                if args.only and args.only != name:
                    continue
                print(f"\nRunning {name} integration tests…", flush=True)
                folder = output / name
                folder.mkdir()
                started = time.monotonic()
                stage = {"passed": False, "report": str(folder / "report.json")}
                summary["stages"][name] = stage
                try:
                    if name == "data-model":
                        capture("docker", "pause", "fraud-mysql-dev-data-model-worker-1")
                    command = ([sys.executable, "-B", str(Path(__file__).with_name("pipeline.py"))]
                               if name == "pipeline" else ["node", str(REPO / "backend" / SERVICES[name] / "scripts/mysql-endpoint-audit.mjs")])
                    exit_code = execute([*command, "--output", str(folder)], output / f"{name}.log")
                    stage["exit_code"] = exit_code
                    report = json.loads((folder / "report.json").read_text())
                    if name == "pipeline":
                        stage["passed"] = exit_code == 0 and report.get("passed") is True
                        stage["cases"] = report.get("cases", [])
                    else:
                        stage["coverage"] = coverage(name, inventories[name], report)
                        profile_passed = report.get("profilePassed") if name == "decision" else report.get("passed")
                        stage["passed"] = exit_code == 0 and profile_passed is True and not (
                            stage["coverage"]["missing"] or stage["coverage"]["stale_inventory"])
                        stage["suites"] = report.get("suites", [])
                        stage["requests"] = report.get("requestCount")
                        if name == "decision":
                            stage["mounted"] = report.get("mountedOperations")
                            stage["mounted_verified"] = report.get("mountedSuccessfulOperations")
                            stage["passed"] &= bool(stage["mounted"]) and stage["mounted"] == stage["mounted_verified"]
                except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as exc:
                    stage["error"] = str(exc)
                finally:
                    if name == "data-model":
                        capture("docker", "unpause", "fraud-mysql-dev-data-model-worker-1")
                        wait_healthy("data-model-worker")
                    stage["elapsed_seconds"] = round(time.monotonic() - started, 2)
                    (output / "report.json").write_text(json.dumps(summary, indent=2) + "\n")
                print(f"{'PASS' if stage['passed'] else 'FAIL'} {name}", flush=True)
        except (OSError, ValueError, RuntimeError, subprocess.SubprocessError) as exc:
            summary["error"] = str(exc)
        except KeyboardInterrupt:
            summary["error"] = "Interrupted; child processes terminated and worker restoration attempted"
        finally:
            summary["cleanup_errors"] = restore(before, output)
            expected_stages = {args.only} if args.only else {*SERVICES, "pipeline"}
            summary["passed"] = (set(summary["stages"]) == expected_stages and
                                 all(stage["passed"] for stage in summary["stages"].values()) and
                                 not summary.get("error") and not summary["cleanup_errors"])
            summary["all_endpoints_verified"] = summary["passed"] and args.only is None
            (output / "report.json").write_text(json.dumps(summary, indent=2) + "\n")
    print(f"\n{'PASS' if summary['passed'] else 'FAIL'}: {summary['scope']}; report: {output / 'report.json'}")
    if summary.get("error"):
        print(summary["error"])
    for error in summary["cleanup_errors"]:
        print("RESTORATION FAILED: " + error)
    return 0 if summary["passed"] else 1


if __name__ == "__main__":
    sys.exit(main())
