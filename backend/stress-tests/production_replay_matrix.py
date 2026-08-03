from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parents[2]
DEFAULT_OUTPUT_ROOT = ROOT / "backend" / "stress-tests" / "production-replay-matrix-runs"


@dataclass(frozen=True)
class Experiment:
    label: str
    decision_mode: str
    live_decision_mode: str
    live_async_fallback_enabled: bool
    ingestion_trigger_decision_mode: str
    ingestion_trigger_overload_mode: str
    tenant_data_read_mode: str
    separate_read_pool: bool


def utc_stamp() -> str:
    return datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S-%fZ")


def build_default_experiments() -> list[Experiment]:
    return [
        Experiment(
            label="async-only-ingestion-http-shared-read-pool",
            decision_mode="async",
            live_decision_mode="async_only",
            live_async_fallback_enabled=True,
            ingestion_trigger_decision_mode="async_only",
            ingestion_trigger_overload_mode="defer_async",
            tenant_data_read_mode="ingestion_http",
            separate_read_pool=False,
        ),
        Experiment(
            label="async-only-ingestion-http-separate-read-pool",
            decision_mode="async",
            live_decision_mode="async_only",
            live_async_fallback_enabled=True,
            ingestion_trigger_decision_mode="async_only",
            ingestion_trigger_overload_mode="defer_async",
            tenant_data_read_mode="ingestion_http",
            separate_read_pool=True,
        ),
        Experiment(
            label="async-only-direct-db-separate-read-pool",
            decision_mode="async",
            live_decision_mode="async_only",
            live_async_fallback_enabled=True,
            ingestion_trigger_decision_mode="async_only",
            ingestion_trigger_overload_mode="defer_async",
            tenant_data_read_mode="direct_db",
            separate_read_pool=True,
        ),
        Experiment(
            label="sync-direct-db-separate-read-pool-with-fallback",
            decision_mode="sync",
            live_decision_mode="sync",
            live_async_fallback_enabled=True,
            ingestion_trigger_decision_mode="sync",
            ingestion_trigger_overload_mode="defer_async",
            tenant_data_read_mode="direct_db",
            separate_read_pool=True,
        ),
    ]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Run or print a production replay experiment matrix across async, read-mode, and read-pool settings."
    )
    parser.add_argument("--target", choices=("local", "remote"), default="local")
    parser.add_argument("--execute", action="store_true", help="Actually run the matrix. Without this, print the plan only.")
    parser.add_argument("--output-root", default=str(DEFAULT_OUTPUT_ROOT))
    parser.add_argument("--transactions", default=os.getenv("TRANSACTIONS", "1000"))
    parser.add_argument("--multiplier", default=os.getenv("MULTIPLIER", "3600"))
    parser.add_argument("--max-in-flight", default=os.getenv("MAX_IN_FLIGHT", "50"))
    parser.add_argument("--checkpoint-every", default=os.getenv("CHECKPOINT_EVERY", "100"))
    parser.add_argument("--tenant-id", default=os.getenv("TENANT_ID", ""))
    parser.add_argument("--data-root", default=os.getenv("DATA_ROOT", "/Users/kwilson/Desktop/ITC/fraud_data"))
    parser.add_argument("--capture-metrics", action="store_true", help="Capture one runtime/read metrics snapshot after each replay run.")
    parser.add_argument("--decision-engine-url", default=os.getenv("DECISION_ENGINE_URL", "http://127.0.0.1:8082"))
    parser.add_argument("--ingestion-url", default=os.getenv("INGESTION_URL", "http://127.0.0.1:8081"))
    return parser.parse_args()


def build_command(target: str) -> list[str]:
    if target == "local":
        return ["bash", "./backend/stress-tests/production_replay/run_local_replay.sh"]
    return ["bash", "./backend/stress-tests/production_replay/run_remote_replay.sh"]


