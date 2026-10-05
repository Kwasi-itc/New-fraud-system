"""Read-only generator and service observation, usable without a database reset."""
from __future__ import annotations

import argparse
import asyncio
import hashlib
import json
import math
import os
import platform
import subprocess
import time
from collections.abc import Awaitable, Callable
from datetime import datetime, timezone
from importlib.metadata import version
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

import httpx
import psutil

from .benchmark_reporting import write_json_atomic
from .postgres_observation import counter_deltas


# Only explicit runtime controls may be persisted. Never copy the full environment.
RUNTIME_SETTINGS = frozenset({
    "LIVE_DECISION_MODE", "LIVE_DECISION_CONCURRENCY_LIMIT", "LIVE_ASYNC_FALLBACK_ENABLED",
    "TENANT_DATA_READ_MODE", "HTTP_CLIENT_TIMEOUT", "RULE_EVALUATION_CONCURRENCY",
    "SCENARIO_EVALUATION_CONCURRENCY", "AGGREGATE_PUSHDOWN_MODE", "AGGREGATE_REMOTE_CONCURRENCY_LIMIT",
    "WRITE_PATH_CONCURRENCY_LIMIT", "WRITE_PATH_OVERLOAD_MODE", "GIN_MODE", "LOG_LEVEL",
    "WORKER_MODE", "WORKER_POLL_INTERVAL", "WORKER_BATCH_LIMIT",
    "DB_MAX_CONNS", "DB_MIN_CONNS", "DATABASE_MAX_CONNS", "DATABASE_MIN_CONNS",
    "READ_DATABASE_MAX_CONNS", "READ_DATABASE_MIN_CONNS", "WORKER_DATABASE_MAX_CONNS", "WORKER_DATABASE_MIN_CONNS",
})


def project_container(item: dict[str, Any]) -> dict[str, Any]:
    config, host, state = item.get("Config", {}), item.get("HostConfig", {}), item.get("State", {})
    env = dict(value.split("=", 1) for value in config.get("Env", []) if "=" in value)
    targets = {}
    for name in ("DATABASE_URL", "READ_DATABASE_URL", "WORKER_DATABASE_URL"):
        if env.get(name):
            parsed = urlsplit(env[name])
            targets[name] = {"host": parsed.hostname, "port": parsed.port or 5432,
                             "database": parsed.path.lstrip("/")}
    return {"name": item.get("Name", "").lstrip("/"),
            "service": config.get("Labels", {}).get("com.docker.compose.service"),
            "image_reference": config.get("Image"),
            "image_id": item.get("Image"), "started_at": state.get("StartedAt"),
            "restart_count": item.get("RestartCount"), "status": state.get("Status"),
            "limits": {key: host.get(key) for key in ("Memory", "NanoCpus", "CpuQuota", "CpuPeriod", "CpusetCpus")},
            "runtime_settings": {key: env[key] for key in sorted(RUNTIME_SETTINGS) if key in env},
            "database_targets": targets,
            "networks": sorted(item.get("NetworkSettings", {}).get("Networks", {}))}


def capture_deployment(compose: list[str], env: dict[str, str], services: tuple[str, ...],
                       expected_database: str, expected_host: str, expected_port: int) -> dict[str, Any]:
    try:
        ids = subprocess.run([*compose, "ps", "--all", "-q", *services], env=env,
                             capture_output=True, text=True, check=True, timeout=15).stdout.split()
        if not ids:
            return {"status": "error", "error_type": "NoContainers"}
        raw = subprocess.run(["docker", "inspect", *ids], env=env, capture_output=True,
                             text=True, check=True, timeout=15)
        containers = [project_container(item) for item in json.loads(raw.stdout)]
        missing = sorted(set(services) - {item["service"] for item in containers})
        running = all(item["status"] == "running" for item in containers)
        correct_targets = all(item["database_targets"].get("DATABASE_URL") and
                              all(target == {"database": expected_database, "host": expected_host, "port": expected_port}
                                  for target in item["database_targets"].values()) for item in containers)
        return {"status": "ok" if correct_targets and running and not missing else "error", "containers": containers,
                "database_names_match": correct_targets,
                "all_running": running, "missing_services": missing,
                "limitations": ["image_id_is_local_content_id", "settings_show_explicit_env_only",
                                "pool_limits_observed_via_service_metrics", "remote_database_hardware_not_observed"]}
    except (OSError, subprocess.SubprocessError, ValueError, TypeError) as exc:
        return {"status": "error", "error_type": type(exc).__name__}


