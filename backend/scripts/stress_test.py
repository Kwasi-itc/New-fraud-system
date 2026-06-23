from __future__ import annotations

import argparse
import asyncio
import os
import statistics
import time
import uuid
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from typing import Any

import httpx


def unique_name(prefix: str) -> str:
    return f"{prefix}_{uuid.uuid4().hex[:12]}"


def utc_future(minutes: int = 30) -> str:
    return (datetime.now(timezone.utc) + timedelta(minutes=minutes)).isoformat().replace("+00:00", "Z")


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
    return {
        "object_id": object_id,
        "amount": amount,
        "status": "pending",
        "account_id": f"acct_{uuid.uuid4().hex[:8]}",
        "ip": "1.2.3.4",
        "merchant": "ITC Market",
        "email": "Risk@Example.com",
        "country": "gh",
        "owner_id": f"owner_{uuid.uuid4().hex[:8]}",
        "note": None,
    }


def percentile(values: list[float], pct: float) -> float:
    if not values:
        return 0.0
    ordered = sorted(values)
    index = max(0, min(len(ordered) - 1, int(round((pct / 100.0) * (len(ordered) - 1)))))
    return ordered[index]


@dataclass(frozen=True)
class Config:
    mode: str
    concurrency: int
    total_requests: int | None
    duration_seconds: float | None
    batch_size: int
    amount: int
    timeout_seconds: float
    data_model_url: str
    ingestion_url: str
    decision_engine_url: str
    auth_token: str | None
    scenario_threshold: int


class StressHarness:
    def __init__(self, config: Config) -> None:
        headers: dict[str, str] = {}
        if config.auth_token:
            headers["Authorization"] = f"Bearer {config.auth_token}"
        timeout = httpx.Timeout(config.timeout_seconds)
        self.config = config
        self.data_model = httpx.AsyncClient(base_url=config.data_model_url, timeout=timeout, headers=headers)
        self.ingestion = httpx.AsyncClient(base_url=config.ingestion_url, timeout=timeout, headers=headers)
        self.decision_engine = httpx.AsyncClient(base_url=config.decision_engine_url, timeout=timeout, headers=headers)
        self.tenant_id = ""
        self.object_type = ""
        self.scenario_id = ""
        self._request_counter = 0
        self._request_lock = asyncio.Lock()

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
                f"{method} {response.request.url} returned {response.status_code}, expected {sorted(expected_set)}: {response.text}"
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
                json={"name": unique_name("stress tenant"), "external_key": unique_name("ext")},
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
                    "description": "Stress transaction table",
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
                    "description": "Stress account table",
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
                json={"name": unique_name("stress scenario"), "trigger_object_type": self.object_type},
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
        await self.request(
            self.decision_engine,
            "POST",
            f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_id}/iterations/{iteration['id']}/rules",
            201,
            json={
                "display_order": 1,
                "name": "Amount over limit",
                "description": "Stress rule",
                "formula": amount_gt_node(self.config.scenario_threshold),
                "score_modifier": 25,
                "rule_group": "default",
                "stable_rule_id": unique_name("stable_rule"),
            },
        )
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

    async def next_request_number(self) -> int:
        async with self._request_lock:
            self._request_counter += 1
            return self._request_counter

    async def next_object_id(self, worker_id: int) -> str:
        sequence = await self.next_request_number()
        return f"stress_{worker_id}_{sequence}_{uuid.uuid4().hex[:6]}"

    async def run_mode_request(self, worker_id: int) -> None:
        object_id = await self.next_object_id(worker_id)
        payload = record_payload(object_id=object_id, amount=self.config.amount)

        if self.config.mode == "ingest":
            await self.request(
                self.ingestion,
                "POST",
                f"/v1/tenants/{self.tenant_id}/ingest/{self.object_type}",
                200,
                json=payload,
            )
            return

        if self.config.mode == "batch":
            batch_payload = [
                record_payload(object_id=f"{object_id}_{index}", amount=self.config.amount + index)
                for index in range(self.config.batch_size)
            ]
            await self.request(
                self.ingestion,
                "POST",
                f"/v1/tenants/{self.tenant_id}/ingest/{self.object_type}/batch",
                200,
                json=batch_payload,
            )
            return

        if self.config.mode == "decision":
            await self.request(
                self.decision_engine,
                "POST",
                f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_id}/evaluate",
                200,
                json={"object_id": object_id, "object_type": self.object_type, "fields": payload},
            )
            return

        if self.config.mode == "ingestion-event":
            await self.request(
                self.decision_engine,
                "POST",
                f"/v1/tenants/{self.tenant_id}/ingestion-events/record-ingested",
                200,
                json={"object_id": object_id, "object_type": self.object_type, "fields": payload, "source": "stress-test"},
            )
            return

        if self.config.mode == "end-to-end":
            await self.request(
                self.ingestion,
                "POST",
                f"/v1/tenants/{self.tenant_id}/ingest/{self.object_type}",
                200,
                json=payload,
            )
            await self.request(
                self.decision_engine,
                "POST",
                f"/v1/tenants/{self.tenant_id}/scenarios/{self.scenario_id}/evaluate",
                200,
                json={"object_id": object_id, "object_type": self.object_type, "fields": payload},
            )
            return

        raise ValueError(f"unsupported mode: {self.config.mode}")


