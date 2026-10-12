"""Merchant-seeded, fixed-concurrency synchronous replay; no database resets."""
from __future__ import annotations

import argparse
import asyncio
from collections import Counter
from dataclasses import dataclass, field
from datetime import datetime, timezone
import hashlib
from itertools import islice
import json
import math
import os
from pathlib import Path
import sys
import tempfile
import time
from typing import Any, Iterable
from urllib.parse import urlparse
from uuid import UUID

from .api_client import APIError, ServiceClients, ServiceConfig
from .decision_completion import DecisionCompletionError, verify_decision_completion
from .domain import TransactionEvent
from .local_sample import create_full_manifest
from .local_storage import DiskSpaceGuard
from .manifest import ReplayManifest, load_manifest
from .privacy import InternalPrivacyTransformer, minimize_internal_event
from .reference_data import ReferenceRecords, load_merchants, load_merchant_products
from .replay import LatencyMetric
from .scenarios import SCENARIO_SET_INTERNAL
from .setup_environment import EnvironmentSetup
from .sorting import AsyncEventSource, build_sorted_chunks, iter_merged_events


ROOT = Path(__file__).resolve().parent.parent


def write_json(path: Path, value: Any) -> None:
    temporary = path.with_suffix(path.suffix + ".tmp")
    with temporary.open("w", encoding="utf-8") as handle:
        json.dump(value, handle, indent=2, allow_nan=False)
        handle.write("\n")
    os.replace(temporary, path)


def write_final_report(path: Path, value: Any) -> None:
    """A reporting disk error must not replace the original workload failure."""
    original_error = sys.exc_info()[0] is not None
    try:
        write_json(path, value)
    except OSError:
        if not original_error:
            raise
        print(f"Warning: could not save {path.name}; original run failure preserved.", flush=True)


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument("--execute", action="store_true", help="Allow API writes; otherwise prepare sources only")
    result.add_argument("--manifest", type=Path, default=ROOT / "production_replay/manifests/fraud-data.json")
    result.add_argument("--data-root", type=Path, help="Remap the standard six-stream production data layout")
    result.add_argument("--reference-data-root", type=Path, help="Merchant dump root; defaults to data-root")
    result.add_argument("--pii-key-file", type=Path, help="Optional opt-in HMAC key of at least 16 bytes; default leaves identifiers unchanged")
    result.add_argument("--pre-sanitized-source", action="store_true", help="Retain existing HMAC account tokens")
    result.add_argument("--tenant-id", help="Optional clean test tenant UUID; omitted creates a new tenant")
    result.add_argument("--tenant-name", default="Internal Synchronous Merchant Replay")
    result.add_argument("--data-model-url", required=True)
    result.add_argument("--ingestion-url", required=True)
    result.add_argument("--decision-engine-url", required=True)
    result.add_argument("--auth-token-env", default="SERVICE_AUTH_TOKEN")
    result.add_argument("--transactions", type=int, default=1_000_000)
    result.add_argument("--transaction-offset", type=int, default=0)
    result.add_argument("--ingestion-concurrency", type=int, default=10)
    result.add_argument("--evaluation-concurrency", type=int, default=10)
    result.add_argument("--request-timeout", type=float, default=60)
    result.add_argument("--publication-timeout", type=float, default=900)
    result.add_argument("--sort-chunk-size", type=int, default=100_000)
    result.add_argument("--progress-every", type=int, default=10_000)
    result.add_argument("--min-free-disk-mib", type=int, default=1024)
    result.add_argument("--output-root", type=Path, default=ROOT / "internal-sync-replay-runs")
    return result


def validate_arguments(args: argparse.Namespace) -> None:
    for name in ("transactions", "ingestion_concurrency", "evaluation_concurrency", "sort_chunk_size",
                 "progress_every", "min_free_disk_mib", "request_timeout", "publication_timeout"):
        value = getattr(args, name)
        if not math.isfinite(value) or value <= 0:
            raise ValueError(f"{name} must be finite and positive")
    if args.transaction_offset < 0:
        raise ValueError("transaction_offset cannot be negative")
    if args.reference_data_root is not None and args.data_root is None:
        raise ValueError("reference-data-root requires data-root")
    if args.tenant_id:
        args.tenant_id = str(UUID(args.tenant_id))
    for name in ("data_model_url", "ingestion_url", "decision_engine_url"):
        url = urlparse(getattr(args, name))
        if url.scheme not in {"http", "https"} or not url.hostname or url.username or url.password or url.query or url.fragment:
            raise ValueError(f"{name} must be an HTTP(S) service URL without credentials, query or fragment")