def _now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def endpoint_identity(url: str) -> str:
    parsed = urlsplit(url)
    host = parsed.hostname or ""
    if ":" in host:
        host = f"[{host}]"
    port = f":{parsed.port}" if parsed.port is not None else ""
    # Neither credentials, path segments nor query values belong in environment reports.
    return f"{parsed.scheme}://{host}{port}"


def _git(root: Path, *args: str) -> dict[str, Any]:
    try:
        value = subprocess.run(["git", "-C", str(root), *args], capture_output=True,
                               text=True, check=True, timeout=10).stdout.strip()
        return {"available": True, "value": value}
    except (OSError, subprocess.SubprocessError) as exc:
        return {"available": False, "error_type": type(exc).__name__}


def capture_environment(root: Path, output: Path, endpoints: dict[str, str]) -> dict[str, Any]:
    dirty = _git(root, "status", "--porcelain", "--untracked-files=normal")
    if dirty["available"]:
        value = dirty.pop("value")
        dirty.update(dirty=bool(value), changed_entries=len(value.splitlines()))
    sources = {}
    for path in sorted(Path(__file__).parent.glob("*.py")):
        sources[path.name] = hashlib.sha256(path.read_bytes()).hexdigest()
    return {
        "schema_version": 1,
        "captured_at": _now(),
        "scope": "observer_host_not_remote_service_or_database_hardware",
        "platform": {"system": platform.system(), "release": platform.release(),
                     "machine": platform.machine(), "python": platform.python_version()},
        "logical_cpus": psutil.cpu_count(),
        "physical_cores": psutil.cpu_count(logical=False),
        "memory_total_bytes": psutil.virtual_memory().total,
        "output_filesystem": psutil.disk_usage(str(output))._asdict(),
        "packages": {name: version(name) for name in ("httpx", "openpyxl", "psutil")},
        "git_revision": _git(root, "rev-parse", "HEAD"),
        "working_tree": dirty,
        "harness_sha256": sources,
        "endpoints": {name: endpoint_identity(url) for name, url in endpoints.items()},
        "not_captured": ["remote_hardware", "effective_container_limits", "deployed_image_digests",
                         "effective_service_configuration", "queue_registry", "database_configuration"],
    }


def sample_local(output: Path) -> dict[str, Any]:
    process = psutil.Process()
    measurements: dict[str, Any] = {}
    sources: dict[str, Callable[[], Any]] = {
        "cpu_seconds": psutil.cpu_times,
        "memory_bytes": psutil.virtual_memory,
        "disk_io_cumulative": lambda: psutil.disk_io_counters(nowrap=False),
        "network_io_cumulative": lambda: psutil.net_io_counters(nowrap=False),
        "output_filesystem": lambda: psutil.disk_usage(str(output)),
        "process_cpu_seconds": process.cpu_times,
        "process_memory_bytes": process.memory_info,
    }
    errors = {}
    for name, sample in sources.items():
        try:
            value = sample()
            if value is None:
                errors[name] = "unavailable"
            else:
                measurements[name] = value._asdict()
        except (OSError, psutil.Error, NotImplementedError) as exc:
            errors[name] = type(exc).__name__
    boot_time = None
    try:
        boot_time = psutil.boot_time()
    except (OSError, psutil.Error, NotImplementedError) as exc:
        errors["boot_time"] = type(exc).__name__
    return {"status": "partial" if errors else "ok", "measurements": measurements, "errors": errors,
            "scope": "observer_host_and_process", "boot_time_epoch_seconds": boot_time}


def _numeric_fields(value: Any) -> dict[str, int | float]:
    if not isinstance(value, dict):
        return {}
    return {key: item for key, item in value.items()
            if type(item) in (int, float) and math.isfinite(item)}


