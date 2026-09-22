from __future__ import annotations

import argparse
import asyncio
import hashlib
import json
import os
import re
import subprocess
import sys
import tempfile
import time
from collections import Counter
from collections.abc import Callable, Iterable, Iterator
from dataclasses import dataclass, field, replace
from datetime import datetime, timezone
from pathlib import Path
from typing import Any
from urllib.parse import quote

from .api_client import APIError, ServiceClients, ServiceConfig
from .domain import TransactionEvent
from .manifest import ReplayManifest, load_manifest
from .privacy import EXPLICITLY_DROPPED_PII_FIELDS, INTERNAL_RETAINED_FIELDS, InternalPrivacyTransformer
from .replay import LatencyMetric
from .scenarios import SCENARIO_SET_INTERNAL, build_portable_scenarios
from .setup_environment import EnvironmentSetup
from .sorting import build_sorted_chunks, iter_merged_events


DEFAULT_OUTPUT_ROOT = Path(__file__).resolve().parent.parent / "database-scale-runs"
PROTECTED_DATABASES = frozenset({"postgres", "template0", "template1"})
DATABASE_NAME_RE = re.compile(r"^[A-Za-z_][A-Za-z0-9_$-]{0,62}$")
MIGRATION_SERVICES = (
    "data-model-migrate",
    "ingestion-migrate",
    "decision-engine-migrate",
    "screening-migrate",
)
RUNTIME_SERVICES = (
    "data-model-service",
    "ingestion-service",
    "decision-engine-service",
    "screening-service",
    "data-model-worker",
)
QUIESCED_SERVICES = RUNTIME_SERVICES + (
    "ingestion-worker",
    "decision-engine-worker",
    "screening-worker",
    "frontend",
)


@dataclass(frozen=True)
class PhasePlan:
    name: str
    seed_count: int
    seed_factory: Callable[[], Iterator[TransactionEvent]]
    evaluation_factory: Callable[[], Iterator[TransactionEvent]]
    description: str


