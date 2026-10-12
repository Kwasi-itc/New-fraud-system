"""Streaming, disk-indexed preflight for finite transaction cohorts."""
from __future__ import annotations

import hashlib
import json
from collections.abc import Iterator
from pathlib import Path
from tempfile import TemporaryDirectory
from typing import Any

from production_replay.identity_ledger import IdentityLedger


def _pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate_json_key")
        result[key] = value
    return result


def _constant(_value):
    raise ValueError("nonfinite_json_value")


def strict_json_loads(value: str | bytes):
    return json.loads(value, object_pairs_hook=_pairs, parse_constant=_constant)


def read_inputs(path: Path) -> Iterator[dict[str, Any]]:
    with path.open("rb") as handle:
        while line := handle.readline(1_048_577):
            if len(line) > 1_048_576:
                raise ValueError("input_row_too_large")
            value = strict_json_loads(line)
            if (not isinstance(value, dict) or not isinstance(value.get("object_id"), str)
                    or not value["object_id"].strip()):
                raise ValueError("input_requires_object_id")
            yield value


def fingerprint_record(value: dict[str, Any]) -> bytes:
    return json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=True,
                      allow_nan=False).encode() + b"\n"


class InputSource:
    def __init__(self, path: Path, count: int) -> None:
        if count <= 0:
            raise ValueError("input_count_must_be_positive")
        self.path, self.count = path, count
        self.observed = 0
        self.digest = hashlib.sha256()
        self.iterator = read_inputs(path)

    def __iter__(self):
        return self

    def __next__(self):
        if self.observed >= self.count:
            raise StopIteration
        value = next(self.iterator)
        self.digest.update(fingerprint_record(value))
        self.observed += 1
        return value

    def close(self):
        self.iterator.close()

    def evidence(self):
        return {"records": self.observed, "canonical_sha256": self.digest.hexdigest()}


def inspect_source(path: Path, count: int, *, tenant: str = "preflight",
                   ledger: IdentityLedger | None = None) -> dict[str, Any]:
    """Validate the scheduled prefix, optionally sharing uniqueness across runs."""
    if ledger is None:
        with TemporaryDirectory(prefix="system-test-input-") as directory:
            with IdentityLedger(Path(directory) / "ids.sqlite3") as temporary:
                return inspect_source(path, count, tenant=tenant, ledger=temporary)
    source = InputSource(path, count)
    try:
        for _ in range(count):
            try:
                value = next(source)
            except StopIteration as exc:
                raise ValueError("input_exhausted_before_schedule_end") from exc
            ledger.register(tenant, value["object_id"])
        return source.evidence()
    finally:
        source.close()