def prepare_sources(
    args: argparse.Namespace, work: Path, guard: DiskSpaceGuard
) -> tuple[ReplayManifest, tuple[ReferenceRecords, ReferenceRecords], Path, int]:
    manifest_path = args.manifest
    if args.data_root is not None:
        manifest_path = work / "manifest.json"
        create_full_manifest(args.manifest, args.data_root.resolve(), manifest_path,
                             args.reference_data_root.resolve() if args.reference_data_root else None)
    manifest = load_manifest(manifest_path)
    references = (load_merchants(manifest.merchant_files()), load_merchant_products(manifest.merchant_product_files()))
    if any(not source.records for source in references):
        raise ValueError("merchant and merchant product sources must both contain records")
    if args.pre_sanitized_source:
        transform = InternalPrivacyTransformer.minimize_presanitized_event
    elif args.pii_key_file is not None:
        transform = InternalPrivacyTransformer(args.pii_key_file.read_bytes()).transform_event
    else:
        transform = minimize_internal_event
    def safe_transform(event: TransactionEvent) -> TransactionEvent:
        if not math.isfinite(event.fields["amount"]):
            raise ValueError("transaction amount must be finite")
        return transform(event)
    chunks = build_sorted_chunks(manifest, work / "sort", args.sort_chunk_size,
                                 transform=safe_transform, storage_check=guard.check)
    if chunks.event_count < args.transaction_offset + args.transactions:
        raise ValueError("not enough source transactions for the requested offset and count")
    selected = work / "selected.ndjson"
    seen: set[str] = set()
    events = iter_merged_events(chunks.chunk_paths)
    try:
        with selected.open("w", encoding="utf-8") as handle:
            for sequence, event in enumerate(islice(events, args.transaction_offset,
                                                    args.transaction_offset + args.transactions)):
                if event.object_id in seen:
                    raise ValueError("duplicate selected transaction identity")
                seen.add(event.object_id)
                if sequence % 256 == 0:
                    guard.check()
                handle.write(json.dumps(event.to_sort_record(sequence), allow_nan=False) + "\n")
    finally:
        events.close()
    for path in chunks.chunk_paths:
        path.unlink()
    return manifest, references, selected, chunks.event_count


@dataclass
class Stage:
    successes: int = 0
    failures: int = 0
    retries: int = 0
    active: int = 0
    max_concurrency: int = 0
    first: float | None = None
    last: float | None = None
    latency: LatencyMetric = field(default_factory=LatencyMetric)
    errors: Counter[str] = field(default_factory=Counter)

    def begin(self) -> float:
        now = time.perf_counter()
        self.first = now if self.first is None else self.first
        self.active += 1
        self.max_concurrency = max(self.max_concurrency, self.active)
        return now

    def end(self, start: float) -> None:
        self.last = time.perf_counter()
        self.latency.add((self.last - start) * 1000)
        self.active -= 1

    def summary(self) -> dict[str, Any]:
        elapsed = self.last - self.first if self.first is not None and self.last is not None else 0
        return {"successes": self.successes, "failures": self.failures, "retries": self.retries,
                "latency": self.latency.summary(), "errors": dict(self.errors),
                "max_observed_concurrency": self.max_concurrency, "window_seconds": round(elapsed, 3),
                "successful_transactions_per_second": round(self.successes / elapsed, 2) if elapsed else None}


def validate_ingestion(response: dict[str, Any], object_id: str) -> None:
    result = response.get("result")
    if (response.get("deferred") is True or not isinstance(result, dict) or result.get("object_id") != object_id
            or result.get("action") != "created" or not result.get("revision_id")):
        raise APIError("invalid_ingestion_completion")


async def validate_decision(
    clients: ServiceClients, tenant_id: str, event: TransactionEvent, response: dict[str, Any],
    status: int, scenario_ids: tuple[str, ...],
) -> tuple[str, ...]:
    ids = await verify_decision_completion(clients, tenant_id, event.object_id, response, status,
                                         allow_deferred=False, expected_scenarios=len(scenario_ids))
    results = response["result"]["results"]
    triggered_scenarios = []
    for item in results:
        if item["triggered"]:
            decision = item["decision"]
            scenario_id = decision.get("scenario_id")
            if scenario_id not in scenario_ids or decision.get("object_type") != "transactions":
                raise DecisionCompletionError("invalid_decision_identity")
            triggered_scenarios.append(scenario_id)
    if len(set(triggered_scenarios)) != len(triggered_scenarios):
        raise DecisionCompletionError("duplicate_scenario_identity")
    return ids