@dataclass
class PipelineMetrics:
    target: int
    started_at: float = field(default_factory=time.perf_counter)
    ingestion_first_at: float | None = None
    ingestion_last_at: float | None = None
    decision_first_at: float | None = None
    decision_last_at: float | None = None
    ingestion_successes: int = 0
    ingestion_failures: int = 0
    ingestion_retries: int = 0
    decision_attempts: int = 0
    decision_successes: int = 0
    decision_failures: int = 0
    ingestion_active: int = 0
    decision_active: int = 0
    max_ingestion_concurrency: int = 0
    max_decision_concurrency: int = 0
    ingestion_latency: LatencyMetric = field(default_factory=LatencyMetric)
    decision_latency: LatencyMetric = field(default_factory=LatencyMetric)
    ingestion_errors: Counter[str] = field(default_factory=Counter)
    decision_errors: Counter[str] = field(default_factory=Counter)
    segments: list[dict[str, Any]] = field(default_factory=list)
    _segment_started_at: float = field(default_factory=time.perf_counter)
    _segment_started_count: int = 0

    def record_segment_if_needed(self, every: int) -> None:
        while self.decision_attempts - self._segment_started_count >= every:
            now = time.perf_counter()
            completed = self._segment_started_count + every
            elapsed = now - self._segment_started_at
            self.segments.append(
                {
                    "through": completed,
                    "requests": every,
                    "elapsed_seconds": round(elapsed, 3),
                    "evaluations_per_second": round(every / elapsed, 2) if elapsed > 0 else None,
                }
            )
            self._segment_started_count = completed
            self._segment_started_at = now

    def summary(self) -> dict[str, Any]:
        finished = time.perf_counter()
        overall = finished - self.started_at
        ingestion_elapsed = _elapsed(self.ingestion_first_at, self.ingestion_last_at)
        decision_elapsed = _elapsed(self.decision_first_at, self.decision_last_at)
        return {
            "target_evaluations": self.target,
            "elapsed_seconds": round(overall, 3),
            "pipeline_evaluations_per_second": round(self.decision_attempts / overall, 2) if overall else None,
            "ingestion": {
                "successes": self.ingestion_successes,
                "failures": self.ingestion_failures,
                "retries": self.ingestion_retries,
                "requests_per_second": round(self.ingestion_successes / ingestion_elapsed, 2)
                if ingestion_elapsed
                else None,
                "latency": self.ingestion_latency.summary(),
                "max_observed_concurrency": self.max_ingestion_concurrency,
                "errors": dict(self.ingestion_errors),
            },
            "decision": {
                "attempts": self.decision_attempts,
                "successes": self.decision_successes,
                "failures": self.decision_failures,
                "evaluations_per_second": round(self.decision_attempts / decision_elapsed, 2)
                if decision_elapsed
                else None,
                "latency": self.decision_latency.summary(),
                "max_observed_concurrency": self.max_decision_concurrency,
                "errors": dict(self.decision_errors),
            },
            "segments": self.segments,
        }


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Run the four-phase database-volume performance suite")
    parser.add_argument("--manifest", required=True)
    parser.add_argument(
        "--data-root",
        help="Rebase all manifest source paths to this directory (useful when the manifest was authored elsewhere)",
    )
    parser.add_argument("--database-name", required=True)
    parser.add_argument(
        "--allow-drop-database",
        required=True,
        help="Safety acknowledgement; must exactly equal --database-name",
    )
    parser.add_argument("--pg-host", required=True)
    parser.add_argument("--pg-port", type=int, default=5432)
    parser.add_argument("--pg-user", required=True)
    parser.add_argument("--pg-password-env", default="PGPASSWORD")
    parser.add_argument("--pg-admin-database", default="postgres")
    parser.add_argument("--pg-sslmode", choices=("verify-full", "verify-ca"), default="verify-full")
    parser.add_argument("--pg-sslrootcert", required=True)
    parser.add_argument("--pii-key-file", required=True)
    parser.add_argument("--ingestion-concurrency", type=int, required=True)
    parser.add_argument("--evaluation-concurrency", type=int, required=True)
    parser.add_argument("--evaluation-count", type=int, default=1_000_000)
    parser.add_argument("--seed-batch-size", type=int, default=500)
    parser.add_argument("--seed-concurrency", type=int, default=10)
    parser.add_argument("--segment-size", type=int, default=100_000)
    parser.add_argument("--sort-chunk-size", type=int, default=100_000)
    parser.add_argument("--same-month", help="Optional YYYY-MM containing at least 6M records")
    parser.add_argument("--seed-month", help="Optional YYYY-MM whose following month contains evaluation records")
    parser.add_argument("--data-model-url", default=os.getenv("DATA_MODEL_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--ingestion-url", default=os.getenv("INGESTION_URL", "http://127.0.0.1:8081"))
    parser.add_argument(
        "--decision-engine-url",
        default=os.getenv("DECISION_ENGINE_URL", "http://127.0.0.1:8082"),
    )
    parser.add_argument("--auth-token", default=os.getenv("SERVICE_AUTH_TOKEN"))
    parser.add_argument("--request-timeout", type=float, default=60.0)
    parser.add_argument("--publication-timeout", type=float, default=1800.0)
    parser.add_argument("--compose-file", default="docker-compose.yml")
    parser.add_argument("--compose-project", help="Compose project name; omit to use Compose's normal project name")
    parser.add_argument("--output-root", default=str(DEFAULT_OUTPUT_ROOT))
    parser.add_argument(
        "--no-manage-services",
        action="store_true",
        help="Do not migrate/recreate Compose services; the caller must point running services at each recreated DB",
    )
    return parser


def _validate_args(args: argparse.Namespace) -> None:
    if not DATABASE_NAME_RE.fullmatch(args.database_name):
        raise ValueError("--database-name contains unsupported characters")
    if args.database_name.casefold() in PROTECTED_DATABASES:
        raise ValueError(f"refusing to drop protected database {args.database_name!r}")
    if args.allow_drop_database != args.database_name:
        raise ValueError("--allow-drop-database must exactly equal --database-name")
    if args.pg_admin_database == args.database_name:
        raise ValueError("--pg-admin-database must differ from --database-name")
    if not (1 <= args.pg_port <= 65535):
        raise ValueError("--pg-port must be between 1 and 65535")
    for name in ("ingestion_concurrency", "evaluation_concurrency", "evaluation_count", "segment_size"):
        if getattr(args, name) <= 0:
            raise ValueError(f"--{name.replace('_', '-')} must be positive")
    if not 1 <= args.seed_batch_size <= 500:
        raise ValueError("--seed-batch-size must be between 1 and 500")
    if args.seed_concurrency <= 0 or args.sort_chunk_size <= 0:
        raise ValueError("seed concurrency and sort chunk size must be positive")
    _validate_month(args.same_month, "--same-month")
    _validate_month(args.seed_month, "--seed-month")
    certificate = Path(args.pg_sslrootcert).expanduser().resolve()
    if not certificate.is_file():
        raise ValueError(f"Postgres CA certificate does not exist: {certificate}")
    key_file = Path(args.pii_key_file).expanduser().resolve()
    if not key_file.is_file():
        raise ValueError(f"PII key file does not exist: {key_file}")
    password = os.getenv(args.pg_password_env)
    if password is None:
        raise ValueError(f"password environment variable {args.pg_password_env!r} is not set")


def _validate_month(value: str | None, flag: str) -> None:
    if value is not None and not re.fullmatch(r"\d{4}-(0[1-9]|1[0-2])", value):
        raise ValueError(f"{flag} must use YYYY-MM")


class DatabaseController:
    def __init__(self, args: argparse.Namespace, root: Path) -> None:
        self.args = args
        self.root = root
        self.password = os.environ[args.pg_password_env]
        self.certificate = Path(args.pg_sslrootcert).expanduser().resolve()
        self.compose_file = Path(args.compose_file).expanduser().resolve()
        self.compose_override = self.root / "docker-compose.database-scale.yml"
        if not self.compose_file.is_file():
            raise ValueError(f"Compose file does not exist: {self.compose_file}")
        if not self.compose_override.is_file() and not args.no_manage_services:
            raise ValueError(f"Compose database-scale override does not exist: {self.compose_override}")

    def admin_env(self) -> dict[str, str]:
        return {
            **os.environ,
            "PGPASSWORD": self.password,
            "PGSSLMODE": self.args.pg_sslmode,
            "PGSSLROOTCERT": str(self.certificate),
        }

    def database_url(self) -> str:
        user = quote(self.args.pg_user, safe="")
        password = quote(self.password, safe="")
        host = self.args.pg_host
        database = quote(self.args.database_name, safe="")
        return (
            f"postgres://{user}:{password}@{host}:{self.args.pg_port}/{database}"
            f"?sslmode={self.args.pg_sslmode}&sslrootcert=/run/secrets/postgres-ca.pem"
        )

    def compose_env(self) -> dict[str, str]:
        return {
            **os.environ,
            "DATABASE_SCALE_DATABASE_URL": self.database_url(),
            "DATABASE_SCALE_SSL_ROOT_CERT": str(self.certificate),
        }

    def compose_command(self, *parts: str) -> list[str]:
        command = [
            "docker",
            "compose",
            "-f",
            str(self.compose_file),
            "-f",
            str(self.compose_override),
        ]
        if self.args.compose_project:
            command.extend(("-p", self.args.compose_project))
        return [*command, *parts]

    def stop_runtime(self) -> None:
        if self.args.no_manage_services:
            return
        _run(self.compose_command("stop", *QUIESCED_SERVICES), env=self.compose_env())

    def recreate(self) -> None:
        self.stop_runtime()
        common = [
            "-h",
            self.args.pg_host,
            "-p",
            str(self.args.pg_port),
            "-U",
            self.args.pg_user,
        ]
        terminate_sql = (
            "SELECT pg_terminate_backend(pid) FROM pg_stat_activity "
            "WHERE datname = :'target_database' AND pid <> pg_backend_pid();"
        )
        _run(
            ["psql", *common, "-d", self.args.pg_admin_database, "-v", f"target_database={self.args.database_name}", "-c", terminate_sql],
            env=self.admin_env(),
        )
        _run(
            [
                "dropdb",
                *common,
                "--maintenance-db",
                self.args.pg_admin_database,
                "--if-exists",
                self.args.database_name,
            ],
            env=self.admin_env(),
        )
        _run(
            [
                "createdb",
                *common,
                "--maintenance-db",
                self.args.pg_admin_database,
                self.args.database_name,
            ],
            env=self.admin_env(),
        )

    def migrate_and_start(self) -> None:
        if self.args.no_manage_services:
            return
        env = self.compose_env()
        for service in MIGRATION_SERVICES:
            _run(self.compose_command("run", "--rm", "--no-deps", service, "up"), env=env)
        _run(
            self.compose_command("up", "-d", "--no-deps", "--force-recreate", *RUNTIME_SERVICES),
            env=env,
        )

    def stats(self) -> dict[str, int]:
        sql = (
            "SELECT pg_database_size(current_database())::bigint, "
            "COALESCE((SELECT sum(n_live_tup)::bigint FROM pg_stat_user_tables), 0);"
        )
        result = _run(
            [
                "psql",
                "-h",
                self.args.pg_host,
                "-p",
                str(self.args.pg_port),
                "-U",
                self.args.pg_user,
                "-d",
                self.args.database_name,
                "-At",
                "-F",
                "|",
                "-c",
                sql,
            ],
            env=self.admin_env(),
            capture=True,
        )
        values = result.stdout.strip().split("|")
        return {"database_bytes": int(values[0]), "estimated_user_rows": int(values[1])}

    def audit_counts(self) -> dict[str, int]:
        sql = (
            "SELECT (SELECT count(*) FROM core_ingestion.ingestion_audit), "
            "(SELECT count(*) FROM core_ingestion.outbox_events);"
        )
        result = _run(
            [
                "psql",
                "-h",
                self.args.pg_host,
                "-p",
                str(self.args.pg_port),
                "-U",
                self.args.pg_user,
                "-d",
                self.args.database_name,
                "-At",
                "-F",
                "|",
                "-c",
                sql,
            ],
            env=self.admin_env(),
            capture=True,
        )
        values = result.stdout.strip().split("|")
        return {"ingestion_audit": int(values[0]), "outbox_events": int(values[1])}


def _run(
    command: list[str],
    *,
    env: dict[str, str],
    check: bool = True,
    capture: bool = False,
) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        command,
        env=env,
        text=True,
        check=check,
        stdout=subprocess.PIPE if capture else None,
        stderr=subprocess.PIPE if capture else None,
    )


