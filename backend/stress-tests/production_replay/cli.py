from __future__ import annotations

import argparse
import asyncio
import json
import os
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from .api_client import APIError, ServiceClients, ServiceConfig
from .manifest import ManifestError, ReplayManifest, load_manifest
from .profiler import profile_manifest
from .replay import ReplayCursor, ReplayMetrics, TransactionChain, schedule_events
from .scenarios import build_portable_scenarios
from .setup_environment import EnvironmentSetup
from .sorting import build_sorted_chunks, iter_merged_events


DEFAULT_OUTPUT_ROOT = Path(__file__).resolve().parent.parent / "production-replay-runs"


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="python -m production_replay",
        description="Profile, prepare, and replay production-shaped fraud transaction data.",
    )
    subparsers = parser.add_subparsers(dest="command", required=True)

    profile = subparsers.add_parser("profile", help="Read and validate sources without calling any service")
    _add_manifest(profile)
    profile.add_argument("--output", help="Optional profile JSON path")

    setup = subparsers.add_parser("setup", help="Profile sources and optionally prepare a tenant")
    _add_manifest(setup)
    _add_services(setup)
    setup.add_argument("--execute", action="store_true", help="Allow tenant and service mutations")
    setup.add_argument("--tenant-id", help="Use an existing clean tenant; omit to create one")
    setup.add_argument("--tenant-name", default="Production Replay Stress Tenant")
    setup.add_argument("--publication-timeout", type=float, default=900.0)
    setup.add_argument("--output-root", default=str(DEFAULT_OUTPUT_ROOT))

    run = subparsers.add_parser("run", help="Profile sources and optionally perform a timed replay")
    _add_manifest(run)
    _add_services(run)
    run.add_argument("--execute", action="store_true", help="Allow ingestion and decision requests")
    run.add_argument("--tenant-id", help="Tenant prepared by the setup command")
    run.add_argument("--multiplier", type=float, help="Replay speed relative to source event time")
    run.add_argument("--max-in-flight", type=int, default=500)
    run.add_argument("--sort-chunk-size", type=int, default=100_000)
    run.add_argument("--checkpoint-every", type=int, default=10_000)
    run.add_argument("--resume-from", help="Checkpoint JSON from an interrupted replay")
    run.add_argument("--output-root", default=str(DEFAULT_OUTPUT_ROOT))
    return parser


