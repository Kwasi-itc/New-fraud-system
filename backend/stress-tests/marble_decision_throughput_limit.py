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


DEFAULT_SCENARIO_NAME = "Stress - Single Value Threshold"


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def unique_id(prefix: str) -> str:
    return f"{prefix}_{uuid.uuid4().hex[:24]}"


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
    return "n/a" if value is None else f"{value:.2f}"


def load_export(path: str) -> dict[str, Any]:
    return json.loads(Path(path).read_text(encoding="utf-8"))


def scenario_id_from_export(path: str, name: str) -> str:
    export = load_export(path)
    for item in export.get("scenarios", []):
        scenario = item.get("scenario", {})
        if scenario.get("name") == name:
            return scenario["id"]
    available = [item.get("scenario", {}).get("name") for item in export.get("scenarios", [])]
    raise SystemExit(f"scenario {name!r} not found in {path}; available scenarios: {available}")


def const_node(value: Any) -> dict[str, Any]:
    return {"constant": value}


def true_node() -> dict[str, Any]:
    return {"name": "=", "children": [const_node(1), const_node(1)]}


def value_gt_node(threshold: float) -> dict[str, Any]:
    return {
        "name": ">",
        "children": [
            {"name": "Payload", "children": [const_node("value")]},
            const_node(threshold),
        ],
    }


def company_payload(company_id: str) -> dict[str, Any]:
    now = utc_now()
    return {
        "object_id": company_id,
        "registration_month": now,
        "created_at": now,
        "updated_at": now,
        "name": "ITC Stress Company",
        "legal_form": "SAS",
        "registration_number": company_id[-16:],
        "activity_type": "6201Z",
        "aml_score": 5.0,
        "country": "DE",
        "city": "BERLIN",
        "zip": "10115",
    }


def account_payload(account_id: str, company_id: str, past_balance: float) -> dict[str, Any]:
    now = utc_now()
    return {
        "object_id": account_id,
        "created_at": now,
        "updated_at": now,
        "balance": past_balance,
        "past_balance": past_balance,
        "iban": f"DE893704004405{uuid.uuid4().int % 10**10:010d}",
        "bic": "COBADEFFXXX",
        "company_id": company_id,
    }


def transaction_payload(transaction_id: str, account_id: str, company_id: str, value: float) -> dict[str, Any]:
    now = utc_now()
    return {
        "object_id": transaction_id,
        "card_payment_currency": "EUR",
        "card_payment_type": "ECOMMERCE",
        "account_id": account_id,
        "company_id": company_id,
        "value": value,
        "status": "PENDING",
        "payment_method": "TRANSFER",
        "card_is_3ds": False,
        "counterparty_iban": "FR7630006000011234567890189",
        "counterparty_bic": "AGRIFRPPXXX",
        "direction": "PAYOUT",
        "transaction_at": now,
        "created_at": now,
        "category": "6012",
        "is_recuring": False,
        "card_merchant_name": "ITC Market",
        "card_payment_country": "DE",
        "creditor_identifier": "stress-creditor",
        "scheme": "CORE",
        "updated_at": now,
        "card_merchant_id": "stress-merchant",
    }


@dataclass(frozen=True)
class Config:
    rate: int
    vus: int
    duration_seconds: float
    warmup_duration_seconds: float
    transaction_value: float
    account_past_balance: float
    timeout_seconds: float
    output: str
    api_url: str
    api_key: str
    org_export: str
    scenario_name: str
    scenario_id: str | None
    scenario_threshold: float
    seed_parent_records: bool
    create_stress_scenario: bool


@dataclass
class Metrics:
    attempted: int = 0
    completed: int = 0
    successes: int = 0
    failures: int = 0
    timeouts: int = 0
    dropped_requests: int = 0
    skipped_decisions: int = 0
    latencies_ms: list[float] | None = None
    errors: list[str] | None = None

    def __post_init__(self) -> None:
        self.latencies_ms = [] if self.latencies_ms is None else self.latencies_ms
        self.errors = [] if self.errors is None else self.errors