def _month(event: TransactionEvent) -> str:
    return event.occurred_at.strftime("%Y-%m")


def _rebase_manifest(manifest: ReplayManifest, data_root: str | None) -> ReplayManifest:
    if not data_root:
        return manifest
    resolved_patterns = [
        str(Path(manifest.resolve_pattern(pattern)).resolve())
        for pattern in (
            *manifest.reference_data.merchant_globs,
            *manifest.reference_data.merchant_product_globs,
            manifest.reference_data.staff_csv,
            *(
                (manifest.reference_data.merchant_watchlist_xlsx,)
                if manifest.reference_data.merchant_watchlist_xlsx
                else ()
            ),
            *(pattern for stream in manifest.transaction_streams for pattern in stream.globs),
        )
    ]
    common_root = Path(os.path.commonpath(resolved_patterns))
    if common_root.suffix or any(character in common_root.name for character in "*?["):
        common_root = common_root.parent
    if common_root == Path(common_root.anchor):
        raise ValueError("could not infer a safe common source root from the manifest")
    target_root = Path(data_root).expanduser().resolve()

    def rebase(pattern: str) -> str:
        resolved = Path(manifest.resolve_pattern(pattern)).resolve()
        try:
            relative = resolved.relative_to(common_root)
        except ValueError as exc:
            raise ValueError(f"manifest path {resolved} is outside inferred source root {common_root}") from exc
        return str(target_root / relative)

    references = replace(
        manifest.reference_data,
        merchant_globs=tuple(rebase(item) for item in manifest.reference_data.merchant_globs),
        merchant_product_globs=tuple(rebase(item) for item in manifest.reference_data.merchant_product_globs),
        staff_csv=rebase(manifest.reference_data.staff_csv),
        merchant_watchlist_xlsx=(
            rebase(manifest.reference_data.merchant_watchlist_xlsx)
            if manifest.reference_data.merchant_watchlist_xlsx
            else None
        ),
    )
    streams = tuple(
        replace(stream, globs=tuple(rebase(item) for item in stream.globs))
        for stream in manifest.transaction_streams
    )
    return replace(manifest, reference_data=references, transaction_streams=streams)