def _add_manifest(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--manifest", required=True, help="Version 1 replay manifest JSON")


def _add_services(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--data-model-url", default=os.getenv("DATA_MODEL_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--ingestion-url", default=os.getenv("INGESTION_URL", "http://127.0.0.1:8081"))
    parser.add_argument("--decision-engine-url", default=os.getenv("DECISION_ENGINE_URL", "http://127.0.0.1:8082"))
    parser.add_argument("--auth-token", default=os.getenv("SERVICE_AUTH_TOKEN"))
    parser.add_argument("--timeout", type=float, default=30.0)


def _services(args: argparse.Namespace) -> ServiceConfig:
    return ServiceConfig(
        data_model_url=args.data_model_url,
        ingestion_url=args.ingestion_url,
        decision_engine_url=args.decision_engine_url,
        auth_token=args.auth_token,
        timeout_seconds=args.timeout,
        max_connections=getattr(args, "max_in_flight", 500),
    )


def _run_id(prefix: str) -> str:
    now = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S-%fZ")
    return f"{prefix}-{now}"


def _create_run_dir(root: str, prefix: str) -> Path:
    path = Path(root).expanduser().resolve() / _run_id(prefix)
    path.mkdir(parents=True, exist_ok=False)
    return path


def _write_json(path: Path, value: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2, sort_keys=True, default=str) + "\n", encoding="utf-8")


def _write_json_atomic(path: Path, value: Any) -> None:
    temporary = path.with_name(path.name + ".tmp")
    _write_json(temporary, value)
    temporary.replace(path)


def _read_json(path: Path) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ValueError(f"could not read checkpoint {path}: {exc}") from exc
    if not isinstance(value, dict):
        raise ValueError(f"checkpoint {path} must contain a JSON object")
    return value


def _snapshot_manifest(manifest: ReplayManifest, run_dir: Path) -> None:
    raw = json.loads(manifest.path.read_text(encoding="utf-8"))
    _write_json(run_dir / "manifest.snapshot.json", raw)


def _print_profile(profile: dict[str, Any]) -> None:
    tx = profile["transactions"]
    references = profile["reference_data"]
    print(f"transactions: {tx['row_count']}")
    print(f"source time: {tx['earliest']} to {tx['latest']}")
    print(
        "source rate: "
        f"avg {tx['average_events_per_second']} eps, "
        f"p95 {tx['p95_events_per_second']} eps, "
        f"p99 {tx['p99_events_per_second']} eps, "
        f"peak {tx['peak_events_per_second']} eps"
    )
    print(
        "references: "
        f"{references['merchants']['unique_keys']} merchants, "
        f"{references['merchant_products']['unique_keys']} merchant products, "
        f"{references['staff']['source_rows']} staff rows"
    )


def _profile(manifest: ReplayManifest) -> dict[str, Any]:
    print("profiling source files (read only)...")
    result = profile_manifest(manifest)
    _print_profile(result)
    return result


async def _setup(args: argparse.Namespace, manifest: ReplayManifest, profile: dict[str, Any]) -> int:
    if not args.execute:
        print("setup not executed; pass --execute to allow service mutations")
        return 0
    if args.publication_timeout <= 0:
        raise ValueError("--publication-timeout must be positive")
    run_dir = _create_run_dir(args.output_root, "setup")
    _snapshot_manifest(manifest, run_dir)
    _write_json(run_dir / "profile.json", profile)
    async with ServiceClients(_services(args)) as clients:
        setup = EnvironmentSetup(manifest, clients, args.tenant_id, args.tenant_name)
        result = await setup.run(args.publication_timeout)
    _write_json(run_dir / "setup.json", result)
    print(f"tenant: {result['tenant_id']}")
    print(f"setup output: {run_dir}")
    return 0


async def _run_replay(args: argparse.Namespace, manifest: ReplayManifest, profile: dict[str, Any]) -> int:
    if not args.execute:
        print("replay not executed; pass --execute, --tenant-id, and --multiplier to send events")
        return 0
    if not args.tenant_id:
        raise ValueError("--tenant-id is required with --execute")
    if args.multiplier is None or args.multiplier <= 0:
        raise ValueError("a positive --multiplier is required with --execute")
    if args.max_in_flight <= 0:
        raise ValueError("--max-in-flight must be positive")
    if args.sort_chunk_size <= 0:
        raise ValueError("--sort-chunk-size must be positive")
    if args.checkpoint_every <= 0:
        raise ValueError("--checkpoint-every must be positive")

    checkpoint_state: dict[str, Any] | None = None
    resume_cursor: ReplayCursor | None = None
    if args.resume_from:
        checkpoint_path = Path(args.resume_from).expanduser().resolve()
        checkpoint_state = _read_json(checkpoint_path)
        if checkpoint_state.get("checkpoint_version") != 1:
            raise ValueError("unsupported replay checkpoint version")
        if checkpoint_state.get("tenant_id") != args.tenant_id:
            raise ValueError("checkpoint tenant does not match --tenant-id")
        if float(checkpoint_state.get("multiplier", 0)) != args.multiplier:
            raise ValueError("checkpoint multiplier does not match --multiplier")
        if checkpoint_state.get("source_fingerprint") != profile.get("source_fingerprint"):
            raise ValueError("checkpoint source fingerprint does not match the current manifest and files")
        cursor_value = checkpoint_state.get("cursor")
        resume_cursor = ReplayCursor.from_dict(cursor_value) if isinstance(cursor_value, dict) else None
        run_dir = checkpoint_path.parent
        print(f"resuming replay from {checkpoint_path}")
    else:
        run_dir = _create_run_dir(args.output_root, "replay")
        checkpoint_path = run_dir / "checkpoint.json"
        _snapshot_manifest(manifest, run_dir)
        _write_json(run_dir / "profile.json", profile)
    _write_json(
        run_dir / "run-config.json",
        {
            "tenant_id": args.tenant_id,
            "multiplier": args.multiplier,
            "max_in_flight": args.max_in_flight,
            "sort_chunk_size": args.sort_chunk_size,
            "checkpoint_every": args.checkpoint_every,
            "resume_from": str(Path(args.resume_from).expanduser().resolve()) if args.resume_from else None,
            "service_urls": {
                "data_model": args.data_model_url,
                "ingestion": args.ingestion_url,
                "decision_engine": args.decision_engine_url,
            },
            "auth_token": "set" if args.auth_token else None,
            "acceptance_thresholds": None,
        },
    )

    metrics = ReplayMetrics.from_state(checkpoint_state.get("metrics", {})) if checkpoint_state else ReplayMetrics()
    original_source_start = _parse_iso(checkpoint_state.get("source_start")) if checkpoint_state else None
    original_source_end = _parse_iso(checkpoint_state.get("source_end")) if checkpoint_state else None
    source_start = original_source_start
    source_end = original_source_end
    with tempfile.TemporaryDirectory(prefix="sort-", dir=run_dir) as temp_dir:
        print("sorting and merging transaction streams...")
        sort_result = await asyncio.to_thread(
            build_sorted_chunks, manifest, Path(temp_dir), args.sort_chunk_size
        )
        profiled_count = int(profile["transactions"]["row_count"])
        if sort_result.event_count != profiled_count:
            raise ValueError(
                f"transaction sources changed after profiling: profiled {profiled_count}, sorted {sort_result.event_count}"
            )
        if manifest.source_fingerprint() != profile["source_fingerprint"]:
            raise ValueError("source files changed between profiling and replay preparation")
        print(f"replaying {sort_result.event_count} events at {args.multiplier:g}x...")
        async with ServiceClients(_services(args)) as clients:
            await clients.wait_until_ready()
            await _verify_replay_tenant(clients, manifest, args.tenant_id)
            chain = TransactionChain(clients, args.tenant_id, metrics, args.max_in_flight)

            async def save_checkpoint(cursor: ReplayCursor, batch_start: datetime, batch_end: datetime) -> None:
                nonlocal original_source_start, original_source_end
                original_source_start = original_source_start or batch_start
                original_source_end = batch_end
                _write_replay_checkpoint(
                    checkpoint_path,
                    manifest=manifest,
                    source_fingerprint=profile["source_fingerprint"],
                    tenant_id=args.tenant_id,
                    multiplier=args.multiplier,
                    cursor=cursor,
                    metrics=metrics,
                    source_start=original_source_start,
                    source_end=original_source_end,
                    expected_events=sort_result.event_count,
                )
                print(f"checkpoint: {metrics.completed} / {sort_result.event_count} completed")

            if checkpoint_state is None:
                _write_replay_checkpoint(
                    checkpoint_path,
                    manifest=manifest,
                    source_fingerprint=profile["source_fingerprint"],
                    tenant_id=args.tenant_id,
                    multiplier=args.multiplier,
                    cursor=None,
                    metrics=metrics,
                    source_start=None,
                    source_end=None,
                    expected_events=sort_result.event_count,
                )
            resumed_start, resumed_end = await schedule_events(
                iter_merged_events(sort_result.chunk_paths),
                multiplier=args.multiplier,
                max_in_flight=args.max_in_flight,
                processor=chain,
                metrics=metrics,
                resume_after=resume_cursor,
                checkpoint_every=args.checkpoint_every,
                checkpoint=save_checkpoint,
            )
            source_start = original_source_start or resumed_start
            source_end = resumed_end or original_source_end

    summary = metrics.summary(multiplier=args.multiplier, source_start=source_start, source_end=source_end)
    summary["tenant_id"] = args.tenant_id
    summary["manifest"] = str(manifest.path)
    summary["source_fingerprint"] = profile["source_fingerprint"]
    summary["resumed"] = checkpoint_state is not None
    summary["checkpoint"] = str(checkpoint_path)
    _write_json(run_dir / "summary.json", summary)
    with (run_dir / "errors.ndjson").open("w", encoding="utf-8") as handle:
        for error in metrics.errors:
            handle.write(json.dumps(error, sort_keys=True) + "\n")
    print(f"status: {summary['status']}")
    print(f"completed: {summary['completed']} / {summary['scheduled']}")
    print(f"replay output: {run_dir}")
    return 0 if summary["status"] == "completed" else 2


def _write_replay_checkpoint(
    path: Path,
    *,
    manifest: ReplayManifest,
    source_fingerprint: str,
    tenant_id: str,
    multiplier: float,
    cursor: ReplayCursor | None,
    metrics: ReplayMetrics,
    source_start: datetime | None,
    source_end: datetime | None,
    expected_events: int,
) -> None:
    _write_json_atomic(
        path,
        {
            "checkpoint_version": 1,
            "updated_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
            "manifest": str(manifest.path),
            "source_fingerprint": source_fingerprint,
            "tenant_id": tenant_id,
            "multiplier": multiplier,
            "expected_events": expected_events,
            "cursor": vars(cursor) if cursor is not None else None,
            "source_start": _iso(source_start),
            "source_end": _iso(source_end),
            "metrics": metrics.to_state(),
        },
    )


def _parse_iso(value: Any) -> datetime | None:
    if not value:
        return None
    return datetime.fromisoformat(str(value).replace("Z", "+00:00"))


def _iso(value: datetime | None) -> str | None:
    return value.isoformat().replace("+00:00", "Z") if value else None


async def _verify_replay_tenant(clients: ServiceClients, manifest: ReplayManifest, tenant_id: str) -> None:
    model = await clients.request(clients.data_model, "GET", f"/v1/tenants/{tenant_id}/data-model", 200)
    tables = model.get("data_model", {}).get("tables", {})
    if "transactions" not in tables:
        raise APIError("tenant does not have the production replay transactions model; run setup first")
    response = await clients.request(clients.decision_engine, "GET", f"/v1/tenants/{tenant_id}/scenarios", 200)
    scenarios = {item["name"]: item for item in response.get("scenarios", [])}
    expected = {item.name for item in build_portable_scenarios(manifest)}
    missing = sorted(name for name in expected if name not in scenarios or not scenarios[name].get("live_iteration_id"))
    if missing:
        raise APIError("tenant is missing live production replay scenarios: " + ", ".join(missing))


async def async_main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    manifest = load_manifest(args.manifest)
    profile = await asyncio.to_thread(_profile, manifest)
    if args.command == "profile":
        if args.output:
            output = Path(args.output).expanduser().resolve()
            _write_json(output, profile)
            print(f"profile output: {output}")
        else:
            print(json.dumps(profile, indent=2, sort_keys=True))
        return 0
    if args.command == "setup":
        return await _setup(args, manifest, profile)
    if args.command == "run":
        return await _run_replay(args, manifest, profile)
    raise AssertionError(f"unhandled command: {args.command}")


def main(argv: list[str] | None = None) -> None:
    try:
        raise SystemExit(asyncio.run(async_main(argv)))
    except (APIError, ManifestError, ValueError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(1) from exc


if __name__ == "__main__":
    main()
