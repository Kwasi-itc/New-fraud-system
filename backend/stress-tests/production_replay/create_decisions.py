from __future__ import annotations

import argparse
import json
import os
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import httpx


DEFAULT_EVENT_DATE = "2026-06-01T01:05:00Z"
DEFAULT_RAW_TIMESTAMP = "2026-06-01 01:05:00"

DEFAULT_FIELDS: dict[str, Any] = {
    "amount": 1200.0,
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
    return request_key, body


def submit_one(
    client: httpx.Client,
    tenant_id: str,
    index: int,
    request_key: str,
    body: dict[str, Any],
) -> dict[str, Any]:
    path = f"/v1/tenants/{tenant_id}/async-decision-executions"
    started = time.perf_counter()
    started_at = now_iso()
    try:
        response = client.post(path, json=body)
        latency_ms = round((time.perf_counter() - started) * 1_000, 2)
        response_body: Any
        try:
            response_body = response.json()
        except json.JSONDecodeError:
            response_body = response.text[:1_000]
        result = response_body.get("result") if isinstance(response_body, dict) else None
        async_execution = response_body.get("async_decision_execution") if isinstance(response_body, dict) else None
        completed_inline = bool(response_body.get("completed_inline")) if isinstance(response_body, dict) else False
        if not isinstance(result, dict):
            result = {}
        if not isinstance(async_execution, dict):
            async_execution = {}
        request_item = body["items"][0]
        return {
            "index": index,
            "ok": response.status_code == 201,
            "status_code": response.status_code,
            "request_started_at": started_at,
            "response_received_at": now_iso(),
            "latency_ms": latency_ms,
            "object_id": request_item["object_id"],
            "request_key": request_key,
            "decision_id": result.get("id"),
            "outcome": result.get("outcome"),
            "score": result.get("score"),
            "triggered": result.get("triggered"),
            "completed_inline": completed_inline,
            "deferred": response.status_code == 201 and not completed_inline,
            "async_execution_id": async_execution.get("id"),
            "async_execution_status": async_execution.get("status"),
            "callback_url": body.get("callback_url"),
            "response": response_body if response.status_code != 201 else None,
        }
    except httpx.HTTPError as exc:
        request_item = body["items"][0]
        return {
            "index": index,
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


def run(args: argparse.Namespace) -> int:
    if args.count <= 0:
        raise ValueError("--count must be positive")
    if not args.tenant_id.strip():
        raise ValueError("--tenant-id is required")
    if not args.scenario_id.strip():
        raise ValueError("--scenario-id is required for async decision executions")
    if args.wait_timeout_ms < 0:
        raise ValueError("--wait-timeout-ms must be greater than or equal to zero")
    if args.progress_every < 0:
        raise ValueError("--progress-every must be greater than or equal to zero")

    base_fields = load_base_fields(args)
    headers = {"Authorization": f"Bearer {args.auth_token}"} if args.auth_token else {}
    timeout = httpx.Timeout(args.timeout)
    output_path = Path(args.output).expanduser().resolve() if args.output else None
    if output_path:
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text("", encoding="utf-8")

    results: list[dict[str, Any]] = []
    with httpx.Client(
        base_url=args.decision_engine_url.rstrip("/"),
        headers=headers,
        timeout=timeout,
    ) as client:
        for index in range(1, args.count + 1):
            request_key, body = build_request(args, base_fields, index)
            if args.dry_run:
                result = {
                    "index": index,
                    "ok": True,
                    "dry_run": True,
                    "object_id": body["items"][0]["object_id"],
                    "request_key": request_key,
                    "request": body,
                }
            else:
                result = submit_one(client, args.tenant_id, index, request_key, body)
            results.append(result)
            if output_path:
                with output_path.open("a", encoding="utf-8") as handle:
                    handle.write(json.dumps(result, sort_keys=True) + "\n")
            if args.progress_every > 0 and index % args.progress_every == 0:
                print(f"submitted {index} / {args.count}", flush=True)

    created = sum(1 for result in results if result.get("status_code") == 201)
    completed_inline = sum(1 for result in results if result.get("completed_inline") is True)
    deferred = sum(1 for result in results if result.get("deferred") is True)
    failures = sum(1 for result in results if not result.get("ok"))
    summary = {
        "decision_engine_url": args.decision_engine_url,
        "tenant_id": args.tenant_id,
        "scenario_id": args.scenario_id or None,
        "count": args.count,
        "created": created,
        "completed_inline": completed_inline,
        "failures": failures,
        "deferred": deferred,
        "callback_url": args.callback_url or None,
        "wait_timeout_ms": args.wait_timeout_ms,
        "output": str(output_path) if output_path else None,
    }
    print(json.dumps(summary, indent=2, sort_keys=True))
    return 0 if failures == 0 else 2


def main() -> None:
    parser = argparse.ArgumentParser(description="Create many async decision executions from one transaction template")
    parser.add_argument("--decision-engine-url", default=os.getenv("DECISION_ENGINE_URL", "http://127.0.0.1:8082"))
    parser.add_argument("--tenant-id", required=True)
    parser.add_argument("--scenario-id", default="")
    parser.add_argument("--count", type=int, required=True)
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
    parser.add_argument("--object-prefix", default="manual-async-txn")
    parser.add_argument("--source-transaction-prefix", default="manual-source-txn")
    parser.add_argument("--request-key-prefix", default="manual-async-decision")
    parser.add_argument("--run-id", default="")
    parser.add_argument("--output", default="/tmp/fraud-decision-create-results.ndjson")
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    if not args.run_id:
        args.run_id = default_run_id()
    raise SystemExit(run(args))


if __name__ == "__main__":
    main()
