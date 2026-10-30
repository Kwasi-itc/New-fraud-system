"""Offline validation/reporting and verified disposable local execution."""
from __future__ import annotations

import argparse
import asyncio
import json
import math
import os
import secrets
import subprocess
from dataclasses import replace
from pathlib import Path

from production_replay.api_client import ServiceClients, ServiceConfig
from production_replay.benchmark_observation import capture_environment
from production_replay.benchmark_reporting import write_json_atomic
from production_replay.postgres_observation import PostgresObserver
from system_tests.local_stress import local_fixture
from system_tests.service_pilot import run_fixture
from .model import Experiment, SCALARS, VERSION, digest, formula
from .report import report
from .run import run_experiment
from .database import capture_history


def load(path: Path) -> list[Experiment]:
    document = json.loads(path.read_text(encoding="utf-8"))
    if set(document) != {"schema_version", "experiments"} or document["schema_version"] != VERSION:
        raise ValueError("unsupported campaign contract")
    if not isinstance(document["experiments"], list) or not 1 <= len(document["experiments"]) <= 200:
        raise ValueError("campaign requires 1..200 experiments")
    experiments = [Experiment.parse(x) for x in document["experiments"]]
    if len({x.name for x in experiments}) != len(experiments):
        raise ValueError("experiment names must be unique")
    if len({x.read_mode for x in experiments}) != 1:
        raise ValueError("each local stack has one read mode; use separate campaigns for reader parity")
    return experiments


def fixture(project: str, image_prefix: str | None, read_mode: str) -> dict:
    value = local_fixture(project, image_prefix)
    for role, service in value["services"].items():
        if role == "postgres":
            service["mem_limit"] = "1g"
            continue
        if role == "ingestion-migrate" and image_prefix is None:
            # Current ingestion Dockerfile copies ingestion-service/ and shared mysqlstore/.
            service["build"] = {"context": str(Path(__file__).resolve().parents[3]),
                                "dockerfile": "ingestion-service/Dockerfile"}
        service["environment"] = {**service["environment"],
            "SERVICE_AUTH_MODE": "token", "SERVICE_AUTH_TOKEN": "${DECISION_BENCHMARK_TOKEN:?required}",
            "DATA_MODEL_SERVICE_TOKEN": "${DECISION_BENCHMARK_TOKEN:?required}",
            "TENANT_DATA_READ_MODE": read_mode, "LIVE_DECISION_MODE": "sync",
            "LIVE_ASYNC_FALLBACK_ENABLED": "false", "LIVE_DECISION_CONCURRENCY_LIMIT": "0"}
    return value


def inspect_owned(identity: str, project: str) -> dict:
    value = json.loads(subprocess.check_output(["docker", "inspect", identity], text=True, timeout=15))[0]
    labels = value["Config"]["Labels"]
    if labels.get("system-tests.project") != project or labels.get("system-tests.disposable") != "true":
        raise ValueError("not_owned_disposable_container")
    return {"id": value["Id"], "image": value["Image"], "running": value["State"]["Running"],
            "memory_limit_bytes": value["HostConfig"]["Memory"], "labels": labels}


def source_manifest(root):
    # Capture contents, not just git status: the user may have uncommitted source changes.
    files = subprocess.check_output(["git", "-C", str(root), "ls-files", "--cached", "--others", "--exclude-standard"],
                                    text=True, timeout=20).splitlines()
    hashes = {}
    import hashlib
    for relative in sorted(set(files)):
        path = root / relative
        if relative.startswith("backend/") and path.is_file() and path.suffix in (".go", ".py", ".sql", ".mod", ".sum"):
            hashes[relative] = hashlib.sha256(path.read_bytes()).hexdigest()
    return hashes