def _next_month(value: str) -> str:
    year, month = (int(part) for part in value.split("-"))
    return f"{year + (month == 12):04d}-{1 if month == 12 else month + 1:02d}"


def _month_counts(chunk_paths: tuple[Path, ...]) -> Counter[str]:
    return Counter(_month(event) for event in iter_merged_events(chunk_paths))


def _select_events(
    chunk_paths: tuple[Path, ...],
    *,
    month: str | None = None,
    skip: int = 0,
    limit: int | None = None,
) -> Iterator[TransactionEvent]:
    matched = 0
    emitted = 0
    for event in iter_merged_events(chunk_paths):
        if month is not None and _month(event) != month:
            continue
        if matched < skip:
            matched += 1
            continue
        if limit is not None and emitted >= limit:
            return
        emitted += 1
        yield event


def _build_phase_plans(
    chunk_paths: tuple[Path, ...],
    event_count: int,
    counts: Counter[str],
    args: argparse.Namespace,
) -> list[PhasePlan]:
    evaluation_count = args.evaluation_count
    if event_count < evaluation_count:
        raise ValueError(f"source has {event_count} records; baseline requires {evaluation_count}")
    same_month_required = 5_000_000 + evaluation_count
    same_month = args.same_month or next(
        (month for month in sorted(counts) if counts[month] >= same_month_required), None
    )
    if same_month is None or counts[same_month] < same_month_required:
        raise ValueError(
            f"no month contains the {same_month_required} records required for the 5M seed phase"
        )
    seed_month = args.seed_month or next(
        (
            month
            for month in sorted(counts)
            if counts[month] > 0 and counts[_next_month(month)] >= evaluation_count
        ),
        None,
    )
    if seed_month is None:
        raise ValueError("no populated month is followed by a month with enough evaluation records")
    next_month = _next_month(seed_month)
    if counts[next_month] < evaluation_count:
        raise ValueError(f"month {next_month} has fewer than {evaluation_count} evaluation records")

    def factory(**kwargs: Any) -> Callable[[], Iterator[TransactionEvent]]:
        return lambda: _select_events(chunk_paths, **kwargs)

    return [
        PhasePlan(
            "empty_database",
            0,
            factory(limit=0),
            factory(),
            "Empty database; ingest and evaluate the first evaluation set.",
        ),
        PhasePlan(
            "seed_1m_same_month",
            1_000_000,
            factory(month=same_month, limit=1_000_000),
            factory(month=same_month, skip=1_000_000),
            f"Seed 1M from {same_month}; evaluate the next set from the same month.",
        ),
        PhasePlan(
            "seed_5m_same_month",
            5_000_000,
            factory(month=same_month, limit=5_000_000),
            factory(month=same_month, skip=5_000_000),
            f"Seed 5M from {same_month}; evaluate the next set from the same month.",
        ),
        PhasePlan(
            "seed_full_month_next_month",
            counts[seed_month],
            factory(month=seed_month),
            factory(month=next_month),
            f"Seed all {counts[seed_month]} records from {seed_month}; evaluate {next_month}.",
        ),
    ]


