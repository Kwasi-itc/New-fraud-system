from __future__ import annotations

import argparse
import asyncio
import json
import os
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import httpx


DEFAULT_EVENT_DATE = "2026-06-01T01:05:00Z"
DEFAULT_RAW_TIMESTAMP = "2026-06-01 01:05:00"
DEFAULT_DECISION_SERVICE_BASE_URL = "http://54.246.247.31"
DEFAULT_DECISION_SERVICE_PORT = "8082"
DEFAULT_TENANT_ID = "e83b4edf-570d-46af-9ff5-c189729a0897"
DEFAULT_SCENARIO_ID = "03b820dc-0457-4639-8a65-1b278141c56b"
DEFAULT_CONCURRENCY_LEVELS = "10,13,15,17,18"

DEFAULT_FIELDS: dict[str, Any] = {
    "amount": 100000.0,
    "fees": 0.0,
    "currency": "GHS",
    "country": "GH",
    "channel": "wallet",
    "direction": "incoming",
    "system_type": "wallet_transfer",
    "stream_id": "manual-sync",
    "processor": "uniwallet_v2",
    "transaction_type": "incoming",
    "payment_type": "wallet",
    "channel_id": "wallet",
    "source_id": "MTN",
    "thirdparty_id": "manual-thirdparty",
    "source_account_no": "233240000001",
    "terminal_id": "POS-42",
    "merchant_id": "1099",
    "product_id": "product-456",
    "sub_merchant_id": "1099",
    "account_ref": "233240000001",
    "account_name": "Manual Async Test",
    "payment_msisdn": "233240000001",
    "narration": "manual synchronous decision create test",
    "raw_account_ref": "233240000001",
    "raw_account_name": "Manual Async Test",
}


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def default_run_id() -> str:
    return datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")


def env_default(name: str, fallback: str) -> str:
    return os.getenv(name, "").strip() or fallback


def default_decision_engine_url() -> str:
    configured_url = os.getenv("DECISION_ENGINE_URL", "").strip() or os.getenv(
        "NEXT_PUBLIC_DECISION_ENGINE_SERVICE_URL", ""
    ).strip()
    if configured_url:
        return configured_url
    base_url = env_default("NEXT_PUBLIC_SERVICE_BASE_URL", DEFAULT_DECISION_SERVICE_BASE_URL).rstrip("/")
    port = env_default("DECISION_ENGINE_FORWARDED_PORT", DEFAULT_DECISION_SERVICE_PORT)
    return f"{base_url}:{port}"


def parse_concurrency_levels(raw: str) -> list[int]:
    levels: list[int] = []
    for part in raw.split(","):
        value = part.strip()
        if not value:
            continue
        level = int(value)
        if level <= 0:
            raise ValueError("--concurrency-levels values must be positive")
        levels.append(level)
    if not levels:
        raise ValueError("--concurrency-levels must contain at least one positive integer")
    return levels


def percentile(values: list[float], pct: float) -> float | None:
    if not values:
        return None
    ordered = sorted(values)
    index = min(len(ordered) - 1, max(0, round((pct / 100) * (len(ordered) - 1))))
    return round(ordered[index], 2)


def summarize_results(results: list[dict[str, Any]], elapsed_seconds: float | None = None) -> dict[str, Any]:
    latencies = [float(result["latency_ms"]) for result in results if isinstance(result.get("latency_ms"), int | float)]
    status_counts: dict[str, int] = {}
    for result in results:
        if result.get("dry_run"):
            key = "dry_run"
        else:
            status = result.get("status_code")
            key = "connection_error" if status is None else str(status)
        status_counts[key] = status_counts.get(key, 0) + 1
    summary: dict[str, Any] = {
        "requests": len(results),
        "ok": sum(1 for result in results if result.get("ok")),
        "failures": sum(1 for result in results if not result.get("ok")),
        "deferred": sum(1 for result in results if result.get("deferred") is True),
        "status_counts": status_counts,
        "latency_ms_min": round(min(latencies), 2) if latencies else None,
        "latency_ms_p50": percentile(latencies, 50),
        "latency_ms_p95": percentile(latencies, 95),
        "latency_ms_p99": percentile(latencies, 99),
        "latency_ms_max": round(max(latencies), 2) if latencies else None,
    }
    if elapsed_seconds is not None:
        summary["elapsed_seconds"] = round(elapsed_seconds, 3)
        summary["requests_per_second"] = round(len(results) / elapsed_seconds, 3) if elapsed_seconds > 0 else None
    return summary


