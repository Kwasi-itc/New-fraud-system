from __future__ import annotations

import argparse
import asyncio
import json
import os
import platform
import statistics
import subprocess
import sys
import time
import uuid
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import httpx


def unique_name(prefix: str) -> str:
    return f"{prefix}_{uuid.uuid4().hex[:12]}"


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def true_node() -> dict[str, Any]:
    return {"function": "eq", "children": [{"constant": 1}, {"constant": 1}]}


def amount_gt_node(limit: int) -> dict[str, Any]:
    return {
        "function": "gt",
        "children": [
            {"function": "field_ref", "named_children": {"field": {"constant": "amount"}}},
            {"constant": limit},
        ],
    }


def record_payload(object_id: str, amount: int) -> dict[str, Any]:
    suffix = object_id.replace("-", "_")
    return {
        "object_id": object_id,
        "amount": amount,
        "status": "pending",
        "account_id": f"acct_{suffix}",
        "ip": "1.2.3.4",
        "merchant": "ITC Market",
        "email": f"risk_{suffix}@example.com",
        "country": "gh",
        "owner_id": f"owner_{suffix}",
        "event_time": utc_now(),
        "note": None,
    }


def percentile(values: list[float], pct: float) -> float | None:
    if not values:
        return None
    ordered = sorted(values)
    position = (pct / 100.0) * (len(ordered) - 1)
    lower = int(position)
    upper = min(lower + 1, len(ordered) - 1)
    if lower == upper:
        return ordered[lower]
    weight = position - lower
    return ordered[lower] + (ordered[upper] - ordered[lower]) * weight


def format_optional(value: float | None) -> str:
    if value is None:
        return "n/a"
    return f"{value:.2f}"


@dataclass(frozen=True)
class Config:
    rate: int
    vus: int
    duration_seconds: float
    warmup_duration_seconds: float
    amount: int
    timeout_seconds: float
    output: str
    data_model_url: str
    ingestion_url: str
    decision_engine_url: str
    auth_token: str | None
    scenario_threshold: int


@dataclass
class Metrics:
    attempted: int = 0
    completed: int = 0
    successes: int = 0
    failures: int = 0
    timeouts: int = 0
    dropped_requests: int = 0
    latencies_ms: list[float] | None = None
    errors: list[str] | None = None

    def __post_init__(self) -> None:
        if self.latencies_ms is None:
            self.latencies_ms = []
        if self.errors is None:
            self.errors = []


