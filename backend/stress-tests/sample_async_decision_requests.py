from __future__ import annotations

import argparse
import json
import time
import uuid
from datetime import datetime, timezone
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


DEFAULT_URL = (
    "http://54.246.247.31:8082/v1/tenants/"
    "e83b4edf-570d-46af-9ff5-c189729a0897/async-decision-executions"
)
SCENARIO_ID = "2308ed9c-6fa7-4f95-bb92-bfff88f6fbe8"


def sample_transaction(sequence: int) -> tuple[str, dict[str, object]]:
    suffix = uuid.uuid4().hex
    object_id = f"async-sample:{suffix}"
    occurred_at = datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
    fields: dict[str, object] = {
        "object_id": object_id,
        "transaction_id": f"ASYNC-{sequence:04d}-{suffix[:12]}",
        "date": occurred_at,
        "amount": 1250.50 + sequence,
        "fees": 2.50,
        "currency": "GHS",
        "country": "GH",
        "channel": "wallet",
        "direction": "outgoing",
        "system_type": "wallet_transfer",
        "stream_id": "async-sample",
        "processor": "sample-client",
        "transaction_type": "wallet_transfer",
        "payment_type": "wallet",
        "merchant_id": f"SAMPLE-MERCHANT-{sequence:04d}",
        "product_id": f"SAMPLE-PRODUCT-{sequence:04d}",
        "account_ref": f"SAMPLE-ACCOUNT-{sequence:04d}",
        "account_name": "Async Sample Customer",
        "payment_msisdn": "233200000000",
        "narration": "Sample asynchronous decision request",
        "source_id": "async-sample-script",
        "source_trans_id": f"ASYNC-{suffix}",
        "raw_timestamp": occurred_at,
        "source_file": "sample_async_decision_requests.py",
    }
    return object_id, fields


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Submit sample asynchronous decision requests."
    )
    parser.add_argument("--url", default=DEFAULT_URL)
    parser.add_argument("--count", type=int, default=1)
    parser.add_argument("--wait-timeout-ms", type=int, default=0)
    parser.add_argument("--token", help="Optional bearer token.")
    args = parser.parse_args()

    if args.count <= 0:
        parser.error("--count must be positive")
    if args.wait_timeout_ms < 0:
        parser.error("--wait-timeout-ms cannot be negative")

    headers = {
        "Accept": "application/json",
        "Content-Type": "application/json",
    }
    if args.token:
        headers["Authorization"] = f"Bearer {args.token}"

    failures = 0
    for sequence in range(1, args.count + 1):
        object_id, fields = sample_transaction(sequence)
        payload = {
            "scenario_id": SCENARIO_ID,
            "object_type": "transactions",
            "idempotency_key": f"async-sample:{uuid.uuid4()}",
            "wait_timeout_ms": args.wait_timeout_ms,
            "items": [
                {
                    "object_id": object_id,
                    "object_type": "transactions",
                    "fields": fields,
                }
            ],
        }
        request = Request(
            args.url,
            data=json.dumps(payload).encode("utf-8"),
            headers=headers,
            method="POST",
        )
        started_at = time.perf_counter()

        try:
            with urlopen(request) as response:
                response_body = json.loads(response.read())
                elapsed_ms = (time.perf_counter() - started_at) * 1_000
                print(
                    json.dumps(
                        {
                            "request": sequence,
                            "object_id": object_id,
                            "http_status": response.status,
                            "latency_ms": round(elapsed_ms, 2),
                            "response": response_body,
                        },
                        indent=2,
                    )
                )
        except HTTPError as error:
            failures += 1
            elapsed_ms = (time.perf_counter() - started_at) * 1_000
            print(
                json.dumps(
                    {
                        "request": sequence,
                        "object_id": object_id,
                        "http_status": error.code,
                        "latency_ms": round(elapsed_ms, 2),
                        "error": error.read().decode("utf-8", errors="replace"),
                    },
                    indent=2,
                )
            )
        except (TimeoutError, URLError) as error:
            failures += 1
            elapsed_ms = (time.perf_counter() - started_at) * 1_000
            print(
                json.dumps(
                    {
                        "request": sequence,
                        "object_id": object_id,
                        "latency_ms": round(elapsed_ms, 2),
                        "error": str(error),
                    },
                    indent=2,
                )
            )

    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
