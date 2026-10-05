from __future__ import annotations

import argparse
import asyncio
import hashlib
import json
import math
import os
import re
import subprocess
import sys
import tempfile
import time
from collections import Counter
from collections.abc import Callable, Iterable, Iterator
from dataclasses import asdict, dataclass, field, replace
from datetime import datetime, timezone
from pathlib import Path
from typing import Any
from urllib.parse import quote

from .api_client import APIError, ServiceClients, ServiceConfig
from .benchmark_reporting import RunReport, write_json_atomic
from .benchmark_observation import RuntimeObserver, capture_environment, capture_deployment
from .decision_completion import DecisionCompletionError, verify_decision_completion
from .identity_ledger import IdentityLedger
from .postgres_observation import PostgresObserver, counter_deltas
from .database_reconciliation import reconcile_database
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
    "case-manager-migrate",
)
RUNTIME_SERVICES = (
    "data-model-service",
    "ingestion-service",
    "decision-engine-service",
    "screening-service",
    "data-model-worker",
)
QUIESCED_SERVICES = RUNTIME_SERVICES + (
    "case-manager-service",
    "case-manager-worker",
    "screening-case-delivery-worker",
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
    ingestion_started: int = 0
    ingestion_failures: int = 0
    ingestion_retries: int = 0
    decision_attempts: int = 0
    decision_started: int = 0
    decision_deferred: int = 0
    decision_unresolved: int = 0
    decision_successes: int = 0
    decision_failures: int = 0
    ingestion_active: int = 0
    decision_active: int = 0
    max_ingestion_concurrency: int = 0
    max_decision_concurrency: int = 0
    ingestion_latency: LatencyMetric = field(default_factory=LatencyMetric)
    decision_latency: LatencyMetric = field(default_factory=LatencyMetric)
    decision_request_latency: LatencyMetric = field(default_factory=LatencyMetric)
    ingestion_errors: Counter[str] = field(default_factory=Counter)
    decision_errors: Counter[str] = field(default_factory=Counter)
    segments: list[dict[str, Any]] = field(default_factory=list)
    _segment_started_at: float = field(default_factory=time.perf_counter)
    _segment_started_count: int = 0
    _source_digest: Any = field(default_factory=hashlib.sha256, repr=False)
    source_first_at: str | None = None
    source_last_at: str | None = None

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
        pipeline_end = max(value for value in (self.ingestion_last_at, self.decision_last_at, self.ingestion_first_at)
                           if value is not None) if self.ingestion_first_at is not None else None
        pipeline_elapsed = _elapsed(self.ingestion_first_at, pipeline_end)
        ingestion_elapsed = _elapsed(self.ingestion_first_at, self.ingestion_last_at)
        decision_elapsed = _elapsed(self.decision_first_at, self.decision_last_at)
        segments = list(self.segments)
        remaining = self.decision_attempts - self._segment_started_count
        if remaining:
            elapsed = (self.decision_last_at or finished) - self._segment_started_at
            segments.append({"through": self.decision_attempts, "requests": remaining,
                             "elapsed_seconds": round(elapsed, 3), "partial": True,
                             "evaluations_per_second": round(remaining / elapsed, 2) if elapsed > 0 else None})
        return {
            "schema_version": 2,
            "target_evaluations": self.target,
            "elapsed_seconds": round(overall, 3),
            "pipeline_evaluations_per_second": round(self.decision_attempts / pipeline_elapsed, 2) if pipeline_elapsed else None,
            "pipeline_successes_per_second": round(self.decision_successes / pipeline_elapsed, 2) if pipeline_elapsed else None,
            "throughput_windows_seconds": {"ingestion": ingestion_elapsed, "decision": decision_elapsed,
                                           "end_to_end": pipeline_elapsed},
            "evaluation_source": {"sha256": self._source_digest.hexdigest(),
                                  "records_selected": self.ingestion_started,
                                  "first_event_at": self.source_first_at, "last_event_at": self.source_last_at},
            "ingestion": {
                "started": self.ingestion_started,
                "successes": self.ingestion_successes,
                "failures": self.ingestion_failures,
                "retries": self.ingestion_retries,
                "unfinished": self.ingestion_started - self.ingestion_successes - self.ingestion_failures,
                "requests_per_second": round(self.ingestion_successes / ingestion_elapsed, 2)
                if ingestion_elapsed
                else None,
                "latency": self.ingestion_latency.summary(),
                "max_observed_concurrency": self.max_ingestion_concurrency,
                "errors": dict(self.ingestion_errors),
            },
            "decision": {
                "started": self.decision_started,
                "attempts": self.decision_attempts,
                "successes": self.decision_successes,
                "failures": self.decision_failures,
                "deferred": self.decision_deferred,
                "unresolved": self.decision_unresolved + self.decision_started - self.decision_attempts,
                "pending_submission": self.ingestion_successes - self.decision_started,
                "evaluations_per_second": round(self.decision_attempts / decision_elapsed, 2)
                if decision_elapsed
                else None,
                "successful_evaluations_per_second": round(self.decision_successes / decision_elapsed, 2)
                if decision_elapsed else None,
                "latency": self.decision_latency.summary(),
                "request_latency": self.decision_request_latency.summary(),
                "max_observed_concurrency": self.max_decision_concurrency,
                "errors": dict(self.decision_errors),
            },
            "segments": segments,
        }


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Run the four-phase database-volume performance suite")
    parser.add_argument("--manifest", required=True)
    parser.add_argument(
        "--data-root",
        help="Rebase all manifest source paths to this directory (useful when the manifest was authored elsewhere)",
    )
    parser.add_argument(
        "--seed-data-root",
        help=(
            "Optional second data tree containing the preceding seed month; "
            "its transactions are merged with --data-root before month selection"
        ),
    )
    parser.add_argument(
        "--pre-sanitized-source",
        action="store_true",
        help="Trust HMAC-tokenized minimal CSVs produced by sanitize_database_scale_sources.sh",
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
    parser.add_argument("--evaluation-cohort", choices=("fixed", "phase-specific"), default="fixed",
                        help="Reuse one evaluation corpus across phases (default); phase-specific retains legacy selections")
    parser.add_argument("--evaluation-offset", type=int, default=5_000_000,
                        help="Records reserved ahead of the fixed evaluation corpus in its month")
    parser.add_argument("--database-instance-class", default="db.r7g.large",
                        help="Declared RDS instance class for the report; cannot be verified through psql")
    parser.add_argument("--verification-timeout", type=float, default=1800,
                        help="Deadline for exact post-run database reconciliation")
    parser.add_argument(
        "--phase",
        action="extend",
        nargs="+",
        choices=(
            "all", "empty", "1m", "5m", "1month",
            "empty_database", "seed_1m_same_month", "seed_5m_same_month",
            "seed_full_month_next_month",
        ),
        default=None,
        help="Phase to run; repeat this option to run multiple phases in order (default: all)",
    )
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
    parser.add_argument("--pipeline-timeout", type=float, default=86400.0,
                        help="Maximum seconds for each measured ingestion/decision pipeline")
    parser.add_argument("--deferred-policy", choices=("reject", "wait"), default="reject",
                        help="Reject async fallback, or start the decision worker and verify terminal completion")
    parser.add_argument("--decision-completion-timeout", type=float, default=60.0)
    parser.add_argument("--decision-poll-interval", type=float, default=0.5)
    parser.add_argument("--capture-metrics", action="store_true",
                        help="Collect local host and service metrics during evaluation; incomplete capture fails acceptance")
    parser.add_argument("--metrics-interval", type=float, default=5.0)
    parser.add_argument("--capture-database-metrics", action="store_true",
                        help="With --capture-metrics, sample PostgreSQL and capped public River queue prefixes")
    parser.add_argument("--metrics-timeout", type=float, default=5.0)
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
    if getattr(args, "evaluation_offset", 5_000_000) < 0:
        raise ValueError("--evaluation-offset must be nonnegative")
    if not math.isfinite(getattr(args, "verification_timeout", 1800)) or getattr(args, "verification_timeout", 1800) <= 0:
        raise ValueError("--verification-timeout must be finite and positive")
    if args.capture_database_metrics and not args.capture_metrics:
        raise ValueError("--capture-database-metrics requires --capture-metrics")
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
    for name in ("request_timeout", "publication_timeout", "pipeline_timeout",
                 "decision_completion_timeout", "decision_poll_interval", "metrics_interval", "metrics_timeout"):
        if not math.isfinite(getattr(args, name)) or getattr(args, name) <= 0:
            raise ValueError(f"--{name.replace('_', '-')} must be finite and positive")
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


def _sql_string_literal(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"


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
            # Some production Compose files interpolate these while parsing the
            # base file, before the database-scale override replaces DATABASE_URL.
            "RDS_DB_USER": self.args.pg_user,
            "RDS_DB_PASSWORD": self.password,
            "RDS_DB_HOST": self.args.pg_host,
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
        # Validate the merged project before touching services or the disposable DB.
        # Required authentication settings in the base file must not be bypassed.
        _run(self.compose_command("config", "--quiet"), env=self.compose_env(), timeout=30)
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
            f"WHERE datname = {_sql_string_literal(self.args.database_name)} "
            "AND pid <> pg_backend_pid();"
        )
        _run(
            ["psql", *common, "-d", self.args.pg_admin_database, "-c", terminate_sql],
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
        services = RUNTIME_SERVICES
        if self.args.deferred_policy == "wait":
            services += ("decision-engine-worker",)
        _run(
            self.compose_command("up", "-d", "--no-deps", "--force-recreate", *services),
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
                "-X",
                "-w",
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
            env={**self.admin_env(), "PGOPTIONS": "-c default_transaction_read_only=on -c statement_timeout=30000 -c lock_timeout=1000",
                 "PGCONNECT_TIMEOUT": "10"},
            capture=True,
            timeout=35,
        )
        values = result.stdout.strip().split("|")
        return {"database_bytes": int(values[0]), "estimated_user_rows": int(values[1])}

    def observer(self) -> PostgresObserver:
        return PostgresObserver(
            ["psql", "-h", self.args.pg_host, "-p", str(self.args.pg_port),
             "-U", self.args.pg_user, "-d", self.args.database_name],
            self.admin_env(), self.args.metrics_timeout,
        )

    async def reconcile(self, ledger: IdentityLedger, tenant_id: str, seed_count: int,
                        evaluation_count: int) -> dict[str, Any]:
        return await reconcile_database(self.observer().command, self.admin_env(), ledger, tenant_id,
                                        seed_count, evaluation_count, self.args.verification_timeout,
                                        math.ceil(seed_count / self.args.seed_batch_size))

    def deployment(self) -> dict[str, Any]:
        if self.args.no_manage_services:
            return {"status": "unavailable", "reason": "services_managed_externally"}
        services = RUNTIME_SERVICES + (("decision-engine-worker",) if self.args.deferred_policy == "wait" else ())
        return capture_deployment(self.compose_command(), self.compose_env(), services, self.args.database_name,
                                  self.args.pg_host, self.args.pg_port)



def _run(
    command: list[str],
    *,
    env: dict[str, str],
    check: bool = True,
    capture: bool = False,
    timeout: float | None = None,
) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        command,
        env=env,
        text=True,
        check=check,
        stdout=subprocess.PIPE if capture else None,
        stderr=subprocess.PIPE if capture else None,
        timeout=timeout,
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
    aliases = {
        "empty": "empty_database",
        "1m": "seed_1m_same_month",
        "5m": "seed_5m_same_month",
        "1month": "seed_full_month_next_month",
    }
    requested_phases = [aliases.get(value, value) for value in (args.phase or ["all"])]
    if len(set(requested_phases)) != len(requested_phases):
        raise ValueError("duplicate --phase selections are not allowed")
    if "all" in requested_phases and len(requested_phases) > 1:
        raise ValueError("--phase all cannot be combined with other --phase values")
    if "all" in requested_phases:
        requested_phases = [
            "empty_database",
            "seed_1m_same_month",
            "seed_5m_same_month",
            "seed_full_month_next_month",
        ]
    plans: list[PhasePlan] = []

    def factory(**kwargs: Any) -> Callable[[], Iterator[TransactionEvent]]:
        return lambda: _select_events(chunk_paths, **kwargs)

    for requested in requested_phases:
        if requested == "empty_database":
            plans.append(PhasePlan(
                "empty_database", 0, factory(limit=0), factory(),
                "Empty database; ingest and evaluate the first evaluation set.",
            ))
            continue
        if requested in ("seed_1m_same_month", "seed_5m_same_month"):
            seed_count = 5_000_000 if requested == "seed_5m_same_month" else 1_000_000
            same_month_required = seed_count + evaluation_count
            same_month = args.same_month or next(
                (month for month in sorted(counts) if counts[month] >= same_month_required), None
            )
            if same_month is None or counts[same_month] < same_month_required:
                raise ValueError(
                    f"no month contains the {same_month_required} records required for the selected seed phase"
                )
            plans.append(PhasePlan(
                requested, seed_count,
                factory(month=same_month, limit=seed_count),
                factory(month=same_month, skip=seed_count),
                f"Seed {seed_count // 1_000_000}M from {same_month}; evaluate the next set from the same month.",
            ))
            continue
        if requested == "seed_full_month_next_month":
            seed_month = args.seed_month or next(
                (month for month in sorted(counts)
                 if counts[month] > 0 and counts[_next_month(month)] >= evaluation_count +
                    (args.evaluation_offset if getattr(args, "evaluation_cohort", "phase-specific") == "fixed" else 0)),
                None,
            )
            if seed_month is None or counts[seed_month] <= 0:
                raise ValueError("no populated month is followed by a month with enough evaluation records")
            next_month = _next_month(seed_month)
            if counts[next_month] < evaluation_count:
                raise ValueError(f"month {next_month} has fewer than {evaluation_count} evaluation records")
            plans.append(PhasePlan(
                requested, counts[seed_month], factory(month=seed_month), factory(month=next_month),
                f"Seed all {counts[seed_month]} records from {seed_month}; evaluate {next_month}.",
            ))
            continue
        raise ValueError(f"unsupported phase: {requested}")
    if getattr(args, "evaluation_cohort", "phase-specific") == "fixed":
        offset = args.evaluation_offset
        seeded_same_month = max((plan.seed_count for plan in plans if plan.name in
                                 ("seed_1m_same_month", "seed_5m_same_month")), default=0)
        if offset < seeded_same_month:
            raise ValueError("--evaluation-offset must be at least the largest same-month seed to avoid overlap")
        full_month = next((plan for plan in plans if plan.name == "seed_full_month_next_month"), None)
        # The chosen evaluation month must follow the full-month seed where requested.
        if full_month:
            selected_seed = next(iter(full_month.seed_factory()))
            evaluation_month = _next_month(_month(selected_seed))
            if args.same_month and args.same_month != evaluation_month:
                raise ValueError("--same-month must be the month following --seed-month for a fixed cohort")
        else:
            evaluation_month = args.same_month or next(
                (month for month in sorted(counts) if counts[month] >= offset + evaluation_count), None)
        if evaluation_month is None or counts[evaluation_month] < offset + evaluation_count:
            raise ValueError("fixed evaluation cohort requires offset + evaluation-count records in its month")
        for i, plan in enumerate(plans):
            seed_factory = plan.seed_factory
            if plan.name in ("seed_1m_same_month", "seed_5m_same_month"):
                seed_factory = factory(month=evaluation_month, limit=plan.seed_count)
            plans[i] = replace(plan, seed_factory=seed_factory,
                               evaluation_factory=factory(month=evaluation_month, skip=offset),
                               description=f"{plan.name}: seed {plan.seed_count:,}; evaluate fixed corpus from {evaluation_month}, offset {offset:,}.")
    return plans


async def _seed_phase(
    clients: ServiceClients,
    tenant_id: str,
    events: Iterable[TransactionEvent],
    expected: int,
    batch_size: int,
    concurrency: int,
    ledger: IdentityLedger | None = None,
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
            if ledger is not None:
                ledger.register_seed(tenant_id, event.object_id)
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
    pipeline_timeout: float = 86400.0,
    allow_deferred: bool = False,
    decision_completion_timeout: float = 60.0,
    decision_poll_interval: float = 0.5,
    expected_scenarios: int | None = None,
    on_progress: Callable[[dict[str, Any]], None] | None = None,
    ledger: IdentityLedger | None = None,
    error_log_path: Path | None = None,
    phase_name: str = "",
) -> dict[str, Any]:
    if min(target, ingestion_concurrency, evaluation_concurrency, segment_size) <= 0:
        raise ValueError("pipeline counts and concurrency must be positive")
    if any(not math.isfinite(value) or value <= 0 for value in
           (pipeline_timeout, decision_completion_timeout, decision_poll_interval)):
        raise ValueError("pipeline deadlines and poll interval must be finite and positive")
    metrics = PipelineMetrics(target)
    iterator = iter(events)
    state_lock = asyncio.Lock()
    decision_queue: asyncio.Queue[TransactionEvent | None] = asyncio.Queue(
        maxsize=max(ingestion_concurrency, evaluation_concurrency) * 4
    )
    in_progress = 0
    source_exhausted = False
    error_log_lock = asyncio.Lock()
    progress_reported = 0

    async def log_error(stage: str, event: TransactionEvent, error: APIError) -> None:
        if error_log_path is None:
            return
        record = {
            "time": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
            "phase": phase_name,
            "stage": stage,
            "tenant_id": tenant_id,
            "status_code": error.status_code,
            "error_category": _error_class(error),
            "error_type": type(error).__name__,
        }
        async with error_log_lock:
            await asyncio.to_thread(_append_text, error_log_path, json.dumps(record, sort_keys=True, default=str) + "\n")

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
            if ledger is not None:
                ledger.register(tenant_id, event.object_id)
            metrics._source_digest.update(json.dumps(event.fields, sort_keys=True, separators=(",", ":"), default=str).encode() + b"\n")
            metrics.source_first_at = metrics.source_first_at or event.occurred_at.isoformat()
            metrics.source_last_at = event.occurred_at.isoformat()
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
                metrics.ingestion_started += 1
                metrics.ingestion_active += 1
                metrics.max_ingestion_concurrency = max(
                    metrics.max_ingestion_concurrency, metrics.ingestion_active
                )
            succeeded = False
            try:
                _response, attempts = await clients.ingest_one(
                    tenant_id,
                    "transactions",
                    event.fields,
                    _idempotency_key(tenant_id, event.object_id),
                    max_attempts=3,
                )
            except APIError as exc:
                if ledger is not None:
                    ledger.transition(tenant_id, event.object_id, "ingesting", "ingestion_failed", attempts=exc.attempts)
                elapsed_ms = (time.perf_counter() - request_started) * 1_000
                async with state_lock:
                    metrics.ingestion_failures += 1
                    metrics.ingestion_retries += exc.attempts - 1
                    metrics.ingestion_latency.add(elapsed_ms)
                    metrics.ingestion_errors[_error_class(exc)] += 1
                await log_error("ingestion", event, exc)
            else:
                if ledger is not None:
                    ledger.transition(tenant_id, event.object_id, "ingesting", "ingested", attempts=attempts)
                succeeded = True
                completed = time.perf_counter()
                async with state_lock:
                    metrics.ingestion_successes += 1
                    metrics.ingestion_retries += attempts - 1
                    metrics.ingestion_latency.add((completed - request_started) * 1_000)
            finally:
                async with state_lock:
                    metrics.ingestion_last_at = time.perf_counter()
                    metrics.ingestion_active -= 1
                    in_progress -= 1
            # Only successful calls reach the decision stage.
            if succeeded:
                await decision_queue.put(event)

    async def decision_worker() -> None:
        nonlocal progress_reported
        while True:
            event = await decision_queue.get()
            if event is None:
                decision_queue.task_done()
                return
            request_started = time.perf_counter()
            async with state_lock:
                metrics.decision_first_at = metrics.decision_first_at or request_started
                metrics.decision_started += 1
                metrics.decision_active += 1
                metrics.max_decision_concurrency = max(metrics.max_decision_concurrency, metrics.decision_active)
            try:
                if ledger is not None:
                    ledger.transition(tenant_id, event.object_id, "ingested", "evaluating")
                try:
                    response, status_code, _payload = await clients.record_ingested(
                        tenant_id,
                        event.object_id,
                        event.fields,
                        mode="sync",
                        source="database_scale_suite",
                    )
                finally:
                    metrics.decision_request_latency.add((time.perf_counter() - request_started) * 1_000)
                if status_code == 202 or response.get("deferred") is True:
                    metrics.decision_deferred += 1
                decision_ids = await verify_decision_completion(
                    clients, tenant_id, event.object_id, response, status_code,
                    allow_deferred=allow_deferred,
                    timeout_seconds=decision_completion_timeout,
                    poll_interval_seconds=decision_poll_interval,
                    expected_scenarios=expected_scenarios,
                )
                if ledger is not None:
                    ledger.transition(tenant_id, event.object_id, "evaluating", "completed", decisions=decision_ids)
            except APIError as exc:
                if ledger is not None:
                    state = "decision_failed" if isinstance(exc, DecisionCompletionError) and not exc.unresolved else "unresolved"
                    ledger.transition(tenant_id, event.object_id, "evaluating", state)
                completed = time.perf_counter()
                async with state_lock:
                    metrics.decision_attempts += 1
                    metrics.decision_failures += 1
                    # HTTP errors/timeouts do not establish whether the server committed a decision.
                    if not isinstance(exc, DecisionCompletionError) or exc.unresolved:
                        metrics.decision_unresolved += 1
                    metrics.decision_errors[_error_class(exc)] += 1
                    metrics.decision_latency.add((completed - request_started) * 1_000)
                    metrics.decision_last_at = completed
                    metrics.record_segment_if_needed(segment_size)
                await log_error("decision", event, exc)
                completed_decisions = metrics.decision_attempts
                if completed_decisions >= progress_reported + segment_size or completed_decisions == target:
                    progress_reported = (completed_decisions // segment_size) * segment_size
                    if completed_decisions == target:
                        progress_reported = target
                    print(f"  completed decisions {completed_decisions:,} / {target:,}", flush=True)
                if isinstance(exc, DecisionCompletionError) and exc.category == "deferred_rejected":
                    raise
            else:
                completed = time.perf_counter()
                async with state_lock:
                    metrics.decision_attempts += 1
                    metrics.decision_successes += 1
                    metrics.decision_latency.add((completed - request_started) * 1_000)
                    metrics.decision_last_at = completed
                    metrics.record_segment_if_needed(segment_size)
                completed_decisions = metrics.decision_attempts
                if completed_decisions >= progress_reported + segment_size or completed_decisions == target:
                    progress_reported = (completed_decisions // segment_size) * segment_size
                    if completed_decisions == target:
                        progress_reported = target
                    print(f"  completed decisions {completed_decisions:,} / {target:,}", flush=True)
            finally:
                metrics.decision_active -= 1
                decision_queue.task_done()

    decision_tasks = [asyncio.create_task(decision_worker()) for _ in range(evaluation_concurrency)]
    ingestion_tasks = [asyncio.create_task(ingestion_worker()) for _ in range(ingestion_concurrency)]

    async def finish_ingestion() -> None:
        await asyncio.gather(*ingestion_tasks)
        if metrics.ingestion_successes != target:
            raise ValueError(
                f"evaluation source exhausted after {metrics.ingestion_successes} successful ingests; target is {target}"
            )
        for _ in decision_tasks:
            await decision_queue.put(None)

    stop_progress = asyncio.Event()

    def snapshot() -> dict[str, Any]:
        summary = metrics.summary()
        summary["configured_concurrency"] = {
            "ingestion": ingestion_concurrency, "evaluation": evaluation_concurrency,
        }
        summary["deferred_policy"] = "wait" if allow_deferred else "reject"
        if ledger is not None:
            summary["identity_reconciliation"] = ledger.summary(target)
        return summary

    async def report_progress() -> None:
        while not stop_progress.is_set():
            if on_progress is not None:
                on_progress(snapshot())
            try:
                await asyncio.wait_for(stop_progress.wait(), timeout=5.0)
            except TimeoutError:
                continue

    producer = asyncio.create_task(finish_ingestion())

    async def run_workers() -> None:
        try:
            # Observe consumers immediately, even while producers block on a full queue.
            await asyncio.gather(producer, *decision_tasks)
        finally:
            stop_progress.set()

    coordinator = asyncio.create_task(run_workers())
    progress = asyncio.create_task(report_progress())
    tasks = [*ingestion_tasks, *decision_tasks, producer, coordinator, progress]
    try:
        await asyncio.wait_for(asyncio.gather(coordinator, progress), timeout=pipeline_timeout)
        if metrics.decision_attempts != target:
            raise AssertionError(f"submitted {metrics.decision_attempts} decisions; expected {target}")
    finally:
        for task in tasks:
            task.cancel()
        await asyncio.gather(*tasks, return_exceptions=True)
        if on_progress is not None:
            on_progress(snapshot())
    return snapshot()


def _idempotency_key(tenant_id: str, object_id: str) -> str:
    digest = hashlib.sha256(f"{tenant_id}\0{object_id}".encode()).hexdigest()
    return f"database-scale-evaluation:{digest}"


def _error_class(error: APIError) -> str:
    if isinstance(error, DecisionCompletionError):
        return error.category
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
    report: RunReport,
    error_log_path: Path | None = None,
) -> dict[str, Any]:
    with IdentityLedger(report.output / f"{phase.name}.identities.sqlite3") as ledger:
        try:
            return await _run_phase_with_ledger(phase, args, manifest, database, report, error_log_path, ledger)
        except BaseException:
            if getattr(args, "capture_database_metrics", False) and report.phase is not None:
                captured = report.phase.get("database", {})
                if "postgres_before_evaluation" in captured and "postgres_after_evaluation" not in captured:
                    snapshot = await database.observer()(storage=True)
                    report.update_phase(database={**captured, "postgres_after_failure": snapshot},
                                        telemetry={**report.phase.get("telemetry", {}), "valid": False})
            raise


async def _run_phase_with_ledger(
    phase: PhasePlan, args: argparse.Namespace, manifest: ReplayManifest,
    database: DatabaseController, report: RunReport, error_log_path: Path | None,
    ledger: IdentityLedger,
) -> dict[str, Any]:
    print(f"\n[{phase.name}] {phase.description}")
    report.start_phase(phase.name, phase.description)
    database.recreate()
    report.stage("migrating_and_starting")
    database.migrate_and_start()
    deployment = await asyncio.to_thread(database.deployment)
    report.update_phase(deployment=deployment)
    if deployment.get("status") == "error":
        raise ValueError("deployment_inventory_failed_or_wrong_database")
    async with ServiceClients(_service_config(args)) as clients:
        report.stage("setup")
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
        report.update_phase(tenant_id=tenant_id, published_scenarios=setup_result["scenarios"])
        database_before_seed = await asyncio.to_thread(database.stats)
        report.stage("seeding")
        seed_result = await _seed_phase(
            clients,
            tenant_id,
            phase.seed_factory(),
            phase.seed_count,
            args.seed_batch_size,
            args.seed_concurrency,
            ledger,
        )
        report.update_phase(seed=seed_result)
        database_before_evaluation = await asyncio.to_thread(database.stats)
        postgres_before_evaluation = None
        if getattr(args, "capture_database_metrics", False):
            postgres_before_evaluation = await database.observer()(storage=True)
        report.update_phase(database={
            "before_evaluation": database_before_evaluation,
            "postgres_before_evaluation": postgres_before_evaluation,
        })
        if postgres_before_evaluation is not None and postgres_before_evaluation.get("status") != "ok":
            raise ValueError("required PostgreSQL baseline snapshot failed; see phase JSON")
        report.stage("evaluating")
        async def evaluate() -> dict[str, Any]:
            return await _run_fixed_pipeline(
                clients,
                tenant_id,
                phase.evaluation_factory(),
                target=args.evaluation_count,
                ingestion_concurrency=args.ingestion_concurrency,
                evaluation_concurrency=args.evaluation_concurrency,
                segment_size=args.segment_size,
                pipeline_timeout=args.pipeline_timeout,
                allow_deferred=args.deferred_policy == "wait",
                decision_completion_timeout=args.decision_completion_timeout,
                decision_poll_interval=args.decision_poll_interval,
                expected_scenarios=len(setup_result["scenarios"]),
                on_progress=lambda evaluation: report.update_phase(evaluation=evaluation),
                ledger=ledger,
                error_log_path=error_log_path,
                phase_name=phase.name,
            )

        telemetry: dict[str, Any] = {"enabled": False, "valid": None}
        if args.capture_metrics:
            async with RuntimeObserver(
                report.output / f"{phase.name}.metrics.ndjson",
                {"decision": args.decision_engine_url, "ingestion": args.ingestion_url},
                auth_token=args.auth_token, interval=args.metrics_interval, timeout=args.metrics_timeout,
                database=database.observer() if getattr(args, "capture_database_metrics", False) else None,
            ) as observer:
                try:
                    pipeline = await observer.run_during(evaluate)
                finally:
                    telemetry = observer.summary()
                    report.update_phase(telemetry=telemetry)
        else:
            pipeline = await evaluate()
    postgres_after_evaluation = None
    if getattr(args, "capture_database_metrics", False):
        postgres_after_evaluation = await database.observer()(storage=True)
        boundaries_valid = all(value is not None and value.get("status") == "ok"
                               for value in (postgres_before_evaluation, postgres_after_evaluation))
        telemetry["boundary_snapshots_valid"] = boundaries_valid
        telemetry["valid"] = telemetry.get("valid") is True and boundaries_valid
        postgres_delta = counter_deltas(postgres_before_evaluation, postgres_after_evaluation)
        telemetry["database_deltas_valid"] = postgres_delta["valid"]
        telemetry["valid"] = telemetry["valid"] and postgres_delta["valid"]
    else:
        postgres_delta = None
    # Persist boundaries before expensive verification, so failures retain them.
    report.update_phase(telemetry=telemetry, database={
        "before_seed": database_before_seed, "before_evaluation": database_before_evaluation,
        "postgres_before_evaluation": postgres_before_evaluation,
        "postgres_after_evaluation": postgres_after_evaluation, "postgres_evaluation_delta": postgres_delta,
    })
    report.stage("verifying_database")
    database_after_evaluation = await asyncio.to_thread(database.stats)
    reconciliation = await database.reconcile(ledger, tenant_id, phase.seed_count, args.evaluation_count)
    pipeline["identity_reconciliation"]["durable_database_effects_verified"] = reconciliation.get("valid") is True
    record_cardinality = reconciliation.get("record_cardinality", {})
    audit_counts = {name: reconciliation.get("persisted_totals", {}).get(name, 0)
                    for name in ("ingestion_audit", "outbox_events")}
    expected_per_record_entries = phase.seed_count + args.evaluation_count
    per_record_entries_verified = reconciliation.get("valid") is True
    report.update_phase(database={
        "before_seed": database_before_seed,
        "before_evaluation": database_before_evaluation,
        "after_evaluation": database_after_evaluation,
        "postgres_before_evaluation": postgres_before_evaluation,
        "postgres_after_evaluation": postgres_after_evaluation,
        "record_cardinality": record_cardinality,
        "durable_reconciliation": reconciliation,
        "postgres_evaluation_delta": postgres_delta,
        "storage_growth_bytes": {"seeding": database_before_evaluation["database_bytes"] - database_before_seed["database_bytes"],
                                 "evaluation": database_after_evaluation["database_bytes"] - database_before_evaluation["database_bytes"]},
        "per_record_entries": {**audit_counts, "expected_exact_each": expected_per_record_entries,
                               "verified": per_record_entries_verified},
    })
    if not per_record_entries_verified:
        raise ValueError(
            "durable database reconciliation failed; see phase JSON discrepancies"
        )
    return {
        "phase": phase.name,
        "description": phase.description,
        "tenant_id": tenant_id,
        "seed": seed_result,
        "evaluation": pipeline,
        "telemetry": telemetry,
        "deployment": deployment,
        "published_scenarios": setup_result["scenarios"],
        "database": {
            "before_seed": database_before_seed,
            "before_evaluation": database_before_evaluation,
            "after_evaluation": database_after_evaluation,
            "postgres_before_evaluation": postgres_before_evaluation,
            "postgres_after_evaluation": postgres_after_evaluation,
            "record_cardinality": record_cardinality,
            "durable_reconciliation": reconciliation,
            "postgres_evaluation_delta": postgres_delta,
            "storage_growth_bytes": {"seeding": database_before_evaluation["database_bytes"] - database_before_seed["database_bytes"],
                                     "evaluation": database_after_evaluation["database_bytes"] - database_before_evaluation["database_bytes"]},
            "per_record_entries": {
                **audit_counts,
                "expected_exact_each": expected_per_record_entries,
                "verified": per_record_entries_verified,
            },
        },
    }


def _acceptance(phases: list[dict[str, Any]]) -> dict[str, Any]:
    if not phases:
        return {"passed": False, "evaluated": False, "reason": "no_completed_phases", "comparisons": []}
    baseline = next((phase for phase in phases if phase["phase"] == "empty_database"), None)
    baseline_evaluation = (baseline or phases[0])["evaluation"]
    baseline_decision = baseline_evaluation["decision"]
    baseline_ingestion = baseline_evaluation["ingestion"]
    baseline_decision_rate = baseline_decision["successful_evaluations_per_second"]
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
            decision["successful_evaluations_per_second"], baseline_decision_rate
        )
        decision_p95_ratio = _ratio(decision["latency"]["p95_ms"], baseline_decision_p95)
        ingestion_throughput_retention = _ratio(
            ingestion["requests_per_second"], baseline_ingestion_rate
        )
        ingestion_p95_ratio = _ratio(ingestion["latency"]["p95_ms"], baseline_ingestion_p95)
        reliability_failures = []
        reconciliation = evaluation.get("identity_reconciliation")
        if reconciliation is None or reconciliation.get("valid") is not True:
            reliability_failures.append("identity_reconciliation_failed")
        telemetry = phase.get("telemetry", {"enabled": False})
        telemetry_passed = not telemetry["enabled"] or telemetry.get("valid") is True
        durable = phase.get("database", {}).get("durable_reconciliation")
        if durable is None or durable.get("valid") is not True:
            reliability_failures.append("durable_reconciliation_failed")
        source = evaluation.get("evaluation_source", {})
        baseline_source = baseline_evaluation.get("evaluation_source", {})
        cohort_matches = bool(source.get("sha256")) and source.get("sha256") == baseline_source.get("sha256")
        if ingestion["failures"]:
            reliability_failures.append("ingestion_failures")
        if decision["failures"]:
            reliability_failures.append("decision_failures")
        if decision["unresolved"]:
            reliability_failures.append("unresolved_decisions")
        if ingestion["successes"] != evaluation["target_evaluations"]:
            reliability_failures.append("ingestion_target_not_met")
        if decision["successes"] != evaluation["target_evaluations"]:
            reliability_failures.append("decision_completion_target_not_met")
        performance_passed = bool(
            baseline is not None
            and cohort_matches
            and decision_throughput_retention is not None
            and decision_throughput_retention >= 0.8
            and decision_p95_ratio is not None
            and decision_p95_ratio <= 1.2
            and ingestion_throughput_retention is not None
            and ingestion_throughput_retention >= 0.8
            and ingestion_p95_ratio is not None
            and ingestion_p95_ratio <= 1.2
        )
        phase_passed = performance_passed and not reliability_failures and telemetry_passed
        passed = passed and phase_passed
        comparisons.append(
            {
                "phase": phase["phase"],
                "performance_passed": performance_passed,
                "evaluation_cohort_matches_baseline": cohort_matches,
                "telemetry_passed": telemetry_passed,
                "reliability_passed": not reliability_failures,
                "reliability_failures": reliability_failures,
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
        "evaluated": baseline is not None,
        "reason": None if baseline is not None else "empty_database_baseline_not_in_run",
        "criteria": {
            "minimum_throughput_retention": 0.8,
            "maximum_p95_latency_ratio": 1.2,
            "applies_to": ["ingestion", "decision"],
            "maximum_ingestion_failures": 0,
            "maximum_decision_failures": 0,
            "maximum_unresolved_decisions": 0,
            "throughput_basis": "successful_completions",
            "baseline_phase": "empty_database",
            "identical_evaluation_cohort_required": True,
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
    output = Path(args.output_root).expanduser().resolve() / datetime.now(timezone.utc).strftime(
        "database-scale-%Y%m%dT%H%M%S-%fZ"
    )
    output.mkdir(parents=True, exist_ok=False)
    print(f"Results: {output}")
    with RunReport(output) as report:
        return await _execute_suite(args, output, report)


async def _execute_suite(args: argparse.Namespace, output: Path, report: RunReport) -> int:
    root = Path(__file__).resolve().parents[3]
    report.stage("environment_capture")
    environment = await asyncio.to_thread(
        capture_environment, root, output,
        {"decision": args.decision_engine_url, "ingestion": args.ingestion_url, "data_model": args.data_model_url},
    )
    _write_json(output / "environment.json", environment)
    manifest = _rebase_manifest(load_manifest(args.manifest), args.data_root)
    source_manifests = [manifest]
    if args.seed_data_root:
        source_manifests.append(_rebase_manifest(load_manifest(args.manifest), args.seed_data_root))
    key = Path(args.pii_key_file).expanduser().resolve().read_bytes().strip()
    privacy = InternalPrivacyTransformer(key)
    database = DatabaseController(args, root)

    with tempfile.TemporaryDirectory(prefix="database-scale-sort-", dir=output) as sort_directory:
        report.stage("sorting")
        print("Building privacy-safe, time-sorted source chunks...")
        event_transform = (
            privacy.minimize_presanitized_event
            if args.pre_sanitized_source
            else privacy.transform_event
        )
        chunk_paths: list[Path] = []
        event_count = 0
        for index, source_manifest in enumerate(source_manifests):
            sort_result = await asyncio.to_thread(
                build_sorted_chunks,
                source_manifest,
                Path(sort_directory) / f"source-{index:02d}",
                args.sort_chunk_size,
                event_transform,
            )
            chunk_paths.extend(sort_result.chunk_paths)
            event_count += sort_result.event_count
        merged_chunk_paths = tuple(chunk_paths)
        counts = await asyncio.to_thread(_month_counts, merged_chunk_paths)
        try:
            plans = _build_phase_plans(merged_chunk_paths, event_count, counts, args)
        except ValueError as exc:
            available = ", ".join(f"{month}={count}" for month, count in sorted(counts.items()))
            raise ValueError(f"{exc}; available source months: {available or 'none'}") from exc
        _write_json(
            output / "run-config.json",
            {
                "manifest": str(manifest.path),
                "data_root_override": str(Path(args.data_root).expanduser().resolve())
                if args.data_root
                else None,
                "seed_data_root_override": str(Path(args.seed_data_root).expanduser().resolve())
                if args.seed_data_root
                else None,
                "pre_sanitized_source": args.pre_sanitized_source,
                "evaluation_cohort": args.evaluation_cohort,
                "evaluation_offset": args.evaluation_offset,
                "runs_per_phase": 1,
                "database_instance_class": {"declared": args.database_instance_class, "verified": False},
                "verification_timeout_seconds": args.verification_timeout,
                "phase": args.phase,
                "database_name": args.database_name,
                "scenario_set": SCENARIO_SET_INTERNAL,
                "scenario_definitions": [asdict(scenario) for scenario in
                                         build_portable_scenarios(manifest, SCENARIO_SET_INTERNAL)],
                "scenarios": [
                    {
                        "name": scenario.name,
                        "rules": [rule.name for rule in scenario.rules],
                    }
                    for scenario in build_portable_scenarios(manifest, SCENARIO_SET_INTERNAL)
                ],
                "evaluation_count": args.evaluation_count,
                "schema_version": 2,
                "capture_metrics": args.capture_metrics,
                "capture_database_metrics": args.capture_database_metrics,
                "metrics_interval_seconds": args.metrics_interval,
                "metrics_timeout_seconds": args.metrics_timeout,
                "deferred_policy": args.deferred_policy,
                "pipeline_timeout_seconds": args.pipeline_timeout,
                "decision_completion_timeout_seconds": args.decision_completion_timeout,
                "decision_poll_interval_seconds": args.decision_poll_interval,
                "request_timeout_seconds": args.request_timeout,
                "publication_timeout_seconds": args.publication_timeout,
                "ingestion_concurrency": args.ingestion_concurrency,
                "evaluation_concurrency": args.evaluation_concurrency,
                "seed_concurrency": args.seed_concurrency,
                "source_event_count": event_count,
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
        error_log_path = output / "errors.ndjson"
        for plan in plans:
            result = await _run_phase(plan, args, manifest, database, report, error_log_path)
            results.append(result)
            report.finish_phase(result)

    acceptance = _acceptance(results)
    report.finish(acceptance)
    print(f"\nSuite passed: {acceptance['passed']}")
    print(f"Results: {output}")
    print(f"Errors: {output / 'errors.ndjson'}")
    return 0 if acceptance["passed"] else 2


def _write_json(path: Path, value: Any) -> None:
    write_json_atomic(path, value)


def _append_text(path: Path, value: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a", encoding="utf-8") as handle:
        handle.write(value)


def main(argv: list[str] | None = None) -> None:
    try:
        raise SystemExit(asyncio.run(async_main(argv)))
    except (APIError, OSError, subprocess.CalledProcessError, ValueError, TimeoutError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(1) from exc


if __name__ == "__main__":
    main()