class ThroughputHarness:
    def __init__(self, config: Config) -> None:
        headers: dict[str, str] = {}
        if config.auth_token:
            headers["Authorization"] = f"Bearer {config.auth_token}"
        timeout = httpx.Timeout(config.timeout_seconds)
        limits = httpx.Limits(max_connections=max(config.vus * 2, 100), max_keepalive_connections=max(config.vus, 20))
        self.config = config
        self.data_model = httpx.AsyncClient(base_url=config.data_model_url, timeout=timeout, headers=headers, limits=limits)
        self.ingestion = httpx.AsyncClient(base_url=config.ingestion_url, timeout=timeout, headers=headers, limits=limits)
        self.decision_engine = httpx.AsyncClient(base_url=config.decision_engine_url, timeout=timeout, headers=headers, limits=limits)
        self.tenant_id = ""
        self.object_type = ""
        self.scenario_id = ""
        self.rule_id = ""
        self._sequence = 0

    async def close(self) -> None:
        await self.data_model.aclose()
        await self.ingestion.aclose()
        await self.decision_engine.aclose()

    async def wait_until_ready(self, client: httpx.AsyncClient, service_name: str) -> None:
        deadline = time.monotonic() + 30.0
        last_error: Exception | None = None
        while time.monotonic() < deadline:
            try:
                response = await client.get("/readyz")
                if response.status_code == 200:
                    return
                last_error = RuntimeError(f"status {response.status_code}")
            except (httpx.HTTPError, OSError) as exc:
                last_error = exc
            await asyncio.sleep(0.5)
        detail = f": {last_error}" if last_error else ""
        raise RuntimeError(f"{service_name} was not ready{detail}")

    async def request(
        self,
        client: httpx.AsyncClient,
        method: str,
        path: str,
        expected: int | set[int] | tuple[int, ...],
        **kwargs: Any,
    ) -> dict[str, Any]:
        response = await client.request(method, path, **kwargs)
        expected_set = {expected} if isinstance(expected, int) else set(expected)
        if response.status_code not in expected_set:
            raise RuntimeError(
                f"{method} {response.request.url} returned {response.status_code}, "
                f"expected {sorted(expected_set)}: {response.text}"
            )
        if not response.content:
            return {}
        return response.json()

    async def bootstrap(self) -> None:
        await self.wait_until_ready(self.data_model, "data-model")
        await self.wait_until_ready(self.ingestion, "ingestion")
        await self.wait_until_ready(self.decision_engine, "decision-engine")

        tenant = (
            await self.request(
                self.data_model,
                "POST",
                "/v1/tenants",
                201,
                json={"name": unique_name("throughput_tenant"), "external_key": unique_name("throughput_ext")},
            )
        )["tenant"]
        self.tenant_id = tenant["id"]
        await self.request(self.data_model, "POST", f"/v1/tenants/{self.tenant_id}/provision", 200)

        transactions = (
            await self.request(
                self.data_model,
                "POST",
                f"/v1/tenants/{self.tenant_id}/tables",
                201,
                json={
                    "name": unique_name("transactions"),
                    "description": "Throughput stress transaction table",
                    "alias": "Transactions",
                    "semantic_type": "entity",
                },
            )
        )["table"]
        accounts = (
            await self.request(
                self.data_model,
                "POST",
                f"/v1/tenants/{self.tenant_id}/tables",
                201,
                json={
                    "name": unique_name("accounts"),
                    "description": "Throughput stress account table",
                    "alias": "Accounts",
                    "semantic_type": "entity",
                },
            )
        )["table"]
        self.object_type = transactions["name"]

        fields: dict[str, dict[str, Any]] = {}
        for field in [
            {"name": "amount", "data_type": "int", "nullable": False},
            {
                "name": "status",
                "data_type": "string",
                "nullable": False,
                "is_enum": True,
                "enum_values": [{"value": "pending", "label": "Pending", "sort_order": 10}],
            },
            {"name": "account_id", "data_type": "string", "nullable": True},
            {"name": "ip", "data_type": "ip_address", "nullable": True},
            {"name": "merchant", "data_type": "string", "nullable": False},
            {"name": "email", "data_type": "string", "nullable": False},
            {"name": "country", "data_type": "string", "nullable": False},
            {"name": "owner_id", "data_type": "string", "nullable": True},
            {"name": "event_time", "data_type": "timestamp", "nullable": True},
            {"name": "note", "data_type": "string", "nullable": True},
        ]:
            created = (
                await self.request(
                    self.data_model,
                    "POST",
                    f"/v1/tables/{transactions['id']}/fields",
                    201,
                    json=field,
                )
            )["field"]
            fields[created["name"]] = created

        account_key = (
            await self.request(
                self.data_model,
                "POST",
                f"/v1/tables/{accounts['id']}/fields",
                201,
                json={"name": "account_key", "data_type": "string", "nullable": False, "is_unique": True},
            )
        )["field"]
        await self.request(
            self.data_model,
            "POST",
            f"/v1/tables/{accounts['id']}/fields",
            201,
            json={"name": "account_status", "data_type": "string", "nullable": False},
        )
        await self.request(
            self.data_model,
            "POST",
            f"/v1/tables/{accounts['id']}/fields",
            201,
            json={"name": "owner_id", "data_type": "string", "nullable": True},
        )
        await self.request(
            self.data_model,
            "POST",
            f"/v1/tenants/{self.tenant_id}/links",
            201,
            json={
                "name": "account",
                "parent_table_id": accounts["id"],
                "parent_field_id": account_key["id"],
                "child_table_id": transactions["id"],
                "child_field_id": fields["account_id"]["id"],
            },
        )

        scenario = (
            await self.request(
                self.decision_engine,
                "POST",
                f"/v1/tenants/{self.tenant_id}/scenarios",
                201,
                json={"name": unique_name("throughput_scenario"), "trigger_object_type": self.object_type},
            )
        )["scenario"]
        self.scenario_id = scenario["id"]
        iteration = (
            await self.request(
                self.decision_engine,
                "POST",
                f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_id}/iterations",
                201,
            )
        )["iteration"]
        await self.request(
            self.decision_engine,
            "PUT",
            f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_id}/iterations/{iteration['id']}",
            200,
            json={
                "trigger_formula": true_node(),
                "score_review_threshold": 10,
                "score_block_and_review_threshold": 30,
                "score_decline_threshold": 60,
                "schedule": "",
            },
        )
        rule = (
            await self.request(
                self.decision_engine,
                "POST",
                f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_id}/iterations/{iteration['id']}/rules",
                201,
                json={
                    "display_order": 1,
                    "name": "Amount over limit",
                    "description": "Throughput baseline rule",
                    "formula": amount_gt_node(self.config.scenario_threshold),
                    "score_modifier": 25,
                    "rule_group": "throughput",
                    "stable_rule_id": unique_name("throughput_rule"),
                },
            )
        )["rule"]
        self.rule_id = rule["id"]
        validation = await self.request(
            self.decision_engine,
            "POST",
            f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_id}/iterations/{iteration['id']}/validate",
            200,
        )
        if validation.get("validation", {}).get("valid") is not True:
            raise RuntimeError(f"iteration validation failed: {json.dumps(validation, default=str)}")
        await self.request(
            self.decision_engine,
            "POST",
            f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_id}/iterations/{iteration['id']}/commit",
            200,
        )
        await self.request(
            self.decision_engine,
            "POST",
            f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_id}/publications",
            200,
            json={"action": "publish", "iteration_id": iteration["id"]},
        )

    def next_object_id(self) -> str:
        self._sequence += 1
        return f"throughput_{self._sequence}_{uuid.uuid4().hex[:8]}"

    async def evaluate_once(self) -> None:
        object_id = self.next_object_id()
        payload = record_payload(object_id=object_id, amount=self.config.amount)
        await self.request(
            self.decision_engine,
            "POST",
            f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_id}/evaluate",
            200,
            json={"object_id": object_id, "object_type": self.object_type, "fields": payload},
        )