async def _seed_phase(
    clients: ServiceClients,
    tenant_id: str,
    events: Iterable[TransactionEvent],
    expected: int,
    batch_size: int,
    concurrency: int,
) -> dict[str, Any]:
    if expected == 0:
        return {"records": 0, "batches": 0, "elapsed_seconds": 0.0, "records_per_second": None}
    started = time.perf_counter()
    batches = 0
    records = 0
    submitted_batches = 0
    pending: set[asyncio.Task[int]] = set()

    async def submit(number: int, batch: list[TransactionEvent]) -> int:
        object_ids = [event.object_id for event in batch]
        digest = hashlib.sha256((tenant_id + "\0" + "\0".join(object_ids)).encode()).hexdigest()
        response = await clients.ingest_batch(
            tenant_id,
            "transactions",
            [event.fields for event in batch],
            f"database-scale-seed:{number}:{digest}",
        )
        results = response.get("results")
        if not isinstance(results, list) or len(results) != len(batch):
            raise APIError(f"seed batch {number} returned an incomplete result set")
        return len(batch)

    async def collect(done: set[asyncio.Task[int]]) -> None:
        nonlocal batches, records
        for count in await asyncio.gather(*done):
            batches += 1
            records += count
            if records % 100_000 < batch_size:
                print(f"  seeded {records:,} / {expected:,}")

    batch: list[TransactionEvent] = []
    try:
        for event in events:
            batch.append(event)
            if len(batch) < batch_size:
                continue
            submitted_batches += 1
            pending.add(asyncio.create_task(submit(submitted_batches, batch)))
            batch = []
            if len(pending) >= concurrency:
                done, pending = await asyncio.wait(pending, return_when=asyncio.FIRST_COMPLETED)
                await collect(done)
        if batch:
            submitted_batches += 1
            pending.add(asyncio.create_task(submit(submitted_batches, batch)))
        if pending:
            await collect(pending)
    except BaseException:
        for task in pending:
            task.cancel()
        await asyncio.gather(*pending, return_exceptions=True)
        raise
    if records != expected:
        raise ValueError(f"seed selector produced {records} records; expected {expected}")
    elapsed = time.perf_counter() - started
    return {
        "records": records,
        "batches": batches,
        "elapsed_seconds": round(elapsed, 3),
        "records_per_second": round(records / elapsed, 2) if elapsed else None,
    }