def load_base_fields(args: argparse.Namespace) -> dict[str, Any]:
    fields = dict(DEFAULT_FIELDS)
    if args.base_fields_file:
        file_value = json.loads(Path(args.base_fields_file).read_text(encoding="utf-8"))
        if not isinstance(file_value, dict):
            raise ValueError("--base-fields-file must contain a JSON object")
        fields.update(file_value)
    if args.base_fields:
        inline_value = json.loads(args.base_fields)
        if not isinstance(inline_value, dict):
            raise ValueError("--base-fields must be a JSON object")
        fields.update(inline_value)
    if args.amount is not None:
        fields["amount"] = args.amount
    return fields


def build_request(args: argparse.Namespace, base_fields: dict[str, Any], index: int) -> tuple[str, dict[str, Any]]:
    suffix = f"{index:06d}"
    object_id = f"{args.object_prefix}-{args.run_id}-{suffix}"
    source_trans_id = f"{args.source_transaction_prefix}-{args.run_id}-{suffix}"
    request_key = f"{args.request_key_prefix}-{args.run_id}-{suffix}"
    fields = dict(base_fields)
    fields.update(
        {
            "object_id": object_id,
            "transaction_id": object_id,
            "source_trans_id": source_trans_id,
            "source_transaction_id": source_trans_id,
            "raw_timestamp": args.raw_timestamp,
            "date": args.event_date,
        }
    )
    if args.endpoint_mode == "async":
        item: dict[str, Any] = {
            "object_id": object_id,
            "object_type": "transactions",
            "fields": fields,
        }
        body: dict[str, Any] = {
            "scenario_id": args.scenario_id,
            "object_type": "transactions",
            "idempotency_key": request_key,
            "wait_timeout_ms": args.wait_timeout_ms,
            "items": [item],
        }
        if args.callback_url:
            body["callback_url"] = args.callback_url
    else:
        body = {
            "scenario_id": args.scenario_id,
            "object_id": object_id,
            "object_type": "transactions",
            "fields": fields,
        }
    return request_key, body


async def submit_one(
    client: httpx.AsyncClient,
    endpoint_mode: str,
    tenant_id: str,
    index: int,
    level: int,
    level_index: int,
    request_key: str,
    body: dict[str, Any],
) -> dict[str, Any]:
    if endpoint_mode == "async":
        path = f"/v1/tenants/{tenant_id}/async-decision-executions"
        success_status = 201
    else:
        path = f"/v1/tenants/{tenant_id}/decisions"
        success_status = 200
    started = time.perf_counter()
    started_at = now_iso()
    try:
        response = await client.post(path, json=body)
        latency_ms = round((time.perf_counter() - started) * 1_000, 2)
        response_body: Any
        try:
            response_body = response.json()
        except json.JSONDecodeError:
            response_body = response.text[:1_000]
        result = response_body.get("result") if isinstance(response_body, dict) else None
        decision = result.get("decision") if isinstance(result, dict) else None
        async_execution = response_body.get("async_decision_execution") if isinstance(response_body, dict) else None
        completed_inline = bool(response_body.get("completed_inline")) if isinstance(response_body, dict) else False
        if not isinstance(result, dict):
            result = {}
        if not isinstance(decision, dict):
            decision = {}
        if not isinstance(async_execution, dict):
            async_execution = {}
        request_item = body["items"][0] if endpoint_mode == "async" else body
        return {
            "index": index,
            "level_index": level_index,
            "concurrency": level,
            "endpoint_mode": endpoint_mode,
            "ok": response.status_code == success_status,
            "status_code": response.status_code,
            "request_started_at": started_at,
            "response_received_at": now_iso(),
            "latency_ms": latency_ms,
            "object_id": request_item["object_id"],
            "request_key": request_key,
            "decision_id": decision.get("id") or result.get("id"),
            "outcome": decision.get("outcome") or result.get("outcome"),
            "score": decision.get("score") or result.get("score"),
            "triggered": result.get("triggered") if "triggered" in result else decision.get("triggered"),
            "completed_inline": completed_inline,
            "deferred": response.status_code == 202 or (response.status_code == 201 and not completed_inline),
            "async_execution_id": async_execution.get("id"),
            "async_execution_status": async_execution.get("status"),
            "callback_url": body.get("callback_url"),
            "response": response_body if response.status_code != success_status else None,
        }
    except httpx.HTTPError as exc:
        request_item = body["items"][0] if endpoint_mode == "async" else body
        return {
            "index": index,
            "level_index": level_index,
            "concurrency": level,
            "endpoint_mode": endpoint_mode,
            "ok": False,
            "status_code": None,
            "request_started_at": started_at,
            "response_received_at": now_iso(),
            "latency_ms": round((time.perf_counter() - started) * 1_000, 2),
            "object_id": request_item["object_id"],
            "request_key": request_key,
            "decision_id": None,
            "outcome": None,
            "score": None,
            "triggered": None,
            "completed_inline": False,
            "deferred": False,
            "async_execution_id": None,
            "async_execution_status": None,
            "callback_url": body.get("callback_url"),
            "error": str(exc),
        }