async def run_one_request(harness: ThroughputHarness, metrics: Metrics, record_metrics: bool) -> None:
    started_at = time.perf_counter()
    try:
        await harness.evaluate_once()
    except httpx.TimeoutException as exc:
        if record_metrics:
            metrics.completed += 1
            metrics.failures += 1
            metrics.timeouts += 1
            add_error(metrics, f"timeout: {exc}")
    except Exception as exc:
        if record_metrics:
            metrics.completed += 1
            metrics.failures += 1
            add_error(metrics, str(exc))
    else:
        if record_metrics:
            metrics.completed += 1
            metrics.successes += 1
            assert metrics.latencies_ms is not None
            metrics.latencies_ms.append((time.perf_counter() - started_at) * 1000.0)


def add_error(metrics: Metrics, error: str) -> None:
    assert metrics.errors is not None
    if len(metrics.errors) < 20:
        metrics.errors.append(error)


async def run_constant_arrival_rate(
    harness: ThroughputHarness,
    rate: int,
    duration_seconds: float,
    vus: int,
    metrics: Metrics,
    record_metrics: bool,
) -> None:
    if duration_seconds <= 0:
        return
    total_slots = int(rate * duration_seconds)
    in_flight: set[asyncio.Task[None]] = set()
    started_at = time.perf_counter()

    for slot in range(total_slots):
        target_time = started_at + (slot / rate)
        delay = target_time - time.perf_counter()
        if delay > 0:
            await asyncio.sleep(delay)

        done = {task for task in in_flight if task.done()}
        in_flight.difference_update(done)

        if len(in_flight) >= vus:
            if record_metrics:
                metrics.attempted += 1
                metrics.dropped_requests += 1
            continue

        if record_metrics:
            metrics.attempted += 1
        task = asyncio.create_task(run_one_request(harness, metrics, record_metrics))
        in_flight.add(task)

    remaining = (started_at + duration_seconds) - time.perf_counter()
    if remaining > 0:
        await asyncio.sleep(remaining)

    if in_flight:
        await asyncio.gather(*in_flight, return_exceptions=True)


def run_command(args: list[str], timeout_seconds: float = 5.0) -> str | None:
    try:
        result = subprocess.run(args, capture_output=True, text=True, timeout=timeout_seconds, check=False)
    except (OSError, subprocess.SubprocessError):
        return None
    output = (result.stdout or result.stderr).strip()
    return output or None