async def _run_fixed_pipeline(
    clients: ServiceClients,
    tenant_id: str,
    events: Iterable[TransactionEvent],
    *,
    target: int,
    ingestion_concurrency: int,
    evaluation_concurrency: int,
    segment_size: int,
) -> dict[str, Any]:
    metrics = PipelineMetrics(target)
    iterator = iter(events)
    state_lock = asyncio.Lock()
    decision_queue: asyncio.Queue[TransactionEvent | None] = asyncio.Queue(
        maxsize=max(ingestion_concurrency, evaluation_concurrency) * 4
    )
    in_progress = 0
    source_exhausted = False

    async def claim() -> TransactionEvent | None:
        nonlocal in_progress, source_exhausted
        async with state_lock:
            if metrics.ingestion_successes + in_progress >= target or source_exhausted:
                return None
            try:
                event = next(iterator)
            except StopIteration:
                source_exhausted = True
                return None
            in_progress += 1
            return event

    async def ingestion_worker() -> None:
        nonlocal in_progress
        while True:
            event = await claim()
            if event is None:
                return
            request_started = time.perf_counter()
            metrics.ingestion_first_at = metrics.ingestion_first_at or request_started
            async with state_lock:
                metrics.ingestion_active += 1
                metrics.max_ingestion_concurrency = max(
                    metrics.max_ingestion_concurrency, metrics.ingestion_active
                )
            try:
                _response, attempts = await clients.ingest_one(
                    tenant_id,
                    "transactions",
                    event.fields,
                    _idempotency_key(tenant_id, event.object_id),
                    max_attempts=3,
                )
            except APIError as exc:
                elapsed_ms = (time.perf_counter() - request_started) * 1_000
                async with state_lock:
                    metrics.ingestion_failures += 1
                    metrics.ingestion_latency.add(elapsed_ms)
                    metrics.ingestion_errors[_error_class(exc)] += 1
                    metrics.ingestion_active -= 1
                    in_progress -= 1
            else:
                completed = time.perf_counter()
                async with state_lock:
                    metrics.ingestion_successes += 1
                    metrics.ingestion_retries += attempts - 1
                    metrics.ingestion_latency.add((completed - request_started) * 1_000)
                    metrics.ingestion_last_at = completed
                    metrics.ingestion_active -= 1
                    in_progress -= 1
                await decision_queue.put(event)

    async def decision_worker() -> None:
        while True:
            event = await decision_queue.get()
            if event is None:
                decision_queue.task_done()
                return
            request_started = time.perf_counter()
            async with state_lock:
                metrics.decision_first_at = metrics.decision_first_at or request_started
                metrics.decision_active += 1
                metrics.max_decision_concurrency = max(metrics.max_decision_concurrency, metrics.decision_active)
            try:
                await clients.record_ingested(
                    tenant_id,
                    event.object_id,
                    event.fields,
                    mode="sync",
                    source="database_scale_suite",
                )
            except APIError as exc:
                completed = time.perf_counter()
                async with state_lock:
                    metrics.decision_attempts += 1
                    metrics.decision_failures += 1
                    metrics.decision_errors[_error_class(exc)] += 1
                    metrics.decision_latency.add((completed - request_started) * 1_000)
                    metrics.decision_last_at = completed
                    metrics.decision_active -= 1
                    metrics.record_segment_if_needed(segment_size)
            else:
                completed = time.perf_counter()
                async with state_lock:
                    metrics.decision_attempts += 1
                    metrics.decision_successes += 1
                    metrics.decision_latency.add((completed - request_started) * 1_000)
                    metrics.decision_last_at = completed
                    metrics.decision_active -= 1
                    metrics.record_segment_if_needed(segment_size)
            finally:
                decision_queue.task_done()

    decision_tasks = [asyncio.create_task(decision_worker()) for _ in range(evaluation_concurrency)]
    ingestion_tasks = [asyncio.create_task(ingestion_worker()) for _ in range(ingestion_concurrency)]
    try:
        await asyncio.gather(*ingestion_tasks)
        if metrics.ingestion_successes != target:
            raise ValueError(
                f"evaluation source exhausted after {metrics.ingestion_successes} successful ingests; target is {target}"
            )
        await decision_queue.join()
        for _ in decision_tasks:
            await decision_queue.put(None)
        await asyncio.gather(*decision_tasks)
    except BaseException:
        for task in (*ingestion_tasks, *decision_tasks):
            task.cancel()
        await asyncio.gather(*ingestion_tasks, *decision_tasks, return_exceptions=True)
        raise
    if metrics.decision_attempts != target:
        raise AssertionError(f"submitted {metrics.decision_attempts} decisions; expected {target}")
    summary = metrics.summary()
    summary["configured_concurrency"] = {
        "ingestion": ingestion_concurrency,
        "evaluation": evaluation_concurrency,
    }
    return summary


def _idempotency_key(tenant_id: str, object_id: str) -> str:
    digest = hashlib.sha256(f"{tenant_id}\0{object_id}".encode()).hexdigest()
    return f"database-scale-evaluation:{digest}"