def build_env(base: dict[str, str], args: argparse.Namespace, experiment: Experiment) -> dict[str, str]:
    env = dict(base)
    env.update(
        {
            "FRAUD_DATA_ROOT": args.data_root,
            "PRODUCTION_REPLAY_TRANSACTIONS": args.transactions,
            "PRODUCTION_REPLAY_MULTIPLIER": args.multiplier,
            "PRODUCTION_REPLAY_MAX_IN_FLIGHT": args.max_in_flight,
            "PRODUCTION_REPLAY_CHECKPOINT_EVERY": args.checkpoint_every,
            "PRODUCTION_REPLAY_DECISION_MODE": experiment.decision_mode,
            "PRODUCTION_REPLAY_LIVE_DECISION_MODE": experiment.live_decision_mode,
            "PRODUCTION_REPLAY_LIVE_ASYNC_FALLBACK_ENABLED": "true" if experiment.live_async_fallback_enabled else "false",
            "PRODUCTION_REPLAY_INGESTION_TRIGGER_DECISION_MODE": experiment.ingestion_trigger_decision_mode,
            "PRODUCTION_REPLAY_INGESTION_TRIGGER_OVERLOAD_MODE": experiment.ingestion_trigger_overload_mode,
            "PRODUCTION_REPLAY_TENANT_DATA_READ_MODE": experiment.tenant_data_read_mode,
            "PRODUCTION_REPLAY_ENABLE_SEPARATE_READ_POOL": "true" if experiment.separate_read_pool else "false",
            "PRODUCTION_REPLAY_EXPERIMENT_LABEL": experiment.label,
        }
    )
    if args.tenant_id:
        env["PRODUCTION_REPLAY_TENANT_ID"] = args.tenant_id
    return env


def write_json(path: Path, payload: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")


def parse_run_dir(stdout: str) -> str | None:
    for line in reversed(stdout.splitlines()):
        if line.startswith("Results: "):
            return line.split(": ", 1)[1].strip()
        if line.startswith("replay output: "):
            return line.split(": ", 1)[1].strip()
    return None


def maybe_capture_metrics(args: argparse.Namespace, matrix_dir: Path, experiment: Experiment) -> str | None:
    command = [
        sys.executable,
        "backend/stress-tests/capture_runtime_read_metrics.py",
        "--label",
        experiment.label,
        "--samples",
        "1",
        "--decision-engine-url",
        args.decision_engine_url,
        "--ingestion-url",
        args.ingestion_url,
        "--output-dir",
        str(matrix_dir / "metrics-captures"),
    ]
    completed = subprocess.run(
        command,
        cwd=ROOT,
        capture_output=True,
        text=True,
        check=False,
    )
    if completed.returncode != 0:
        return None
    return completed.stdout.strip().splitlines()[-1].strip()


def main() -> None:
    args = parse_args()
    experiments = build_default_experiments()
    matrix_dir = Path(args.output_root).expanduser().resolve() / f"matrix-{utc_stamp()}"
    plan = {
        "matrix_version": 1,
        "created_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        "target": args.target,
        "execute": args.execute,
        "experiments": [asdict(item) for item in experiments],
    }
    write_json(matrix_dir / "matrix-plan.json", plan)

    if not args.execute:
        print(json.dumps(plan, indent=2))
        for experiment in experiments:
            print("")
            print(f"[plan] {experiment.label}")
            print("  target:", args.target)
            print("  decision_mode:", experiment.decision_mode)
            print("  live_decision_mode:", experiment.live_decision_mode)
            print("  live_async_fallback_enabled:", experiment.live_async_fallback_enabled)
            print("  ingestion_trigger_decision_mode:", experiment.ingestion_trigger_decision_mode)
            print("  ingestion_trigger_overload_mode:", experiment.ingestion_trigger_overload_mode)
            print("  tenant_data_read_mode:", experiment.tenant_data_read_mode)
            print("  separate_read_pool:", experiment.separate_read_pool)
        return

    results: list[dict[str, Any]] = []
    for experiment in experiments:
        env = build_env(os.environ, args, experiment)
        command = build_command(args.target)
        completed = subprocess.run(
            command,
            cwd=ROOT,
            env=env,
            capture_output=True,
            text=True,
            check=False,
        )
        run_dir = parse_run_dir(completed.stdout)
        metrics_manifest = maybe_capture_metrics(args, matrix_dir, experiment) if args.capture_metrics else None
        result = {
            "label": experiment.label,
            "command": command,
            "returncode": completed.returncode,
            "run_dir": run_dir,
            "metrics_capture_manifest": metrics_manifest,
            "stdout_path": str(matrix_dir / f"{experiment.label}.stdout.log"),
            "stderr_path": str(matrix_dir / f"{experiment.label}.stderr.log"),
            "settings": asdict(experiment),
        }
        (matrix_dir / f"{experiment.label}.stdout.log").write_text(completed.stdout, encoding="utf-8")
        (matrix_dir / f"{experiment.label}.stderr.log").write_text(completed.stderr, encoding="utf-8")
        results.append(result)
        write_json(matrix_dir / "matrix-results.json", {"results": results})

    print(str(matrix_dir))


if __name__ == "__main__":
    main()