class MarbleThroughputHarness:
    def __init__(self, config: Config) -> None:
        headers = {"X-API-KEY": config.api_key, "Content-Type": "application/json"}
        timeout = httpx.Timeout(config.timeout_seconds)
        limits = httpx.Limits(max_connections=max(config.vus * 2, 100), max_keepalive_connections=max(config.vus, 20))
        self.config = config
        self.client = httpx.AsyncClient(base_url=config.api_url, timeout=timeout, headers=headers, limits=limits)
        if config.scenario_id:
            self.scenario_id = config.scenario_id
        elif config.create_stress_scenario:
            self.scenario_id = ""
        else:
            self.scenario_id = scenario_id_from_export(config.org_export, config.scenario_name)
        self.company_id = unique_id("company")
        self.account_id = unique_id("account")
        self._sequence = 0

    async def close(self) -> None:
        await self.client.aclose()

    async def wait_until_ready(self) -> None:
        deadline = time.monotonic() + 30.0
        last_error: Exception | str | None = None
        while time.monotonic() < deadline:
            try:
                response = await self.client.get("/v1/-/version")
                if response.status_code == 200:
                    return
                last_error = f"status {response.status_code}"
            except (httpx.HTTPError, OSError) as exc:
                last_error = exc
            await asyncio.sleep(0.5)
        raise RuntimeError(f"API was not ready: {last_error}")

    async def request(self, method: str, path: str, expected: int | set[int], **kwargs: Any) -> dict[str, Any]:
        response = await self.client.request(method, path, **kwargs)
        expected_set = {expected} if isinstance(expected, int) else expected
        if response.status_code not in expected_set:
            raise RuntimeError(
                f"{method} {response.request.url} returned {response.status_code}, "
                f"expected {sorted(expected_set)}: {response.text}"
            )
        if not response.content:
            return {}
        return response.json()

    async def create_single_rule_scenario(self) -> None:
        scenario = await self.request(
            "POST",
            "/scenarios",
            200,
            json={
                "name": f"{self.config.scenario_name} {uuid.uuid4().hex[:8]}",
                "description": "Dedicated throughput stress scenario: one trigger and one value threshold rule.",
                "trigger_object_type": "transactions",
            },
        )
        self.scenario_id = scenario["id"]

        iteration = await self.request(
            "POST",
            "/scenario-iterations",
            200,
            json={
                "scenario_id": self.scenario_id,
                "body": {
                    "trigger_condition_ast_expression": true_node(),
                    "rules": [
                        {
                            "scenario_iteration_id": "",
                            "display_order": 0,
                            "name": "Transaction value over threshold",
                            "description": "Single-rule throughput baseline.",
                            "formula_ast_expression": value_gt_node(self.config.scenario_threshold),
                            "score_modifier": 25,
                            "rule_group": "throughput",
                        }
                    ],
                    "score_review_threshold": 10,
                    "score_block_and_review_threshold": 30,
                    "score_decline_threshold": 60,
                    "schedule": "",
                },
            },
        )
        iteration_id = iteration["id"]

        validation = await self.request("POST", f"/scenario-iterations/{iteration_id}/validate", 200, json={})
        validation_body = validation.get("scenario_validation", {})
        validation_errors = (
            validation_body.get("trigger", {}).get("errors", [])
            + validation_body.get("rules", {}).get("errors", [])
            + validation_body.get("decision", {}).get("errors", [])
        )
        for rule_validation in validation_body.get("rules", {}).get("rules", {}).values():
            validation_errors += rule_validation.get("errors", [])
        if validation_errors:
            raise RuntimeError(f"stress scenario validation failed: {json.dumps(validation_errors, default=str)}")

        committed = await self.request("POST", f"/scenario-iterations/{iteration_id}/commit", 200, json={})
        committed_iteration_id = committed.get("iteration", {}).get("id", iteration_id)

        await self.request(
            "POST",
            "/scenario-publications",
            200,
            json={"scenario_iteration_id": committed_iteration_id, "publication_action": "publish"},
        )

    async def bootstrap(self) -> None:
        await self.wait_until_ready()
        if not self.scenario_id:
            await self.create_single_rule_scenario()
        if not self.config.seed_parent_records:
            return
        await self.request("POST", "/v1/ingest/companies", {200, 201}, json=company_payload(self.company_id))
        await self.request(
            "POST",
            "/v1/ingest/accounts",
            {200, 201},
            json=account_payload(self.account_id, self.company_id, self.config.account_past_balance),
        )

    def next_transaction_id(self) -> str:
        self._sequence += 1
        return f"tx_{self._sequence}_{uuid.uuid4().hex[:18]}"

    async def decide_once(self) -> bool:
        transaction_id = self.next_transaction_id()
        payload = transaction_payload(
            transaction_id,
            self.account_id,
            self.company_id,
            self.config.transaction_value,
        )
        response = await self.request(
            "POST",
            "/v1/decisions",
            200,
            json={"scenario_id": self.scenario_id, "trigger_object": payload},
        )
        metadata = response.get("metadata", {})
        count = metadata.get("count", metadata)
        return int(count.get("skipped", 0)) == 0


