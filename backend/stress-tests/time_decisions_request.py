from __future__ import annotations

import argparse
import time
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode
from urllib.request import Request, urlopen


DEFAULT_BASE_URL = (
    "http://54.246.247.31:8082/v1/tenants/"
    "e83b4edf-570d-46af-9ff5-c189729a0897/decisions"
)
DEFAULT_TOTAL_RECORDS = 1_780_000
BATCH_SIZE = 100
PAGE_COUNT = 10


def page_offsets(total_records: int) -> list[tuple[str, int, int]]:
    total_pages = (total_records + BATCH_SIZE - 1) // BATCH_SIZE
    first_pages = range(min(PAGE_COUNT, total_pages))
    last_start = max(PAGE_COUNT, total_pages - PAGE_COUNT)
    last_pages = range(last_start, total_pages)

    pages = [("first", page + 1, page * BATCH_SIZE) for page in first_pages]
    pages.extend(("last", page + 1, page * BATCH_SIZE) for page in last_pages)
    return pages


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Time the first 10 and last 10 decision pages in batches of 100."
    )
    parser.add_argument("--base-url", default=DEFAULT_BASE_URL)
    parser.add_argument(
        "--total-records",
        type=int,
        default=DEFAULT_TOTAL_RECORDS,
        help="Total decision count used to calculate the final 10 page offsets.",
    )
    parser.add_argument("--token", help="Optional bearer token.")
    args = parser.parse_args()

    if args.total_records <= 0:
        parser.error("--total-records must be positive")

    headers = {"Accept": "application/json"}
    if args.token:
        headers["Authorization"] = f"Bearer {args.token}"

    failures = 0
    for group, page, offset in page_offsets(args.total_records):
        url = f"{args.base_url}?{urlencode({'limit': BATCH_SIZE, 'offset': offset})}"
        request = Request(url, headers=headers, method="GET")
        started_at = time.perf_counter()

        try:
            with urlopen(request) as response:
                body = response.read()
                elapsed_ms = (time.perf_counter() - started_at) * 1_000
                print(
                    f"group={group} page={page} offset={offset} "
                    f"status={response.status} latency_ms={elapsed_ms:.2f} "
                    f"response_bytes={len(body)}"
                )
        except HTTPError as error:
            elapsed_ms = (time.perf_counter() - started_at) * 1_000
            body = error.read()
            failures += 1
            print(
                f"group={group} page={page} offset={offset} "
                f"status={error.code} latency_ms={elapsed_ms:.2f} "
                f"response_bytes={len(body)}"
            )
        except (TimeoutError, URLError) as error:
            elapsed_ms = (time.perf_counter() - started_at) * 1_000
            failures += 1
            print(
                f"group={group} page={page} offset={offset} "
                f"error={error} latency_ms={elapsed_ms:.2f}"
            )

    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
