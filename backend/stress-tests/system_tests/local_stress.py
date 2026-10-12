"""Create a disposable local database, tenant and scenarios, then generate load."""
from __future__ import annotations

import argparse
import asyncio
import math
from pathlib import Path

from production_replay.api_client import ServiceClients, ServiceConfig
from production_replay.benchmark_reporting import write_json_atomic
from production_replay.manifest import load_manifest
from production_replay.setup_environment import EnvironmentSetup
from .prepare_inputs import prepare, synthetic_fields
from .queue_campaign import async_main as run_experiment, parse_stage
from .service_pilot import fixture, run_fixture


def local_fixture(project: str, image_prefix: str | None = None) -> dict:
    result = fixture(project, image_prefix)
    # Use the container's disk for load runs, rather than the smoke pilot's RAM disk.
    # The anonymous volume belongs only to this disposable project.
    postgres = result["services"]["postgres"]
    postgres.pop("tmpfs")
    postgres["volumes"] = ["/var/lib/postgresql/data"]
    return result


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True, help="New artifact directory")
    parser.add_argument("--stage", action="append", type=parse_stage,
                        help="NAME:transactions-per-second:seconds; repeat for a ramp (default load:10:30)")
    parser.add_argument("--path", choices=("ingestion", "ingest-sync", "ingest-async"), default="ingest-sync")
    parser.add_argument("--max-inflight", type=int, default=20)
    parser.add_argument("--deadline", type=float, default=30)
    parser.add_argument("--completion-timeout", type=float, default=60)
    parser.add_argument("--max-scheduling-lag", type=float, default=1)
    parser.add_argument("--reuse-images-from", help="Prior system-tests-* project's images; does not rebuild code")
    return parser


def validate(args) -> None:
    args.stage = args.stage or [parse_stage("load:10:30")]
    if len({stage.name for stage in args.stage}) != len(args.stage):
        raise ValueError("stage names must be unique")
    if sum(stage.count for stage in args.stage) > 1_000_000 or sum(stage.seconds for stage in args.stage) > 86400:
        raise ValueError("local run limit is 1 million transactions and 24 hours")
    if args.max_inflight <= 0:
        raise ValueError("max-inflight must be positive")
    for name in ("deadline", "completion_timeout", "max_scheduling_lag"):
        if not math.isfinite(getattr(args, name)) or getattr(args, name) <= 0:
            raise ValueError(f"{name} must be finite and positive")


async def exercise(output: Path, endpoints: dict[str, str], args) -> dict:
    source = output / "source"
    source.mkdir()
    manifest_path = source / "manifest.json"
    write_json_atomic(manifest_path, {"version": 1, "timezone": "UTC",
        "reference_data": {"merchant_globs": ["unused.json"], "merchant_product_globs": ["unused.json"], "staff_csv": "unused.csv"},
        "transaction_streams": [{"id": "synthetic", "adapter": "production_transaction_csv_v1",
            "globs": ["unused.csv"], "channel": "wallet", "direction": "incoming", "system_type": "wallet_transfer"}]})
    async with ServiceClients(ServiceConfig(endpoints["data-model"], endpoints["ingestion"], endpoints["decision-engine"])) as clients:
        setup = await asyncio.wait_for(EnvironmentSetup(load_manifest(manifest_path), clients, None,
            "Local generated stress test", scenario_set="internal").run(publication_timeout_seconds=120), timeout=180)
    write_json_atomic(output / "setup.json", setup)
    count = sum(stage.count for stage in args.stage)
    cohort = source / "transactions.ndjson"
    prepare(cohort, synthetic_fields("local-stress", count), count)
    command = ["--input", str(cohort), "--output", str(output / "workload"),
        "--tenant-id", setup["tenant_id"], "--expected-scenarios", str(len(setup["scenarios"])),
        "--path", args.path, "--max-inflight", str(args.max_inflight), "--deadline", str(args.deadline),
        "--completion-timeout", str(args.completion_timeout), "--max-scheduling-lag", str(args.max_scheduling_lag),
        "--auth-token-env", "LOCAL_STRESS_UNUSED_AUTH_TOKEN",
        "--data-model-url", endpoints["data-model"], "--ingestion-url", endpoints["ingestion"],
        "--decision-engine-url", endpoints["decision-engine"]]
    for stage in args.stage:
        command.extend(["--stage", f"{stage.name}:{stage.rate}:{stage.seconds}"])
    code = await run_experiment(command)
    return {"passed": code == 0, "exit_code": code, "tenant_id": setup["tenant_id"],
            "scenarios": len(setup["scenarios"]), "planned_transactions": count, "path": args.path,
            "production_qualified": False}


def main() -> None:
    parser = build_parser()
    args = parser.parse_args()
    try:
        validate(args)
    except ValueError as exc:
        parser.error(str(exc))

    async def workload(output, endpoints, _project, _worker, _postgres):
        return await exercise(output, endpoints, args)

    run_fixture(args.output, args.reuse_images_from, workload, compose_factory=local_fixture,
                scope="local_generated_load_not_production_capacity")


if __name__ == "__main__":
    main()