async def worker(
    worker_id: int,
    harness: StressHarness,
    metrics: dict[str, Any],
    stop_at: float | None,
    request_budget: int | None,
) -> None:
    while True:
        if stop_at is not None and time.monotonic() >= stop_at:
            return
        if request_budget is not None:
            async with metrics["counter_lock"]:
                if metrics["started"] >= request_budget:
                    return
                metrics["started"] += 1

        started_at = time.perf_counter()
        try:
            await harness.run_mode_request(worker_id)
        except Exception as exc:
            metrics["failures"] += 1
            metrics["errors"].append(str(exc))
        else:
            metrics["successes"] += 1
            metrics["latencies_ms"].append((time.perf_counter() - started_at) * 1000.0)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Stress test harness for ingestion and decisioning services.")
    parser.add_argument(
        "--mode",
        choices=["ingest", "batch", "decision", "ingestion-event", "end-to-end"],
        default="ingest",
        help="Traffic profile to run.",
    )
    parser.add_argument("--concurrency", type=int, default=10, help="Number of concurrent workers.")
    parser.add_argument("--requests", type=int, default=None, help="Total requests to send across all workers.")
    parser.add_argument("--duration", type=float, default=30.0, help="Run length in seconds when --requests is not set.")
    parser.add_argument("--batch-size", type=int, default=10, help="Rows per batch request in batch mode.")
    parser.add_argument("--amount", type=int, default=1250, help="Transaction amount used in generated payloads.")
    parser.add_argument("--timeout", type=float, default=30.0, help="Per-request timeout in seconds.")
    parser.add_argument(
        "--scenario-threshold",
        type=int,
        default=1000,
        help="Decision rule threshold used when bootstrapping the scenario.",
    )
    parser.add_argument("--data-model-url", default=os.getenv("DATA_MODEL_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--ingestion-url", default=os.getenv("INGESTION_URL", "http://127.0.0.1:8081"))
    parser.add_argument("--decision-engine-url", default=os.getenv("DECISION_ENGINE_URL", "http://127.0.0.1:8082"))
    parser.add_argument("--auth-token", default=os.getenv("SERVICE_AUTH_TOKEN"))
    return parser


def print_summary(config: Config, tenant_id: str, object_type: str, scenario_id: str, metrics: dict[str, Any], elapsed: float) -> None:
    latencies = metrics["latencies_ms"]
    total = metrics["successes"] + metrics["failures"]
    throughput = total / elapsed if elapsed > 0 else 0.0
    print(f"mode={config.mode}")
    print(f"tenant_id={tenant_id}")
    print(f"object_type={object_type}")
    print(f"scenario_id={scenario_id}")
    print(f"elapsed_seconds={elapsed:.2f}")
    print(f"total_operations={total}")
    print(f"successful_operations={metrics['successes']}")
    print(f"failed_operations={metrics['failures']}")
    print(f"throughput_ops_per_sec={throughput:.2f}")
    if latencies:
        print(f"latency_avg_ms={statistics.fmean(latencies):.2f}")
        print(f"latency_p50_ms={percentile(latencies, 50):.2f}")
        print(f"latency_p95_ms={percentile(latencies, 95):.2f}")
        print(f"latency_p99_ms={percentile(latencies, 99):.2f}")
        print(f"latency_max_ms={max(latencies):.2f}")
    if metrics["errors"]:
        print("sample_errors:")
        for error in metrics["errors"][:10]:
            print(f"  - {error}")


async def async_main() -> int:
    args = build_parser().parse_args()
    if args.requests is None and args.duration <= 0:
        raise SystemExit("--duration must be greater than 0 when --requests is not set")
    if args.concurrency <= 0:
        raise SystemExit("--concurrency must be greater than 0")
    if args.batch_size <= 0:
        raise SystemExit("--batch-size must be greater than 0")

    config = Config(
        mode=args.mode,
        concurrency=args.concurrency,
        total_requests=args.requests,
        duration_seconds=None if args.requests is not None else args.duration,
        batch_size=args.batch_size,
        amount=args.amount,
        timeout_seconds=args.timeout,
        data_model_url=args.data_model_url,
        ingestion_url=args.ingestion_url,
        decision_engine_url=args.decision_engine_url,
        auth_token=args.auth_token,
        scenario_threshold=args.scenario_threshold,
    )

    harness = StressHarness(config)
    try:
        print("bootstrapping tenant/model/scenario...")
        await harness.bootstrap()
        print("bootstrap complete")

        metrics: dict[str, Any] = {
            "started": 0,
            "successes": 0,
            "failures": 0,
            "latencies_ms": [],
            "errors": [],
            "counter_lock": asyncio.Lock(),
        }
        stop_at = time.monotonic() + config.duration_seconds if config.duration_seconds is not None else None
        started_at = time.perf_counter()
        await asyncio.gather(
            *[
                worker(
                    worker_id=index + 1,
                    harness=harness,
                    metrics=metrics,
                    stop_at=stop_at,
                    request_budget=config.total_requests,
                )
                for index in range(config.concurrency)
            ]
        )
        elapsed = time.perf_counter() - started_at
        print_summary(config, harness.tenant_id, harness.object_type, harness.scenario_id, metrics, elapsed)
        return 0 if metrics["failures"] == 0 else 1
    finally:
        await harness.close()


def main() -> None:
    raise SystemExit(asyncio.run(async_main()))


if __name__ == "__main__":
    main()