def add_error(metrics: Metrics, error: str) -> None:
    assert metrics.errors is not None
    if len(metrics.errors) < 20:
        metrics.errors.append(error)


async def run_one_request(harness: MarbleThroughputHarness, metrics: Metrics, record_metrics: bool) -> None:
    started_at = time.perf_counter()
    try:
        decision_created = await harness.decide_once()
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
            if decision_created:
                metrics.successes += 1
                assert metrics.latencies_ms is not None
                metrics.latencies_ms.append((time.perf_counter() - started_at) * 1000.0)
            else:
                metrics.failures += 1
                metrics.skipped_decisions += 1
                add_error(metrics, "scenario trigger condition did not match")


async def run_constant_arrival_rate(
    harness: MarbleThroughputHarness,
    rate: int,
    duration_seconds: float,
    vus: int,
    metrics: Metrics,
    record_metrics: bool,
) -> None:
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
        "api_url": config.api_url,
    }


def summarize_metrics(config: Config, harness: MarbleThroughputHarness, metrics: Metrics, elapsed_seconds: float) -> dict[str, Any]:
    latencies = metrics.latencies_ms or []
    error_rate = (metrics.failures + metrics.dropped_requests) / metrics.attempted if metrics.attempted else 0.0
    timeout_rate = metrics.timeouts / metrics.attempted if metrics.attempted else 0.0
    success_rate = metrics.successes / metrics.attempted if metrics.attempted else 0.0
    achieved_eps = metrics.successes / config.duration_seconds if config.duration_seconds > 0 else 0.0
    sustainable = metrics.failures == 0 and metrics.timeouts == 0 and metrics.dropped_requests == 0
    return {
        "summary_version": 1,
        "test": {
            "name": "marble_decision_throughput_limit",
            "objective": "Find maximum sustainable public API decision creations per second.",
            "system_under_test": "POST /v1/decisions",
            "sustainability_definition": "0% errors, 0% timeouts, 0 skipped decisions, and 0 dropped local requests.",
        },
        "environment": environment_metadata(config),
        "setup": {
            "scenario_id": harness.scenario_id,
            "scenario_name": config.scenario_name,
            "trigger_object_type": "transactions",
            "seeded_company_id": harness.company_id if config.seed_parent_records else None,
            "seeded_account_id": harness.account_id if config.seed_parent_records else None,
        },
        "run": asdict(config) | {"api_key": "set"},
        "workload_counts": {
            "attempted_decisions": metrics.attempted,
            "completed_decisions": metrics.completed,
            "successful_decisions": metrics.successes,
            "failed_decisions": metrics.failures,
            "timeouts": metrics.timeouts,
            "dropped_requests": metrics.dropped_requests,
            "skipped_decisions": metrics.skipped_decisions,
        },
        "rates": {
            "target_decisions_per_second": config.rate,
            "achieved_successful_decisions_per_second": achieved_eps,
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
    print("Marble Decision Throughput Limit Summary")
    print(f"  target DPS: {rates['target_decisions_per_second']}")
    print(f"  achieved DPS: {rates['achieved_successful_decisions_per_second']:.2f}")
    print(f"  attempted: {counts['attempted_decisions']}")
    print(f"  successful: {counts['successful_decisions']}")
    print(f"  failed: {counts['failed_decisions']}")
    print(f"  timeouts: {counts['timeouts']}")
    print(f"  dropped: {counts['dropped_requests']}")
    print(f"  skipped: {counts['skipped_decisions']}")
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
    parser = argparse.ArgumentParser(description="Marble public API decision throughput limit stress test.")
    parser.add_argument("--rate", type=int, required=True, help="Target decision creations per second.")
    parser.add_argument("--vus", type=int, required=True, help="Maximum concurrent in-flight decisions.")
    parser.add_argument("--duration", type=float, default=60.0, help="Measured run duration in seconds.")
    parser.add_argument("--warmup-duration", type=float, default=30.0, help="Warmup duration in seconds.")
    parser.add_argument("--transaction-value", type=float, default=10000.0, help="Value used in generated transactions.")
    parser.add_argument("--account-past-balance", type=float, default=200000.0, help="Seeded account past balance.")
    parser.add_argument("--timeout", type=float, default=30.0, help="Per-request timeout in seconds.")
    parser.add_argument("--output", default="stress-tests/marble-throughput-limit-summary.json")
    parser.add_argument("--api-url", default=os.getenv("MARBLE_API_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--api-key", default=os.getenv("MARBLE_API_KEY"))
    parser.add_argument("--org-export", default=os.getenv("ORG_EXPORT", "org-export.json"))
    parser.add_argument("--scenario-name", default=os.getenv("SCENARIO_NAME", DEFAULT_SCENARIO_NAME))
    parser.add_argument("--scenario-id", default=os.getenv("SCENARIO_ID"))
    parser.add_argument("--scenario-threshold", type=float, default=1000.0, help="Threshold for the generated single-rule scenario.")
    parser.add_argument(
        "--use-export-scenario",
        action="store_true",
        help="Use --scenario-id or --org-export/--scenario-name instead of creating a dedicated one-rule stress scenario.",
    )
    parser.add_argument("--skip-parent-seed", action="store_true", help="Do not ingest linked company/account records before the run.")
    return parser


def parse_config() -> Config:
    args = build_parser().parse_args()
    if args.rate <= 0:
        raise SystemExit("--rate must be greater than 0")
    if args.vus <= 0:
        raise SystemExit("--vus must be greater than 0")
    if args.duration <= 0:
        raise SystemExit("--duration must be greater than 0")
    if args.timeout <= 0:
        raise SystemExit("--timeout must be greater than 0")
    if not args.api_key:
        raise SystemExit("set --api-key or MARBLE_API_KEY")
    return Config(
        rate=args.rate,
        vus=args.vus,
        duration_seconds=args.duration,
        warmup_duration_seconds=args.warmup_duration,
        transaction_value=args.transaction_value,
        account_past_balance=args.account_past_balance,
        timeout_seconds=args.timeout,
        output=args.output,
        api_url=args.api_url.rstrip("/"),
        api_key=args.api_key,
        org_export=args.org_export,
        scenario_name=args.scenario_name,
        scenario_id=args.scenario_id,
        scenario_threshold=args.scenario_threshold,
        seed_parent_records=not args.skip_parent_seed,
        create_stress_scenario=not args.use_export_scenario and not args.scenario_id,
    )


async def run_trial(config: Config) -> dict[str, Any]:
    harness = MarbleThroughputHarness(config)
    try:
        print("bootstrapping API readiness and parent records...")
        await harness.bootstrap()
        print(f"bootstrap complete; scenario_id={harness.scenario_id}")

        if config.warmup_duration_seconds > 0:
            print(f"warming up at {config.rate} DPS for {config.warmup_duration_seconds:.0f}s...")
            await run_constant_arrival_rate(
                harness,
                config.rate,
                config.warmup_duration_seconds,
                config.vus,
                Metrics(),
                record_metrics=False,
            )

        print(f"running measured load at {config.rate} DPS for {config.duration_seconds:.0f}s with {config.vus} VUs...")
        metrics = Metrics()
        started_at = time.perf_counter()
        await run_constant_arrival_rate(harness, config.rate, config.duration_seconds, config.vus, metrics, True)
        elapsed = time.perf_counter() - started_at

        summary = summarize_metrics(config, harness, metrics, elapsed)
        output_path = Path(config.output)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(json.dumps(summary, indent=2, default=str) + "\n", encoding="utf-8")
        return summary
    finally:
        await harness.close()


async def async_main() -> int:
    config = parse_config()
    summary = await run_trial(config)
    print_text_summary(summary)
    print(f"summary written to {config.output}")
    return 0 if summary["result"]["sustainable"] else 1


def main() -> None:
    raise SystemExit(asyncio.run(async_main()))


if __name__ == "__main__":
    main()
