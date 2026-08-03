#!/usr/bin/env python3
"""External synthetic replay for the logical daily-bucket aggregate cache.

The target tenant must already exist. The harness provisions its ingestion schema,
creates the minimal transaction model, daily logical bucket, and aggregate scenario.
No production CSV files are required.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import statistics
import time
import uuid
from datetime import datetime, timedelta, timezone
from typing import Any

from production_replay.api_client import APIError, ServiceClients, ServiceConfig


def parser() -> argparse.ArgumentParser:
    value = argparse.ArgumentParser(description=__doc__)
    value.add_argument("--tenant-id", required=True)
    value.add_argument("--data-model-url", default=os.getenv("DATA_MODEL_URL", "http://127.0.0.1:8080"))
    value.add_argument("--ingestion-url", default=os.getenv("INGESTION_URL", "http://127.0.0.1:8081"))
    value.add_argument("--decision-engine-url", default=os.getenv("DECISION_ENGINE_URL", "http://127.0.0.1:8082"))
    value.add_argument("--redis-url", default=os.getenv("REDIS_URL", "redis://127.0.0.1:6379/0"))
    value.add_argument("--auth-token", default=os.getenv("SERVICE_AUTH_TOKEN"))
    value.add_argument("--days", type=int, default=10)
    value.add_argument("--transactions-per-day", type=int, default=20)
    value.add_argument("--warm-runs", type=int, default=5)
    value.add_argument("--concurrency", type=int, default=10)
    value.add_argument("--output", default="partition-optimization-summary.json")
    value.add_argument("--setup-timeout", type=float, default=900.0)
    return value


async def main_async(args: argparse.Namespace) -> int:
    try:
        import redis.asyncio as redis
    except ImportError as exc:  # pragma: no cover - exercised by installation
        raise RuntimeError("install backend/stress-tests/production_replay/requirements.txt") from exc
    if args.days < 3 or args.transactions_per_day < 1 or args.warm_runs < 1 or args.concurrency < 1:
        raise ValueError("days >= 3 and all count arguments must be positive")
    config = ServiceConfig(
        args.data_model_url, args.ingestion_url, args.decision_engine_url,
        auth_token=args.auth_token, timeout_seconds=60, max_connections=max(args.concurrency * 2, 20),
    )
    cache = redis.from_url(args.redis_url, decode_responses=False)
    run_id = uuid.uuid4().hex[:12]
    summary: dict[str, Any] = {"run_id": run_id, "tenant_id": args.tenant_id, "status": "running", "phases": {}}
    try:
        async with ServiceClients(config) as clients:
            await clients.wait_until_ready()
            setup = await setup_target(clients, args.tenant_id, args.setup_timeout)
            summary["setup"] = setup
            model = await validate_target(clients, args.tenant_id)
            bucket = model["bucket"]
            seal_delay = timedelta(seconds=int(bucket["seal_delay_seconds"]))
            anchor = (datetime.now(timezone.utc) - seal_delay - timedelta(days=2)).replace(hour=0, minute=0, second=0, microsecond=0)
            first_day = anchor - timedelta(days=args.days - 1)
            known_fields: set[str] = model["fields"]

            before_keys = await aggregate_keys(cache, args.tenant_id)
            records = synthetic_records(run_id, first_day, args.days, args.transactions_per_day, known_fields)
            ingest_ms = await ingest_records(clients, args.tenant_id, records, run_id)
            summary["phases"]["synthetic_backfill"] = {
                "transactions": len(records), "days": args.days, "latency_ms": round(ingest_ms, 2),
                "first_timestamp": records[0]["date"], "last_timestamp": records[-1]["date"],
            }

            trigger_at = anchor + timedelta(hours=23, minutes=59)
            cold_ms = await decision(clients, args.tenant_id, trigger(run_id, "cold", trigger_at, known_fields))
            cold_keys = await aggregate_keys(cache, args.tenant_id)
            added_cold = len(cold_keys - before_keys)
            if added_cold == 0:
                raise AssertionError("cold decision created no aggregate:v2 Redis keys; confirm direct_cached mode and cache eligibility")
            summary["phases"]["cold_cache"] = {"latency_ms": round(cold_ms, 2), "new_cache_keys": added_cold}

            warm_latencies = []
            for index in range(args.warm_runs):
                warm_latencies.append(await decision(clients, args.tenant_id, trigger(run_id, f"warm-{index}", trigger_at, known_fields)))
            warm_keys = await aggregate_keys(cache, args.tenant_id)
            if warm_keys != cold_keys:
                raise AssertionError("warm decisions unexpectedly created cache keys; equivalent filters should reuse canonical keys")
            summary["phases"]["warm_cache"] = latency_summary(warm_latencies) | {"new_cache_keys": 0}

            late_day = first_day + timedelta(days=1, hours=12)
            late = transaction(run_id, "late-correction", late_day, 999.0, known_fields)
            await clients.ingest_one(args.tenant_id, "transactions", late, f"partition-demo:{run_id}:late")
            invalidation_ms = await decision(clients, args.tenant_id, trigger(run_id, "after-invalidation", trigger_at, known_fields))
            invalidated_keys = await aggregate_keys(cache, args.tenant_id)
            new_generation_keys = len(invalidated_keys - warm_keys)
            if new_generation_keys == 0:
                raise AssertionError("late sealed-day write produced no new generation-keyed cache entries")
            summary["phases"]["generation_invalidation"] = {
                "latency_ms": round(invalidation_ms, 2), "new_generation_keys": new_generation_keys,
                "late_transaction_timestamp": late["date"],
            }

            boundary_at = anchor + timedelta(days=1)
            burst = [
                transaction(run_id, f"boundary-burst-{index}", boundary_at - timedelta(minutes=55 - index * 5), 25.0, known_fields)
                for index in range(10)
            ]
            await ingest_records(clients, args.tenant_id, burst, run_id + ":boundary-burst")
            boundary_record = transaction(run_id, "midnight-boundary", boundary_at, 101.0, known_fields)
            await clients.ingest_one(args.tenant_id, "transactions", boundary_record, f"partition-demo:{run_id}:boundary")
            boundary_trigger = trigger(run_id, "inclusive-boundary", boundary_at, known_fields)
            boundary_started = time.perf_counter()
            boundary_response = await clients.decide_once(args.tenant_id, str(boundary_trigger["object_id"]), boundary_trigger)
            boundary_ms = (time.perf_counter() - boundary_started) * 1000
            hits = triggered_rules(boundary_response)
            if "One Hour Transfer Burst" not in hits:
                raise AssertionError(f"inclusive boundary aggregate did not trigger One Hour Transfer Burst; hits={sorted(hits)}")
            summary["phases"]["inclusive_boundary"] = {
                "latency_ms": round(boundary_ms, 2), "timestamp": boundary_record["date"],
                "expected_rule": "One Hour Transfer Burst", "status": "passed",
            }

            semaphore = asyncio.Semaphore(args.concurrency)
            async def concurrent(index: int) -> float:
                async with semaphore:
                    return await decision(clients, args.tenant_id, trigger(run_id, f"concurrent-{index}", trigger_at, known_fields))
            concurrent_latencies = await asyncio.gather(*(concurrent(i) for i in range(args.concurrency * 2)))
            summary["phases"]["concurrent_warm_cache"] = latency_summary(concurrent_latencies) | {
                "requests": len(concurrent_latencies), "concurrency": args.concurrency,
            }
            summary["redis"] = {
                "keys_before": len(before_keys), "keys_after": len(await aggregate_keys(cache, args.tenant_id)),
                "note": "old generation keys remain because cache cleanup is intentionally deferred",
            }
            summary["status"] = "passed"
    except (APIError, AssertionError, ValueError) as exc:
        summary["status"] = "failed"
        summary["error"] = str(exc)
    finally:
        await cache.aclose()
        with open(args.output, "w", encoding="utf-8") as handle:
            json.dump(summary, handle, indent=2, sort_keys=True)
        print(json.dumps(summary, indent=2, sort_keys=True))
    return 0 if summary["status"] == "passed" else 1


async def validate_target(clients: ServiceClients, tenant_id: str) -> dict[str, Any]:
    response = await clients.request(clients.data_model, "GET", f"/v1/tenants/{tenant_id}/data-model", 200)
    model = response.get("data_model", {})
    tables = model.get("tables", {})
    transactions = tables.get("transactions") or next((v for v in tables.values() if v.get("name") == "transactions"), None)
    if not transactions:
        raise ValueError("tenant setup did not produce a published transactions table")
    fields = {item.get("name", key) for key, item in transactions.get("fields", {}).items()}
    buckets = model.get("logical_bucket_definitions", [])
    bucket = next((item for item in buckets if item.get("table_id") == transactions.get("id") and item.get("timestamp_field_name") == "date" and item.get("status") == "active"), None)
    if not bucket:
        raise ValueError("tenant has no active transactions.date logical bucket")
    eligible = datetime.fromisoformat(str(bucket["cache_eligible_at"]).replace("Z", "+00:00"))
    if eligible > datetime.now(timezone.utc):
        raise ValueError(f"logical bucket is not cache eligible until {eligible.isoformat()}")
    scenarios = await clients.request(clients.decision_engine, "GET", f"/v1/tenants/{tenant_id}/scenarios", 200)
    live = {str(item.get("name")) for item in scenarios.get("scenarios", []) if item.get("live_iteration_id")}
    if "Partition Optimization Synthetic Replay" not in live:
        raise ValueError("synthetic partition optimization scenario was not published")
    return {"bucket": bucket, "fields": fields}


async def setup_target(clients: ServiceClients, tenant_id: str, timeout_seconds: float) -> dict[str, Any]:
    await clients.request(clients.data_model, "GET", f"/v1/tenants/{tenant_id}", 200)
    await clients.request(clients.data_model, "POST", f"/v1/tenants/{tenant_id}/provision", {200, 409})
    tables_response = await clients.request(clients.data_model, "GET", f"/v1/tenants/{tenant_id}/tables", 200)
    table = next((item for item in tables_response.get("tables", []) if item.get("name") == "transactions"), None)
    created_table = table is None
    if table is None:
        table = (await clients.request(
            clients.data_model, "POST", f"/v1/tenants/{tenant_id}/tables", 201,
            json={"name": "transactions", "alias": "Transactions", "description": "Synthetic partition optimization replay", "semantic_type": "entity"},
        ))["table"]
    field_specs = (
        ("transaction_id", "string", False, True), ("date", "timestamp", False, False),
        ("amount", "float", False, False), ("channel", "string", False, False),
        ("direction", "string", False, False), ("system_type", "string", False, False),
        ("stream_id", "string", False, False), ("account_ref", "string", True, False),
    )
    fields_response = await clients.request(clients.data_model, "GET", f"/v1/tables/{table['id']}/fields", 200)
    fields = {item["name"]: item for item in fields_response.get("fields", [])}
    created_fields: list[str] = []
    for name, data_type, nullable, unique in field_specs:
        existing = fields.get(name)
        if existing is not None:
            if existing.get("data_type") != data_type or (not nullable and existing.get("nullable") is not False):
                raise ValueError(f"existing transactions.{name} is incompatible with the synthetic replay")
            continue
        fields[name] = (await clients.request(
            clients.data_model, "POST", f"/v1/tables/{table['id']}/fields", 201,
            json={"name": name, "data_type": data_type, "nullable": nullable, "is_unique": unique},
        ))["field"]
        created_fields.append(name)

    deadline = time.monotonic() + timeout_seconds
    last_error = "data model not published"
    while time.monotonic() < deadline:
        try:
            response = await clients.request(clients.data_model, "GET", f"/v1/tenants/{tenant_id}/data-model", 200)
            published = response.get("data_model", {})
            tables = published.get("tables", {})
            published_table = tables.get("transactions") or next((item for item in tables.values() if item.get("name") == "transactions"), None)
            if published_table and all(name in {value.get("name", key) for key, value in published_table.get("fields", {}).items()} for name, *_ in field_specs):
                break
        except APIError as exc:
            last_error = str(exc)
        await asyncio.sleep(1)
    else:
        raise ValueError(f"transactions model did not become available within {timeout_seconds:g}s: {last_error}")

    bucket_path = f"/v1/tables/{table['id']}/logical-buckets"
    bucket_response = await clients.request(clients.data_model, "GET", bucket_path, 200)
    bucket = next((item for item in bucket_response.get("logical_buckets", []) if item.get("timestamp_field_id") == fields["date"]["id"] and item.get("status") != "retired"), None)
    created_bucket = bucket is None
    if bucket is None:
        bucket = (await clients.request(
            clients.data_model, "POST", f"/v1/tenants/{tenant_id}/tables/{table['id']}/logical-buckets", 201,
            json={"timestamp_field_id": fields["date"]["id"], "timezone": "UTC"},
        ))["logical_bucket"]
    while time.monotonic() < deadline:
        bucket_response = await clients.request(clients.data_model, "GET", bucket_path, 200)
        bucket = next((item for item in bucket_response.get("logical_buckets", []) if item.get("id") == bucket["id"]), bucket)
        eligible_text = bucket.get("cache_eligible_at")
        eligible = datetime.fromisoformat(str(eligible_text).replace("Z", "+00:00")) if eligible_text else None
        if bucket.get("status") == "active" and eligible and eligible <= datetime.now(timezone.utc):
            break
        if bucket.get("status") == "blocked_data":
            raise ValueError("logical bucket activation was blocked by null transaction dates")
        await asyncio.sleep(1)
    else:
        raise ValueError(f"logical bucket did not become active and cache eligible within {timeout_seconds:g}s; last status={bucket.get('status')}")

    scenario = await ensure_synthetic_scenario(clients, tenant_id, timeout_seconds)
    return {"created_table": created_table, "created_fields": created_fields, "created_bucket": created_bucket, "scenario": scenario}


async def ensure_synthetic_scenario(clients: ServiceClients, tenant_id: str, timeout_seconds: float) -> dict[str, Any]:
    name = "Partition Optimization Synthetic Replay"
    response = await clients.request(clients.decision_engine, "GET", f"/v1/tenants/{tenant_id}/scenarios", 200)
    existing = next((item for item in response.get("scenarios", []) if item.get("name") == name), None)
    if existing and existing.get("live_iteration_id"):
        return {"id": existing["id"], "created": False, "published": True}
    if existing:
        raise ValueError(f"scenario {name!r} exists but is not published; use a clean tenant or publish/delete it")
    scenario = (await clients.request(
        clients.decision_engine, "POST", f"/v1/tenants/{tenant_id}/scenarios", 201,
        json={"name": name, "trigger_object_type": "transactions"},
    ))["scenario"]
    iteration = (await clients.request(
        clients.decision_engine, "POST", f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations", 201,
    ))["iteration"]
    base = f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}"
    await clients.request(clients.decision_engine, "PUT", base, 200, json={
        "trigger_formula": formula("eq", constant(1), constant(1)), "schedule": "",
        "score_review_threshold": 1, "score_block_and_review_threshold": 9999, "score_decline_threshold": 99999,
    })
    rules = (
        ("Synthetic Ten Day Partition Count", 1, formula("gt", aggregate_formula("transaction_id", "COUNT", "P10D"), constant(0))),
        ("One Hour Transfer Burst", 1, formula("gt", aggregate_formula("transaction_id", "COUNT", "PT1H"), constant(10))),
    )
    for order, (rule_name, score, rule_formula) in enumerate(rules, 1):
        await clients.request(clients.decision_engine, "POST", f"{base}/rules", 201, json={
            "display_order": order, "name": rule_name, "description": "Synthetic logical-bucket optimization assertion",
            "formula": rule_formula, "score_modifier": score, "rule_group": "Partition Optimization",
            "stable_rule_id": rule_name.replace(" ", ""),
        })
    validation = await clients.request(clients.decision_engine, "POST", f"{base}/validate", 200)
    if validation.get("validation", {}).get("valid") is not True:
        raise ValueError(f"synthetic scenario validation failed: {json.dumps(validation)}")
    await clients.request(clients.decision_engine, "POST", f"{base}/commit", 200)
    preparation = f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/publications/preparation"
    preparation_response = await clients.request(
        clients.decision_engine, "GET", preparation, 200, params={"iteration_id": iteration["id"]}
    )
    status = preparation_response.get("preparation", preparation_response)
    prepared = status.get("preparation_finished") is True and status.get("preparation_required") is not True
    if not prepared:
        await clients.request(clients.decision_engine, "POST", preparation, 202, json={"iteration_id": iteration["id"]})
    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        preparation_response = await clients.request(
            clients.decision_engine, "GET", preparation, 200, params={"iteration_id": iteration["id"]}
        )
        status = preparation_response.get("preparation", preparation_response)
        if status.get("preparation_finished") is True and status.get("preparation_required") is not True:
            break
        await asyncio.sleep(1)
    else:
        raise ValueError("synthetic scenario index preparation timed out; confirm data-model-worker is running")
    await clients.request(
        clients.decision_engine, "POST", f"/v1/tenants/{tenant_id}/scenarios/{scenario['id']}/publications", 200,
        json={"action": "publish", "iteration_id": iteration["id"]},
    )
    return {"id": scenario["id"], "created": True, "published": True}


def constant(value: Any) -> dict[str, Any]:
    return {"constant": value}


def field_formula(name: str) -> dict[str, Any]:
    return {"function": "field_ref", "named_children": {"field": constant(name)}}


def formula(name: str, *children: dict[str, Any]) -> dict[str, Any]:
    return {"function": name, "children": list(children)}


def filter_formula(field: str, operator: str, value: dict[str, Any]) -> dict[str, Any]:
    return {"function": "Filter", "named_children": {"tableName": constant("transactions"), "fieldName": constant(field), "operator": constant(operator), "value": value}}


def aggregate_formula(field: str, operation: str, duration: str) -> dict[str, Any]:
    lower = {"function": "TimeAdd", "named_children": {"timestampField": field_formula("date"), "duration": constant(duration), "sign": constant("-")}}
    filters = {"function": "List", "children": [
        filter_formula("account_ref", "=", field_formula("account_ref")),
        filter_formula("date", ">=", lower), filter_formula("date", "<=", field_formula("date")),
    ]}
    return {"function": "Aggregator", "named_children": {"tableName": constant("transactions"), "fieldName": constant(field), "aggregator": constant(operation), "filters": filters}}


def synthetic_records(run_id: str, first_day: datetime, days: int, per_day: int, fields: set[str]) -> list[dict[str, Any]]:
    result = []
    for day in range(days):
        for index in range(per_day):
            occurred_at = first_day + timedelta(days=day, seconds=((index + 1) * 86400 // (per_day + 1)))
            result.append(transaction(run_id, f"d{day}-r{index}", occurred_at, float(10 + day + index), fields))
    return result


def transaction(run_id: str, suffix: str, occurred_at: datetime, amount: float, fields: set[str]) -> dict[str, Any]:
    identifier = f"partition-opt:{run_id}:{suffix}"
    values: dict[str, Any] = {
        "object_id": identifier, "transaction_id": identifier, "amount": amount,
        "channel": "wallet", "date": occurred_at.isoformat().replace("+00:00", "Z"),
        "direction": "incoming", "stream_id": "synthetic-partition-optimization",
        "system_type": "synthetic", "account_ref": f"partition-account-{run_id}",
        "merchant_id": f"partition-merchant-{run_id}", "payment_msisdn": f"233{run_id[:9]}",
        "thirdparty_id": f"partition-thirdparty-{run_id}", "terminal_id": f"partition-terminal-{run_id}",
        "source_id": "partition-optimization-replay", "currency": "GHS",
    }
    return {key: value for key, value in values.items() if key in fields}


def trigger(run_id: str, suffix: str, occurred_at: datetime, fields: set[str]) -> dict[str, Any]:
    return transaction(run_id, f"trigger-{suffix}-{uuid.uuid4().hex[:8]}", occurred_at, 100.0, fields)


async def ingest_records(clients: ServiceClients, tenant_id: str, records: list[dict[str, Any]], run_id: str) -> float:
    started = time.perf_counter()
    for offset in range(0, len(records), 500):
        await clients.ingest_batch(tenant_id, "transactions", records[offset:offset + 500], f"partition-demo:{run_id}:batch:{offset // 500}")
    return (time.perf_counter() - started) * 1000


async def decision(clients: ServiceClients, tenant_id: str, fields: dict[str, Any]) -> float:
    started = time.perf_counter()
    await clients.decide_once(tenant_id, str(fields["object_id"]), fields)
    return (time.perf_counter() - started) * 1000


async def aggregate_keys(cache: Any, tenant_id: str) -> set[bytes]:
    result: set[bytes] = set()
    async for key in cache.scan_iter(match=f"aggregate:v2:{tenant_id}:*", count=500):
        result.add(key if isinstance(key, bytes) else key.encode())
    return result


def latency_summary(values: list[float]) -> dict[str, float]:
    ordered = sorted(values)
    p95 = ordered[min(len(ordered) - 1, max(0, int(len(ordered) * 0.95) - 1))]
    return {"min_ms": round(ordered[0], 2), "median_ms": round(statistics.median(ordered), 2), "p95_ms": round(p95, 2), "max_ms": round(ordered[-1], 2)}


def triggered_rules(response: dict[str, Any]) -> set[str]:
    hits: set[str] = set()
    for result in response.get("result", {}).get("results", []):
        for execution in result.get("rule_executions", []):
            if execution.get("outcome") == "hit" and execution.get("rule_name"):
                hits.add(str(execution["rule_name"]))
    return hits


def main() -> None:
    raise SystemExit(asyncio.run(main_async(parser().parse_args())))


if __name__ == "__main__":
    main()