async def exercise(output, endpoints, project, _worker_id, postgres_id, experiments, token, source):
    inspect_owned(postgres_id, project)
    root = Path(__file__).resolve().parents[4]
    if source_manifest(root) != source:
        raise ValueError("source_changed_during_build_retry_with_frozen_checkout")
    environment = capture_environment(root, output, endpoints)
    hashes = source
    write_json_atomic(output / "source-manifest.json", hashes)
    environment["source_manifest_sha256"] = digest(hashes)
    environment["database"] = {"logical_database": "system_tests", "disposable": True, "project": project,
                               "shared_by": ["data-model", "ingestion", "decision-engine"]}
    identities = subprocess.check_output(["docker", "ps", "--filter", f"label=com.docker.compose.project={project}", "--quiet"], text=True, timeout=15).splitlines()
    environment["containers"] = [inspect_owned(x, project) for x in identities]
    write_json_atomic(output / "environment.json", environment)
    observer = PostgresObserver(["docker", "exec", "-e", "PGOPTIONS=-c default_transaction_read_only=on -c statement_timeout=3000",
                                 postgres_id, "psql", "-U", "system_tests", "-d", "system_tests"], dict(os.environ), 5)
    summaries = []
    async with ServiceClients(ServiceConfig(endpoints["data-model"], endpoints["ingestion"], endpoints["decision-engine"],
                                           auth_token=token, timeout_seconds=30, max_connections=256)) as clients:
        await clients.wait_until_ready()
        observations = {"decision": endpoints["decision-engine"], "ingestion": endpoints["ingestion"]}
        # Interleave configurations across repetitions; each gets fresh tenant state.
        for repeat in range(max(x.repeats for x in experiments)):
            for exp in experiments:
                if repeat >= exp.repeats:
                    continue
                print(f"Qualifying and running {exp.name}, repetition {repeat+1}", flush=True)
                location = output / f"{exp.name}-repeat-{repeat+1}"
                async def database_check(fixtures, destination):
                    return await asyncio.to_thread(capture_history, postgres_id, project, fixtures, exp, destination)
                result = await run_experiment(clients, exp, location, observations, token, observer, database_check)
                result["repetition"] = repeat+1
                write_json_atomic(location / "summary.json", result)
                summaries.append(result)
                report(output)
                if not result["valid"]:
                    return {"passed": False, "reason": "invalid_experiment", "completed_runs": len(summaries)}
    return {"passed": bool(summaries) and all(x["valid"] for x in summaries), "completed_runs": len(summaries),
            "production_qualified": False}


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command", required=True)
    validate = sub.add_parser("validate", help="Offline contract/AST manifest validation; no requests")
    validate.add_argument("--config", type=Path, required=True)
    local = sub.add_parser("local", help="Own a NEW authenticated shared PostgreSQL stack; no remote targets")
    local.add_argument("--config", type=Path, required=True)
    local.add_argument("--output", type=Path, required=True)
    local.add_argument("--execute", action="store_true", help="Required explicit disposable execution opt-in")
    local.add_argument("--max-history-rows", type=int, default=10000, help="Total seeded rows across tenants/repetitions")
    local.add_argument("--max-scheduled-seconds", type=float, default=600)
    local.add_argument("--reuse-images-from", help="Prior system-tests-* project; recorded as reused images")
    reporting = sub.add_parser("report")
    reporting.add_argument("--output", type=Path, required=True)
    matrix = sub.add_parser("matrix", help="Write progressive examples without running them")
    matrix.add_argument("--output", type=Path, required=True)
    args = parser.parse_args(argv)
    if args.command == "report":
        report(args.output)
        return 0
    if args.command == "matrix":
        entries = [replace(Experiment(), name=f"L1-{x}", families=(x,), read_mode="direct_db") for x in SCALARS]
        entries += [replace(Experiment(), name=f"L2-R{r}", rules=r, read_mode="direct_db") for r in (2,5,10,25,50,100)]
        entries += [replace(Experiment(), name=f"L3-S{s}", scenarios=s, route="all", read_mode="direct_db") for s in (2,5,10,25,50)]
        entries += [replace(Experiment(), name=f"L4-H{h}", families=("count","sum","avg","count_distinct","min","max"),
                            rules=6, history_rows=h, read_mode="direct_db") for h in (0,10,10000,100000,1000000,5000000)]
        entries += [replace(Experiment(), name="L5-mixed", route="all", scenarios=5, rules=10,
                            families=(*SCALARS,"count","sum","avg"), history_rows=10000, tenants=2, padding_bytes=10240, read_mode="direct_db"),
                    replace(Experiment(), name="L6-soak", mode="closed", duration=7200, warmup=30, read_mode="direct_db")]
        args.output.parent.mkdir(parents=True, exist_ok=True)
        with args.output.open("x", encoding="utf-8") as file:
            json.dump({"schema_version": VERSION, "experiments": [x.manifest() for x in entries]}, file, indent=2)
        return 0
    experiments = load(args.config)
    if args.command == "validate":
        print(json.dumps({"valid": True, "experiments": len(experiments), "manifest_sha256": digest([x.manifest() for x in experiments]),
                          "formula_hashes": {x.name: [digest(formula(x, i)) for i in range(x.rules)] for x in experiments}}, indent=2))
        return 0
    if not args.execute:
        parser.error("local requires --execute; validate is entirely offline")
    rows = sum((x.history_rows*x.tenants + math.ceil((x.duration+x.warmup)*x.ingestion_rate))*x.repeats for x in experiments)
    seconds = sum((x.duration+x.warmup)*x.repeats for x in experiments)
    if (args.max_history_rows < 0 or not math.isfinite(args.max_scheduled_seconds) or args.max_scheduled_seconds <= 0):
        parser.error("budgets must be finite and nonnegative (time must be positive)")
    if rows > args.max_history_rows or seconds > args.max_scheduled_seconds:
        parser.error("campaign exceeds explicit local row/time budgets")
    token = secrets.token_urlsafe(32)
    source = source_manifest(Path(__file__).resolve().parents[4])
    old = os.environ.get("DECISION_BENCHMARK_TOKEN")
    os.environ["DECISION_BENCHMARK_TOKEN"] = token
    async def workload(output, endpoints, project, worker, postgres):
        return await exercise(output, endpoints, project, worker, postgres, experiments, token, source)
    try:
        run_fixture(args.output, args.reuse_images_from, workload,
                    compose_factory=lambda project, image: fixture(project, image, experiments[0].read_mode),
                    scope="decision_rule_correctness_and_discovery_not_production_capacity")
    finally:
        if old is None:
            os.environ.pop("DECISION_BENCHMARK_TOKEN", None)
        else:
            os.environ["DECISION_BENCHMARK_TOKEN"] = old
        if args.output.exists():
            report(args.output)
    return 0