def _error_class(error: APIError) -> str:
    if error.status_code is not None:
        return f"http_{error.status_code}"
    if "timeout" in str(error).lower():
        return "timeout"
    return "transport_or_unknown"


def _elapsed(start: float | None, end: float | None) -> float | None:
    if start is None or end is None:
        return None
    return max(end - start, 1e-9)


def _service_config(args: argparse.Namespace) -> ServiceConfig:
    return ServiceConfig(
        data_model_url=args.data_model_url,
        ingestion_url=args.ingestion_url,
        decision_engine_url=args.decision_engine_url,
        auth_token=args.auth_token,
        timeout_seconds=args.request_timeout,
        max_connections=max(args.ingestion_concurrency + args.evaluation_concurrency, 20),
    )


async def _run_phase(
    phase: PhasePlan,
    args: argparse.Namespace,
    manifest: ReplayManifest,
    database: DatabaseController,
) -> dict[str, Any]:
    print(f"\n[{phase.name}] {phase.description}")
    database.recreate()
    database.migrate_and_start()
    async with ServiceClients(_service_config(args)) as clients:
        await clients.wait_until_ready(timeout_seconds=180.0)
        setup = EnvironmentSetup(
            manifest,
            clients,
            None,
            f"Database Scale {phase.name}",
            scenario_set=SCENARIO_SET_INTERNAL,
        )
        setup_result = await setup.run(args.publication_timeout)
        tenant_id = str(setup_result["tenant_id"])
        seed_result = await _seed_phase(
            clients,
            tenant_id,
            phase.seed_factory(),
            phase.seed_count,
            args.seed_batch_size,
            args.seed_concurrency,
        )
        database_before_evaluation = await asyncio.to_thread(database.stats)
        pipeline = await _run_fixed_pipeline(
            clients,
            tenant_id,
            phase.evaluation_factory(),
            target=args.evaluation_count,
            ingestion_concurrency=args.ingestion_concurrency,
            evaluation_concurrency=args.evaluation_concurrency,
            segment_size=args.segment_size,
        )
    audit_counts = database.audit_counts()
    expected_per_record_entries = phase.seed_count + args.evaluation_count
    per_record_entries_verified = all(
        count >= expected_per_record_entries for count in audit_counts.values()
    )
    if not per_record_entries_verified:
        raise ValueError(
            "per-record audit/outbox verification failed: "
            f"expected at least {expected_per_record_entries}, found {audit_counts}"
        )
    return {
        "phase": phase.name,
        "description": phase.description,
        "tenant_id": tenant_id,
        "seed": seed_result,
        "evaluation": pipeline,
        "database": {
            "before_evaluation": database_before_evaluation,
            "after_evaluation": database.stats(),
            "per_record_entries": {
                **audit_counts,
                "expected_minimum_each": expected_per_record_entries,
                "verified": per_record_entries_verified,
            },
        },
    }


def _acceptance(phases: list[dict[str, Any]]) -> dict[str, Any]:
    baseline_evaluation = phases[0]["evaluation"]
    baseline_decision = baseline_evaluation["decision"]
    baseline_ingestion = baseline_evaluation["ingestion"]
    baseline_decision_rate = baseline_decision["evaluations_per_second"]
    baseline_decision_p95 = baseline_decision["latency"]["p95_ms"]
    baseline_ingestion_rate = baseline_ingestion["requests_per_second"]
    baseline_ingestion_p95 = baseline_ingestion["latency"]["p95_ms"]
    comparisons: list[dict[str, Any]] = []
    passed = True
    for phase in phases:
        evaluation = phase["evaluation"]
        decision = evaluation["decision"]
        ingestion = evaluation["ingestion"]
        decision_throughput_retention = _ratio(
            decision["evaluations_per_second"], baseline_decision_rate
        )
        decision_p95_ratio = _ratio(decision["latency"]["p95_ms"], baseline_decision_p95)
        ingestion_throughput_retention = _ratio(
            ingestion["requests_per_second"], baseline_ingestion_rate
        )
        ingestion_p95_ratio = _ratio(ingestion["latency"]["p95_ms"], baseline_ingestion_p95)
        phase_passed = bool(
            decision_throughput_retention is not None
            and decision_throughput_retention >= 0.8
            and decision_p95_ratio is not None
            and decision_p95_ratio <= 1.2
            and ingestion_throughput_retention is not None
            and ingestion_throughput_retention >= 0.8
            and ingestion_p95_ratio is not None
            and ingestion_p95_ratio <= 1.2
        )
        passed = passed and phase_passed
        comparisons.append(
            {
                "phase": phase["phase"],
                "decision_throughput_retention": round(decision_throughput_retention, 4)
                if decision_throughput_retention is not None
                else None,
                "decision_p95_latency_ratio": round(decision_p95_ratio, 4)
                if decision_p95_ratio is not None
                else None,
                "ingestion_throughput_retention": round(ingestion_throughput_retention, 4)
                if ingestion_throughput_retention is not None
                else None,
                "ingestion_p95_latency_ratio": round(ingestion_p95_ratio, 4)
                if ingestion_p95_ratio is not None
                else None,
                "passed": phase_passed,
            }
        )
    return {
        "passed": passed,
        "criteria": {
            "minimum_throughput_retention": 0.8,
            "maximum_p95_latency_ratio": 1.2,
            "applies_to": ["ingestion", "decision"],
        },
        "comparisons": comparisons,
    }


