from __future__ import annotations

import argparse
import json
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


def parse_iso(value: str) -> datetime:
    return datetime.fromisoformat(value.replace("Z", "+00:00")).astimezone(timezone.utc)


def read_ndjson(path: Path) -> list[dict[str, Any]]:
    if not path.exists():
        return []
    rows: list[dict[str, Any]] = []
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip():
            continue
        value = json.loads(line)
        if isinstance(value, dict):
            rows.append(value)
    return rows


def callback_execution_id(record: dict[str, Any]) -> str:
    body = record.get("body")
    if isinstance(body, dict) and body.get("execution_id"):
        return str(body["execution_id"])
    return str(record.get("execution_id_header") or "")


def build_report(submissions: list[dict[str, Any]], callbacks: list[dict[str, Any]]) -> dict[str, Any]:
    callback_by_id = {
        callback_execution_id(record): record
        for record in callbacks
        if callback_execution_id(record)
    }
    inline = [row for row in submissions if bool(row.get("completed_inline"))]
    deferred = [row for row in submissions if not bool(row.get("completed_inline"))]
    callback_timings: list[dict[str, Any]] = []
    missing: list[dict[str, Any]] = []

    for row in deferred:
        execution_id = str(row.get("execution_id") or "")
        callback = callback_by_id.get(execution_id)
        if not execution_id or callback is None:
            missing.append(
                {
                    "execution_id": execution_id,
                    "object_id": row.get("object_id"),
                    "status_at_submit_response": row.get("status"),
                }
            )
            continue
        request_started_at = parse_iso(str(row["request_started_at"]))
        response_received_at = parse_iso(str(row["response_received_at"]))
        callback_received_at = parse_iso(str(callback["received_at"]))
        body = callback.get("body") if isinstance(callback.get("body"), dict) else {}
        callback_timings.append(
            {
                "execution_id": execution_id,
                "object_id": row.get("object_id"),
                "stream_id": row.get("stream_id"),
                "event": body.get("event") if isinstance(body, dict) else None,
                "status": body.get("status") if isinstance(body, dict) else None,
                "request_started_at": row["request_started_at"],
                "accepted_at": row["response_received_at"],
                "callback_received_at": callback["received_at"],
                "request_to_callback_ms": round((callback_received_at - request_started_at).total_seconds() * 1_000, 2),
                "accepted_to_callback_ms": round((callback_received_at - response_received_at).total_seconds() * 1_000, 2),
            }
        )

    return {
        "status": "complete" if not missing else "incomplete",
        "total_async_endpoint_submissions": len(submissions),
        "completed_inline": len(inline),
        "deferred_async": len(deferred),
        "callbacks_received_total": len(callback_by_id),
        "deferred_callbacks_received": len(callback_timings),
        "deferred_callbacks_missing": len(missing),
        "callback_timings": callback_timings,
        "missing_callbacks": missing,
    }


def main() -> None:
    parser = argparse.ArgumentParser(description="Correlate async decision submissions with callback receipts")
    parser.add_argument("--submissions", required=True, type=Path)
    parser.add_argument("--callbacks", required=True, type=Path)
    parser.add_argument("--summary", required=True, type=Path)
    parser.add_argument("--wait-timeout", type=float, default=120.0)
    parser.add_argument("--poll-interval", type=float, default=1.0)
    args = parser.parse_args()

    deadline = time.monotonic() + args.wait_timeout
    report: dict[str, Any] = {}
    while True:
        submissions = read_ndjson(args.submissions)
        callbacks = read_ndjson(args.callbacks)
        report = build_report(submissions, callbacks)
        if report["deferred_callbacks_missing"] == 0:
            break
        if time.monotonic() >= deadline:
            break
        time.sleep(args.poll_interval)

    args.summary.parent.mkdir(parents=True, exist_ok=True)
    args.summary.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    compact = {
        "status": report["status"],
        "completed_inline": report["completed_inline"],
        "deferred_async": report["deferred_async"],
        "deferred_callbacks_received": report["deferred_callbacks_received"],
        "deferred_callbacks_missing": report["deferred_callbacks_missing"],
    }
    print(json.dumps(compact, indent=2, sort_keys=True))
    raise SystemExit(0 if report["status"] == "complete" else 2)


if __name__ == "__main__":
    main()