class FixedSyncReplay:
    """Bound both HTTP stages and the handoff queue; never retry decision writes."""

    def __init__(
        self, clients: ServiceClients, tenant_id: str, scenario_ids: tuple[str, ...],
        args: argparse.Namespace, output: Path,
    ) -> None:
        self.clients, self.tenant_id, self.scenario_ids, self.args, self.output = clients, tenant_id, scenario_ids, args, output
        self.ingestion, self.decision = Stage(), Stage()
        self.selected = 0
        self.finished = 0
        self.decision_rows = 0
        self.started: float | None = None
        self.ended: float | None = None
        self.end_to_end = LatencyMetric()
        self.guard = DiskSpaceGuard(output, args.min_free_disk_mib * 1024 ** 2)

    def summary(self) -> dict[str, Any]:
        elapsed = (self.ended or time.perf_counter()) - self.started if self.started is not None else 0
        return {"selected": self.selected, "finished": self.finished,
                "requested_transactions": self.args.transactions,
                "ingestion": self.ingestion.summary(), "decision": self.decision.summary(),
                "decision_rows_reported": self.decision_rows,
                "end_to_end_latency": self.end_to_end.summary(), "elapsed_seconds": round(elapsed, 3),
                "end_to_end_successful_transactions_per_second": round(self.decision.successes / elapsed, 2) if elapsed else None}

    def failed(self, stage: Stage, label: str, event: TransactionEvent, error: APIError) -> None:
        category = error.category if isinstance(error, DecisionCompletionError) else (
            f"http_{error.status_code}" if error.status_code else "api_or_completion_error")
        stage.failures += 1
        stage.errors[category] += 1
        # Do not persist API error text or bodies: either can contain PII or credentials.
        with (self.output / "errors.ndjson").open("a", encoding="utf-8") as handle:
            handle.write(json.dumps({"time": datetime.now(timezone.utc).isoformat(), "stage": label,
                                     "object_id": event.object_id, "category": category,
                                     "http_status": error.status_code, "attempts": error.attempts}) + "\n")

    def finish(self, started: float) -> None:
        self.finished += 1
        self.end_to_end.add((time.perf_counter() - started) * 1000)
        if self.finished % self.args.progress_every == 0:
            self.guard.check()
            write_json(self.output / "progress.json", self.summary())
            print(f"Finished {self.finished:,}/{self.args.transactions:,}; "
                  f"sync evaluations {self.decision.successes:,}; "
                  f"failures {self.ingestion.failures + self.decision.failures:,}", flush=True)

    async def run(self, events: Iterable[TransactionEvent]) -> dict[str, Any]:
        queue: asyncio.Queue[tuple[TransactionEvent, float] | None] = asyncio.Queue(
            maxsize=max(self.args.ingestion_concurrency, self.args.evaluation_concurrency * 2))
        source = AsyncEventSource(events)
        claim = asyncio.Lock()
        self.started = time.perf_counter()

        async def ingest() -> None:
            while True:
                async with claim:
                    event = await source.next()
                    if event is None:
                        return
                    self.selected += 1
                started = time.perf_counter()
                request_start = self.ingestion.begin()
                failed = False
                try:
                    digest = hashlib.sha256(f"{self.tenant_id}:{event.object_id}".encode()).hexdigest()
                    response, attempts = await self.clients.ingest_one(
                        self.tenant_id, "transactions", event.fields, f"internal-sync-replay:{digest}")
                    self.ingestion.retries += attempts - 1
                    validate_ingestion(response, event.object_id)
                    self.ingestion.successes += 1
                except APIError as error:
                    self.ingestion.retries += error.attempts - 1
                    self.failed(self.ingestion, "ingestion", event, error)
                    failed = True
                finally:
                    self.ingestion.end(request_start)
                if failed:
                    self.finish(started)
                else:
                    await queue.put((event, started))

        async def evaluate() -> None:
            while True:
                item = await queue.get()
                if item is None:
                    return
                event, started = item
                request_start = self.decision.begin()
                try:
                    response, status, _ = await self.clients.record_ingested(
                        self.tenant_id, event.object_id, event.fields, mode="sync", source="internal_sync_replay")
                    ids = await validate_decision(self.clients, self.tenant_id, event, response, status, self.scenario_ids)
                    self.decision.successes += 1
                    self.decision_rows += len(ids)
                except APIError as error:
                    self.failed(self.decision, "decision", event, error)
                finally:
                    self.decision.end(request_start)
                self.finish(started)

        async def produce() -> None:
            workers = [asyncio.create_task(ingest()) for _ in range(self.args.ingestion_concurrency)]
            try:
                await asyncio.gather(*workers)
                for _ in range(self.args.evaluation_concurrency):
                    await queue.put(None)
            finally:
                for worker in workers:
                    worker.cancel()
                await asyncio.gather(*workers, return_exceptions=True)

        tasks = [asyncio.create_task(produce()),
                 *(asyncio.create_task(evaluate()) for _ in range(self.args.evaluation_concurrency))]
        try:
            await asyncio.gather(*tasks)
            if self.selected != self.args.transactions or self.finished != self.selected:
                raise RuntimeError("incomplete_selected_transaction_run")
        finally:
            for task in tasks:
                task.cancel()
            await asyncio.gather(*tasks, return_exceptions=True)
            await source.close()
            self.ended = time.perf_counter()
            write_final_report(self.output / "progress.json", self.summary())
        return self.summary()