def project_metrics(payload: Any, envelope: str) -> dict[str, Any]:
    if not isinstance(payload, dict) or not isinstance(payload.get(envelope), dict):
        raise ValueError("invalid metrics envelope")
    body = payload[envelope]
    projected = {}
    for group in ("db_pool", "pressure", "evaluation", "aggregate_pushdown", "broad_read_helpers", "tenant_data_reads"):
        if group in body:
            projected[group] = _numeric_fields(body[group])
    # Dynamic endpoint labels are hashed; arbitrary labels and string values are never copied.
    endpoints = body.get("endpoints")
    if isinstance(endpoints, dict):
        projected["endpoints"] = {
            hashlib.sha256(str(name).encode()).hexdigest(): _numeric_fields(value)
            for name, value in endpoints.items()
        }
    if not any(projected.values()):
        raise ValueError("no recognized metrics groups")
    return projected


class RuntimeObserver:
    def __init__(self, output: Path, endpoints: dict[str, str], *, auth_token: str | None = None,
                 interval: float = 5.0, timeout: float = 5.0, transport: Any = None,
                 database: Callable[[], Awaitable[dict[str, Any]]] | None = None) -> None:
        if any(not math.isfinite(v) or v <= 0 for v in (interval, timeout)):
            raise ValueError("observation interval and timeout must be finite and positive")
        self.output = output
        self.endpoints = endpoints
        self.interval = interval
        self.timeout = timeout
        self.database = database
        self.client = httpx.AsyncClient(
            headers={"Authorization": f"Bearer {auth_token}"} if auth_token else {},
            timeout=timeout, limits=httpx.Limits(max_connections=2), transport=transport,
        )
        self.samples = 0
        self.failures = 0
        self.started = time.monotonic()
        self.file: Any = None
        self.previous_sample: dict[str, Any] | None = None

    async def __aenter__(self) -> RuntimeObserver:
        self.output.parent.mkdir(parents=True, exist_ok=True)
        try:
            self.file = self.output.open("x", encoding="utf-8")
        except BaseException:
            await self.client.aclose()
            raise
        return self

    async def __aexit__(self, *_args: Any) -> None:
        try:
            self.file.close()
        finally:
            await self.client.aclose()

    async def _remote(self, name: str, base: str) -> dict[str, Any]:
        envelope, route = {
            "decision": ("runtime_metrics", "/v1/admin/runtime-metrics"),
            "ingestion": ("read_metrics", "/v1/admin/read-metrics"),
        }[name]
        started = time.monotonic()
        try:
            async def fetch() -> Any:
                async with self.client.stream("GET", base.rstrip("/") + route) as response:
                    response.raise_for_status()
                    data = bytearray()
                    async for part in response.aiter_bytes():
                        data.extend(part)
                        if len(data) > 1_048_576:
                            raise ValueError("metrics response exceeds limit")
                    return project_metrics(json.loads(data), envelope)

            metrics = await asyncio.wait_for(fetch(), timeout=self.timeout)
            return {"status": "ok", "metrics": metrics,
                    "observation_request_ms": round((time.monotonic() - started) * 1000, 3)}
        except (httpx.HTTPError, ValueError, TimeoutError) as exc:
            failure: dict[str, Any] = {"status": "error", "error_type": type(exc).__name__}
            if isinstance(exc, httpx.HTTPStatusError):
                failure["http_status"] = exc.response.status_code
            return failure

    async def sample(self) -> None:
        captured_at = _now()
        started = time.monotonic()
        local = await asyncio.to_thread(sample_local, self.output.parent)
        names = list(self.endpoints)
        responses = await asyncio.gather(*(self._remote(name, self.endpoints[name]) for name in names))
        failed = local["status"] != "ok" or any(value["status"] != "ok" for value in responses)
        database = await self.database() if self.database is not None else None
        failed = failed or (database is not None and database["status"] != "ok")
        sample = {"schema_version": 1, "captured_at": captured_at,
                  "elapsed_seconds": started - self.started, "sample_duration_seconds": time.monotonic() - started,
                  "local": local, "services": dict(zip(names, responses)),
                  "complete": not failed}
        if database is not None:
            sample["database"] = database
        previous = self.previous_sample
        if previous is not None:
            elapsed = sample["elapsed_seconds"] - previous["elapsed_seconds"]
            if elapsed > 0 and local.get("boot_time_epoch_seconds") is not None and local.get("boot_time_epoch_seconds") == previous["local"].get("boot_time_epoch_seconds"):
                rates = {}
                for group in ("cpu_seconds", "process_cpu_seconds", "disk_io_cumulative", "network_io_cumulative"):
                    now_values = local.get("measurements", {}).get(group, {})
                    old_values = previous["local"].get("measurements", {}).get(group, {})
                    deltas = {key: value - old_values[key] for key, value in now_values.items()
                              if key in old_values and isinstance(value, (int, float))}
                    rates[group] = ({key: value / elapsed for key, value in deltas.items()}
                                    if deltas and all(value >= 0 for value in deltas.values()) else None)
                sample["local_counter_rates"] = rates
                cpu = rates.get("cpu_seconds")
                if cpu and sum(cpu.values()) > 0:
                    sample["host_cpu_utilization_pct"] = 100 * (1 - (cpu.get("idle", 0) + cpu.get("iowait", 0)) / sum(cpu.values()))
            if database is not None and previous.get("database") is not None:
                sample["postgres_interval_delta"] = counter_deltas(previous["database"], database)
                failed = failed or not sample["postgres_interval_delta"]["valid"]
                sample["complete"] = not failed
        self.previous_sample = sample
        self.file.write(json.dumps(sample, allow_nan=False) + "\n")
        self.file.flush()
        self.samples += 1
        self.failures += int(failed)

    def summary(self) -> dict[str, Any]:
        return {"enabled": True, "samples": self.samples, "incomplete_samples": self.failures,
                "valid": self.samples > 0 and self.failures == 0, "artifact": self.output.name,
                "interval_seconds": self.interval, "timeout_seconds": self.timeout,
                "coverage": ["observer_host", "observer_process", *self.endpoints,
                             *(["postgresql", "public_river_active_jobs"] if self.database else [])],
                "limitations": ["numeric_projection", "service_percentiles_are_endpoint_window_samples",
                                "no_remote_host_collection", "partial_queue_coverage",
                                "counter_rates_require_consecutive_valid_samples"]}

    async def run_during(self, workload: Callable[[], Awaitable[Any]]) -> Any:
        await self.sample()
        stopped = asyncio.Event()

        async def run() -> Any:
            try:
                return await workload()
            finally:
                stopped.set()

        async def observe() -> None:
            while not stopped.is_set():
                try:
                    await asyncio.wait_for(stopped.wait(), timeout=self.interval)
                except TimeoutError:
                    await self.sample()

        work = asyncio.create_task(run())
        observer = asyncio.create_task(observe())
        try:
            await asyncio.gather(work, observer)
            await self.sample()
            return work.result()
        finally:
            for task in (work, observer):
                task.cancel()
            await asyncio.gather(work, observer, return_exceptions=True)