def environment_metadata(config: Config) -> dict[str, Any]:
    return {
        "captured_at": utc_now(),
        "command": sys.argv,
        "python_version": platform.python_version(),
        "platform": platform.platform(),
        "processor": platform.processor(),
        "cpu_count": os.cpu_count(),
        "git_sha": run_command(["git", "rev-parse", "HEAD"]),
        "git_status_short": run_command(["git", "status", "--short"]),
        "docker_version": run_command(["docker", "--version"]),
        "docker_compose_version": run_command(["docker", "compose", "version"]),
        "service_urls": {
            "data_model": config.data_model_url,
            "ingestion": config.ingestion_url,
            "decision_engine": config.decision_engine_url,
        },
    }


def summarize_metrics(config: Config, harness: ThroughputHarness, metrics: Metrics, elapsed_seconds: float) -> dict[str, Any]:
    latencies = metrics.latencies_ms or []
    error_rate = (metrics.failures + metrics.dropped_requests) / metrics.attempted if metrics.attempted else 0.0
    timeout_rate = metrics.timeouts / metrics.attempted if metrics.attempted else 0.0
    success_rate = metrics.successes / metrics.attempted if metrics.attempted else 0.0
    achieved_eps = metrics.successes / config.duration_seconds if config.duration_seconds > 0 else 0.0
    sustainable = metrics.failures == 0 and metrics.timeouts == 0 and metrics.dropped_requests == 0
    return {
        "summary_version": 1,
        "test": {
            "name": "decision_engine_throughput_limit",
            "objective": "Find maximum sustainable direct decision evaluations per second.",
            "system_under_test": "POST /v1/tenants/{tenantId}/scenarios/{scenarioId}/evaluate",
            "sustainability_definition": "0% errors, 0% timeouts, and 0 dropped requests during measured run.",
        },
        "environment": environment_metadata(config),
        "setup": {
            "tenant_id": harness.tenant_id,
            "object_type": harness.object_type,
            "scenario_id": harness.scenario_id,
            "rule_id": harness.rule_id,
            "scenario_count": 1,
            "rules_per_scenario": 1,
            "rule_shape": "amount > scenario_threshold",
            "scenario_threshold": config.scenario_threshold,
        },
        "run": asdict(config) | {"auth_token": "set" if config.auth_token else None},
        "workload_counts": {
            "attempted_evaluations": metrics.attempted,
            "completed_evaluations": metrics.completed,
            "successful_evaluations": metrics.successes,
            "failed_evaluations": metrics.failures,
            "timeouts": metrics.timeouts,
            "dropped_requests": metrics.dropped_requests,
        },
        "rates": {
            "target_evaluations_per_second": config.rate,
            "achieved_successful_evaluations_per_second": achieved_eps,
            "success_rate": success_rate,
            "error_rate": error_rate,
            "timeout_rate": timeout_rate,
        },
        "latency_ms": {
            "avg": statistics.fmean(latencies) if latencies else None,
            "p50": percentile(latencies, 50),
            "p95": percentile(latencies, 95),
            "p99": percentile(latencies, 99),
            "max": max(latencies) if latencies else None,
        },
        "result": {
            "sustainable": sustainable,
            "target_met": sustainable and achieved_eps >= config.rate,
            "throughput_limit_candidate": config.rate if sustainable else None,
            "elapsed_seconds": elapsed_seconds,
        },
        "sample_errors": metrics.errors or [],
    }