async def run_level(
    args: argparse.Namespace,
    base_fields: dict[str, Any],
    client: httpx.AsyncClient,
    output_path: Path | None,
    level: int,
    start_index: int,
) -> list[dict[str, Any]]:
    semaphore = asyncio.Semaphore(level)

    async def submit_with_limit(level_index: int) -> dict[str, Any]:
        index = start_index + level_index - 1
        request_key, body = build_request(args, base_fields, index)
        if args.dry_run:
            return {
                "index": index,
                "level_index": level_index,
                "concurrency": level,
                "endpoint_mode": args.endpoint_mode,
                "ok": True,
                "dry_run": True,
                "object_id": body["items"][0]["object_id"] if args.endpoint_mode == "async" else body["object_id"],
                "request_key": request_key,
                "request": body,
            }
        async with semaphore:
            return await submit_one(
                client,
                args.endpoint_mode,
                args.tenant_id,
                index,
                level,
                level_index,
                request_key,
                body,
            )

    print(f"starting concurrency={level}: {args.count} requests", flush=True)
    started = time.perf_counter()
    results: list[dict[str, Any]] = []
    tasks = [asyncio.create_task(submit_with_limit(level_index)) for level_index in range(1, args.count + 1)]
    for completed_count, task in enumerate(asyncio.as_completed(tasks), start=1):
        result = await task
        results.append(result)
        if output_path:
            with output_path.open("a", encoding="utf-8") as handle:
                handle.write(json.dumps(result, sort_keys=True) + "\n")
        if args.progress_every > 0 and completed_count % args.progress_every == 0:
            print(f"concurrency={level}: completed {completed_count} / {args.count}", flush=True)

    elapsed_seconds = time.perf_counter() - started
    summary = {
        "concurrency": level,
        "count": args.count,
        **summarize_results(results, elapsed_seconds),
    }
    print(json.dumps(summary, sort_keys=True), flush=True)
    return results