async def execute(args: argparse.Namespace) -> int:
    validate_arguments(args)
    stamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S-%fZ")
    output = args.output_root.resolve() / f"internal-sync-replay-{stamp}"
    output.mkdir(parents=True, exist_ok=False)
    (output / "errors.ndjson").touch(exist_ok=False)
    print("Results:", output, flush=True)
    report: dict[str, Any] = {"status": "preparing", "scenario_set": "internal", "decision_mode": "sync",
                              "transactions": args.transactions, "transaction_offset": args.transaction_offset,
                              "ingestion_concurrency": args.ingestion_concurrency,
                              "evaluation_concurrency": args.evaluation_concurrency,
                              "account_identifiers": "hmac_sha256" if args.pii_key_file is not None and not args.pre_sanitized_source else "unchanged",
                              "database_recreated": False, "mutations_performed": False,
                              "limitations": ["API completion evidence, not PostgreSQL reconciliation",
                                              "No server telemetry or deployment image verification",
                                              "Untriggered result entries do not expose scenario IDs in the API",
                                              "Reference dumps and source directories must remain unchanged during preparation"]}
    write_json(output / "run-config.json", {**report, "endpoints": {
        "data_model": args.data_model_url, "ingestion": args.ingestion_url, "decision": args.decision_engine_url}})
    replay = None
    environment = None
    try:
        with tempfile.TemporaryDirectory(prefix="sources-", dir=output) as directory:
            work = Path(directory)
            guard = DiskSpaceGuard(work, args.min_free_disk_mib * 1024 ** 2)
            guard.check(force=True)
            print("Preparing merchant dumps and ordered, privacy-minimized transactions...", flush=True)
            manifest, references, selected, source_count = await asyncio.to_thread(prepare_sources, args, work, guard)
            report["source_transaction_count"] = source_count
            if not args.execute:
                report["status"] = "prepared_only"
                return 0
            config = ServiceConfig(args.data_model_url, args.ingestion_url, args.decision_engine_url,
                                   os.getenv(args.auth_token_env), args.request_timeout,
                                   max(args.ingestion_concurrency, args.evaluation_concurrency))
            async with ServiceClients(config) as clients:
                await clients.wait_until_ready()
                if args.tenant_id:
                    existing = await clients.request(clients.decision_engine, "GET",
                                                     f"/v1/tenants/{args.tenant_id}/scenarios", 200)
                    if not isinstance(existing.get("scenarios"), list) or existing["scenarios"]:
                        raise ValueError("existing tenant must have no scenarios; use a dedicated clean test tenant")
                report["status"] = "setup"
                report["mutations_performed"] = True
                print("Preparing tables, seeding merchants/products, and publishing internal scenarios...", flush=True)
                environment = EnvironmentSetup(manifest, clients, args.tenant_id, args.tenant_name,
                                               SCENARIO_SET_INTERNAL, merchant_references=references)
                setup = await environment.run(args.publication_timeout)
                report["tenant_id"] = setup["tenant_id"]
                write_json(output / "setup.json", setup)
                print("Tenant:", setup["tenant_id"], flush=True)
                report["status"] = "evaluating"
                replay = FixedSyncReplay(clients, setup["tenant_id"],
                                         tuple(value["scenario_id"] for value in setup["scenarios"].values()), args, output)
                report["pipeline"] = await replay.run(iter_merged_events([selected]))
                report["status"] = "completed_with_errors" if replay.ingestion.failures or replay.decision.failures else "completed"
                return 2 if report["status"] == "completed_with_errors" else 0
    except BaseException as error:
        report["failed_stage"] = report["status"]
        report["status"] = "interrupted" if isinstance(error, (KeyboardInterrupt, asyncio.CancelledError)) else "failed"
        report["failure_type"] = type(error).__name__
        if isinstance(error, APIError):
            report["failure_http_status"] = error.status_code
        raise
    finally:
        if environment is not None and environment.tenant_id:
            report["tenant_id"] = environment.tenant_id
        if replay is not None:
            report["pipeline"] = replay.summary()
        report["completed_at"] = datetime.now(timezone.utc).isoformat()
        write_final_report(output / "summary.json", report)
        print("Status:", report["status"], flush=True)


def main() -> None:
    command_parser = parser()
    args = command_parser.parse_args()
    try:
        validate_arguments(args)
    except ValueError as error:
        command_parser.error(str(error))
    try:
        code = asyncio.run(execute(args))
    except KeyboardInterrupt:
        code = 130
    except (APIError, ValueError, OSError, RuntimeError):
        # Detailed service error bodies can expose source PII; use safe run artifacts.
        print("Run failed; see summary.json for the failing stage and failure type.")
        code = 1
    raise SystemExit(code)