def print_text_summary(summary: dict[str, Any]) -> None:
    counts = summary["workload_counts"]
    rates = summary["rates"]
    latency = summary["latency_ms"]
    result = summary["result"]
    print("")
    print("Decision Engine Throughput Limit Summary")
    print(f"  target EPS: {rates['target_evaluations_per_second']}")
    print(f"  achieved EPS: {rates['achieved_successful_evaluations_per_second']:.2f}")
    print(f"  attempted: {counts['attempted_evaluations']}")
    print(f"  successful: {counts['successful_evaluations']}")
    print(f"  failed: {counts['failed_evaluations']}")
    print(f"  timeouts: {counts['timeouts']}")
    print(f"  dropped: {counts['dropped_requests']}")
    print(f"  error rate: {rates['error_rate']:.6f}")
    print("  latency p50 / p95 / p99 / max, ms:")
    print(
        "  "
        f"{format_optional(latency['p50'])} / "
        f"{format_optional(latency['p95'])} / "
        f"{format_optional(latency['p99'])} / "
        f"{format_optional(latency['max'])}"
    )
    print(f"  sustainable: {result['sustainable']}")
    print(f"  target met: {result['target_met']}")
    print("")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Decision-engine direct-evaluation throughput limit stress test.")
    parser.add_argument("--rate", type=int, required=True, help="Target direct decision evaluations per second.")
    parser.add_argument("--vus", type=int, required=True, help="Maximum concurrent in-flight evaluations.")
    parser.add_argument("--duration", type=float, default=60.0, help="Measured run duration in seconds.")
    parser.add_argument("--warmup-duration", type=float, default=30.0, help="Warmup duration in seconds.")
    parser.add_argument("--amount", type=int, default=1800, help="Amount value used in generated payloads.")
    parser.add_argument("--timeout", type=float, default=30.0, help="Per-request timeout in seconds.")
    parser.add_argument("--scenario-threshold", type=int, default=1000, help="Simple amount rule threshold.")
    parser.add_argument("--output", default="stress-tests/throughput-limit-summary.json", help="Summary JSON output path.")
    parser.add_argument("--data-model-url", default=os.getenv("DATA_MODEL_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--ingestion-url", default=os.getenv("INGESTION_URL", "http://127.0.0.1:8081"))
    parser.add_argument("--decision-engine-url", default=os.getenv("DECISION_ENGINE_URL", "http://127.0.0.1:8082"))
    parser.add_argument("--auth-token", default=os.getenv("SERVICE_AUTH_TOKEN"))
    return parser


def parse_config() -> Config:
    args = build_parser().parse_args()
    if args.rate <= 0:
        raise SystemExit("--rate must be greater than 0")
    if args.vus <= 0:
        raise SystemExit("--vus must be greater than 0")
    if args.duration <= 0:
        raise SystemExit("--duration must be greater than 0")
    if args.warmup_duration < 0:
        raise SystemExit("--warmup-duration must be greater than or equal to 0")
    if args.timeout <= 0:
        raise SystemExit("--timeout must be greater than 0")
    return Config(
        rate=args.rate,
        vus=args.vus,
        duration_seconds=args.duration,
        warmup_duration_seconds=args.warmup_duration,
        amount=args.amount,
        timeout_seconds=args.timeout,
        output=args.output,
        data_model_url=args.data_model_url.rstrip("/"),
        ingestion_url=args.ingestion_url.rstrip("/"),
        decision_engine_url=args.decision_engine_url.rstrip("/"),
        auth_token=args.auth_token,
        scenario_threshold=args.scenario_threshold,
    )


async def async_main() -> int:
    config = parse_config()
    summary = await run_trial(config)
    print_text_summary(summary)
    print(f"summary written to {config.output}")
    return 0 if summary["result"]["sustainable"] else 1


async def run_trial(config: Config) -> dict[str, Any]:
    harness = ThroughputHarness(config)
    try:
        print("bootstrapping tenant/model/scenario...")
        await harness.bootstrap()
        print("bootstrap complete")

        if config.warmup_duration_seconds > 0:
            print(f"warming up at {config.rate} EPS for {config.warmup_duration_seconds:.0f}s...")
            await run_constant_arrival_rate(
                harness,
                config.rate,
                config.warmup_duration_seconds,
                config.vus,
                Metrics(),
                record_metrics=False,
            )

        print(f"running measured load at {config.rate} EPS for {config.duration_seconds:.0f}s with {config.vus} VUs...")
        metrics = Metrics()
        started_at = time.perf_counter()
        await run_constant_arrival_rate(harness, config.rate, config.duration_seconds, config.vus, metrics, True)
        elapsed = time.perf_counter() - started_at

        summary = summarize_metrics(config, harness, metrics, elapsed)
        output_path = Path(config.output)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(json.dumps(summary, indent=2, default=str) + "\n")
        return summary
    finally:
        await harness.close()


def main() -> None:
    raise SystemExit(asyncio.run(async_main()))


if __name__ == "__main__":
    main()