async def run(args: argparse.Namespace) -> int:
    if args.count <= 0:
        raise ValueError("--count must be positive")
    if not args.tenant_id.strip():
        raise ValueError("--tenant-id is required")
    if not args.scenario_id.strip():
        raise ValueError("--scenario-id is required")
    if args.wait_timeout_ms < 0:
        raise ValueError("--wait-timeout-ms must be greater than or equal to zero")
    if args.progress_every < 0:
        raise ValueError("--progress-every must be greater than or equal to zero")

    concurrency_levels = parse_concurrency_levels(args.concurrency_levels)
    base_fields = load_base_fields(args)
    headers = {"Authorization": f"Bearer {args.auth_token}"} if args.auth_token else {}
    timeout = httpx.Timeout(args.timeout)
    output_path = Path(args.output).expanduser().resolve() if args.output else None
    if output_path:
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text("", encoding="utf-8")

    results: list[dict[str, Any]] = []
    overall_started = time.perf_counter()
    async with httpx.AsyncClient(
        base_url=args.decision_engine_url.rstrip("/"),
        headers=headers,
        timeout=timeout,
    ) as client:
        start_index = 1
        for level in concurrency_levels:
            level_results = await run_level(args, base_fields, client, output_path, level, start_index)
            results.extend(level_results)
            start_index += args.count
    overall_elapsed = time.perf_counter() - overall_started

    success_status = 201 if args.endpoint_mode == "async" else 200
    created = sum(1 for result in results if result.get("status_code") == success_status)
    completed_inline = sum(1 for result in results if result.get("completed_inline") is True)
    failures = sum(1 for result in results if not result.get("ok"))
    by_concurrency: list[dict[str, Any]] = []
    for level in concurrency_levels:
        level_results = [result for result in results if result.get("concurrency") == level]
        by_concurrency.append({"concurrency": level, **summarize_results(level_results)})
    combined_summary = {
        "decision_engine_url": args.decision_engine_url,
        "tenant_id": args.tenant_id,
        "scenario_id": args.scenario_id or None,
        "endpoint_mode": args.endpoint_mode,
        "count_per_concurrency": args.count,
        "concurrency_levels": concurrency_levels,
        "total_requests": len(results),
        "created": created,
        "completed_inline": completed_inline,
        "failures": failures,
        "callback_url": args.callback_url if args.endpoint_mode == "async" and args.callback_url else None,
        "wait_timeout_ms": args.wait_timeout_ms,
        "combined": summarize_results(results, overall_elapsed),
        "by_concurrency": by_concurrency,
        "output": str(output_path) if output_path else None,
    }
    print(json.dumps({"combined_summary": combined_summary}, indent=2, sort_keys=True))
    return 0 if failures == 0 else 2


def main() -> None:
    parser = argparse.ArgumentParser(description="Create many decision requests from one transaction template")
    parser.add_argument("--decision-engine-url", default=default_decision_engine_url())
    parser.add_argument("--tenant-id", default=env_default("NEXT_PUBLIC_DATA_MODEL_TENANT_ID", DEFAULT_TENANT_ID))
    parser.add_argument("--scenario-id", default=env_default("SCENARIO_ID", DEFAULT_SCENARIO_ID))
    parser.add_argument("--endpoint-mode", choices=("sync", "async"), default="sync")
    parser.add_argument("--count", type=int, default=100)
    parser.add_argument("--concurrency-levels", default=env_default("DECISION_CREATE_CONCURRENCY_LEVELS", DEFAULT_CONCURRENCY_LEVELS))
    parser.add_argument("--progress-every", type=int, default=100)
    parser.add_argument("--auth-token", default=os.getenv("SERVICE_AUTH_TOKEN", ""))
    parser.add_argument("--timeout", type=float, default=30.0)
    parser.add_argument("--wait-timeout-ms", type=int, default=int(env_default("ASYNC_WAIT_TIMEOUT_MS", "0")))
    parser.add_argument("--callback-url", default=env_default("ASYNC_CALLBACK_URL", ""))
    parser.add_argument("--amount", type=float)
    parser.add_argument("--event-date", default=env_default("DECISION_CREATE_EVENT_DATE", DEFAULT_EVENT_DATE))
    parser.add_argument("--raw-timestamp", default=env_default("DECISION_CREATE_RAW_TIMESTAMP", DEFAULT_RAW_TIMESTAMP))
    parser.add_argument("--base-fields", help="JSON object merged into the default transaction fields")
    parser.add_argument("--base-fields-file", help="Path to a JSON object merged into the default transaction fields")
    parser.add_argument("--object-prefix", default="manual-sync-txn")
    parser.add_argument("--source-transaction-prefix", default="manual-source-txn")
    parser.add_argument("--request-key-prefix", default="manual-sync-decision")
    parser.add_argument("--run-id", default="")
    parser.add_argument("--output", default="/tmp/fraud-decision-create-results.ndjson")
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    if not args.run_id:
        args.run_id = default_run_id()
    raise SystemExit(asyncio.run(run(args)))


if __name__ == "__main__":
    main()