async def async_main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Read-only benchmark observation; never resets databases or services")
    parser.add_argument("--output", required=True, help="New output directory")
    parser.add_argument("--samples", type=int, default=3)
    parser.add_argument("--interval", type=float, default=5.0)
    parser.add_argument("--timeout", type=float, default=5.0)
    parser.add_argument("--decision-engine-url")
    parser.add_argument("--ingestion-url")
    parser.add_argument("--auth-token-env", default="SERVICE_AUTH_TOKEN")
    args = parser.parse_args(argv)
    if args.samples <= 0:
        parser.error("--samples must be positive")
    if any(not math.isfinite(v) or v <= 0 for v in (args.interval, args.timeout)):
        parser.error("--interval and --timeout must be finite and positive")
    output = Path(args.output).resolve()
    output.mkdir(parents=True, exist_ok=False)
    endpoints = {name: value for name, value in
                 (("decision", args.decision_engine_url), ("ingestion", args.ingestion_url)) if value}
    write_json_atomic(output / "environment.json", capture_environment(Path(__file__).resolve().parents[3], output, endpoints))
    async with RuntimeObserver(output / "observations.ndjson", endpoints,
                               auth_token=os.getenv(args.auth_token_env), interval=args.interval, timeout=args.timeout) as observer:
        completed = False
        try:
            for index in range(args.samples):
                await observer.sample()
                if index + 1 < args.samples:
                    await asyncio.sleep(args.interval)
            completed = True
        finally:
            summary = observer.summary()
            summary.update(status="completed" if completed else "incomplete", expected_samples=args.samples)
            summary["valid"] = summary["valid"] and completed and observer.samples == args.samples
            write_json_atomic(output / "observation-summary.json", summary)
        return 0 if observer.summary()["valid"] else 2


if __name__ == "__main__":
    raise SystemExit(asyncio.run(async_main()))