def _ratio(value: float | int | None, baseline: float | int | None) -> float | None:
    if value is None or baseline in (None, 0):
        return None
    return float(value) / float(baseline)


async def async_main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    _validate_args(args)
    manifest = _rebase_manifest(load_manifest(args.manifest), args.data_root)
    root = Path(__file__).resolve().parents[3]
    output = Path(args.output_root).expanduser().resolve() / datetime.now(timezone.utc).strftime(
        "database-scale-%Y%m%dT%H%M%S-%fZ"
    )
    output.mkdir(parents=True, exist_ok=False)
    key = Path(args.pii_key_file).expanduser().resolve().read_bytes().strip()
    privacy = InternalPrivacyTransformer(key)
    database = DatabaseController(args, root)

    with tempfile.TemporaryDirectory(prefix="database-scale-sort-", dir=output) as sort_directory:
        print("Building privacy-safe, time-sorted source chunks...")
        sort_result = await asyncio.to_thread(
            build_sorted_chunks,
            manifest,
            Path(sort_directory),
            args.sort_chunk_size,
            privacy.transform_event,
        )
        counts = await asyncio.to_thread(_month_counts, sort_result.chunk_paths)
        plans = _build_phase_plans(sort_result.chunk_paths, sort_result.event_count, counts, args)
        _write_json(
            output / "run-config.json",
            {
                "manifest": str(manifest.path),
                "data_root_override": str(Path(args.data_root).expanduser().resolve())
                if args.data_root
                else None,
                "database_name": args.database_name,
                "scenario_set": SCENARIO_SET_INTERNAL,
                "scenarios": [
                    {
                        "name": scenario.name,
                        "rules": [rule.name for rule in scenario.rules],
                    }
                    for scenario in build_portable_scenarios(manifest, SCENARIO_SET_INTERNAL)
                ],
                "evaluation_count": args.evaluation_count,
                "ingestion_concurrency": args.ingestion_concurrency,
                "evaluation_concurrency": args.evaluation_concurrency,
                "seed_concurrency": args.seed_concurrency,
                "source_event_count": sort_result.event_count,
                "source_month_counts": dict(sorted(counts.items())),
                "pii": {
                    "strategy": "HMAC-SHA256 deterministic tokenization plus data minimisation",
                    "tokenized_fields": ["account_ref", "object_id", "transaction_id"],
                    "explicitly_dropped_fields": sorted(EXPLICITLY_DROPPED_PII_FIELDS),
                    "retained_fields": sorted(INTERNAL_RETAINED_FIELDS),
                    "key_file_or_value_recorded": False,
                },
                "postgres": {
                    "host": args.pg_host,
                    "port": args.pg_port,
                    "user": args.pg_user,
                    "sslmode": args.pg_sslmode,
                    "password_recorded": False,
                },
            },
        )
        results: list[dict[str, Any]] = []
        for plan in plans:
            result = await _run_phase(plan, args, manifest, database)
            results.append(result)
            _write_json(output / f"{plan.name}.json", result)

    suite = {
        "status": "completed",
        "completed_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        "phases": results,
        "acceptance": _acceptance(results),
    }
    _write_json(output / "summary.json", suite)
    print(f"\nSuite passed: {suite['acceptance']['passed']}")
    print(f"Results: {output}")
    return 0 if suite["acceptance"]["passed"] else 2


def _write_json(path: Path, value: Any) -> None:
    path.write_text(json.dumps(value, indent=2, sort_keys=True, default=str) + "\n", encoding="utf-8")


def main(argv: list[str] | None = None) -> None:
    try:
        raise SystemExit(asyncio.run(async_main(argv)))
    except (APIError, OSError, subprocess.CalledProcessError, ValueError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(1) from exc


if __name__ == "__main__":
    main()
