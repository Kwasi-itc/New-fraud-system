#!/usr/bin/env python3
"""Capture decision-engine runtime metrics and ingestion read metrics snapshots.

This script is intended to be used before, during, or after replay/stress runs so
the resulting JSON artifacts can be compared across read modes and incident-posture
settings.
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


def utc_now_stamp() -> str:
    return datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S-%fZ")


def fetch_json(url: str, timeout_seconds: float) -> dict[str, Any]:
    request = Request(url, headers={"Accept": "application/json"})
    with urlopen(request, timeout=timeout_seconds) as response:
        payload = response.read().decode("utf-8")
    data = json.loads(payload)
    if not isinstance(data, dict):
        raise ValueError(f"expected object payload from {url}, got {type(data).__name__}")
    return data


def write_json(path: Path, payload: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Capture /v1/admin/runtime-metrics and /v1/admin/read-metrics snapshots."
    )
    parser.add_argument(
        "--decision-engine-url",
        default="http://127.0.0.1:8082",
        help="Base URL for decision-engine-service.",
    )
    parser.add_argument(
        "--ingestion-url",
        default="http://127.0.0.1:8081",
        help="Base URL for ingestion-service.",
    )
    parser.add_argument(
        "--output-dir",
        default=str(Path("stress-tests") / "metrics-captures"),
        help="Directory for captured artifacts.",
    )
    parser.add_argument(
        "--label",
        default="snapshot",
        help="Short label embedded in output filenames and manifest.",
    )
    parser.add_argument(
        "--samples",
        type=int,
        default=1,
        help="Number of samples to capture. Use >1 with --interval-seconds for time-series capture.",
    )
    parser.add_argument(
        "--interval-seconds",
        type=float,
        default=0.0,
        help="Sleep interval between samples when --samples > 1.",
    )
    parser.add_argument(
        "--timeout-seconds",
        type=float,
        default=10.0,
        help="HTTP timeout per metrics request.",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.samples <= 0:
        raise SystemExit("--samples must be greater than zero")
    if args.interval_seconds < 0:
        raise SystemExit("--interval-seconds must be greater than or equal to zero")
    if args.timeout_seconds <= 0:
        raise SystemExit("--timeout-seconds must be greater than zero")

    output_dir = Path(args.output_dir)
    capture_id = f"{args.label}-{utc_now_stamp()}"
    capture_dir = output_dir / capture_id
    runtime_url = args.decision_engine_url.rstrip("/") + "/v1/admin/runtime-metrics"
    read_url = args.ingestion_url.rstrip("/") + "/v1/admin/read-metrics"

    samples: list[dict[str, Any]] = []

    for index in range(args.samples):
        captured_at = datetime.now(timezone.utc).isoformat()
        runtime_payload = fetch_json(runtime_url, args.timeout_seconds)
        read_payload = fetch_json(read_url, args.timeout_seconds)

        runtime_path = capture_dir / f"runtime-metrics-{index + 1:03d}.json"
        read_path = capture_dir / f"read-metrics-{index + 1:03d}.json"

        write_json(runtime_path, runtime_payload)
        write_json(read_path, read_payload)

        sample = {
            "sample_index": index + 1,
            "captured_at": captured_at,
            "runtime_metrics_path": str(runtime_path).replace("\\", "/"),
            "read_metrics_path": str(read_path).replace("\\", "/"),
        }
        samples.append(sample)
        print(
            f"[{index + 1}/{args.samples}] captured runtime/read metrics at {captured_at}",
            file=sys.stderr,
        )

        if index + 1 < args.samples and args.interval_seconds > 0:
            time.sleep(args.interval_seconds)

    manifest = {
        "capture_id": capture_id,
        "label": args.label,
        "decision_engine_url": args.decision_engine_url,
        "ingestion_url": args.ingestion_url,
        "runtime_metrics_endpoint": runtime_url,
        "read_metrics_endpoint": read_url,
        "samples": samples,
    }
    manifest_path = capture_dir / "capture-manifest.json"
    write_json(manifest_path, manifest)
    print(str(manifest_path).replace("\\", "/"))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (HTTPError, URLError, TimeoutError, ValueError) as exc:
        print(f"capture failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
