"""Local benchmark artifact storage checks; unrelated to PostgreSQL capacity."""
from __future__ import annotations

import shutil
import time
from pathlib import Path
from typing import Any


DEFAULT_MIN_FREE_DISK_MIB = 1024


class LocalStorageError(RuntimeError):
    """Only controlled, credential/identity-free details belong in this error."""

    def __init__(self, message: str, details: dict[str, Any]) -> None:
        super().__init__(message)
        self.details = details


class DiskSpaceGuard:
    """Reserve reporting headroom, not a prediction of total run storage.

    Each owner uses its own guard (source-preparation thread or event loop).
    Once exhausted, the guard remains failed: a run must not resume silently.
    """

    def __init__(self, path: Path, minimum_bytes: int, interval: float = 1.0) -> None:
        if minimum_bytes <= 0 or interval <= 0:
            raise ValueError("disk guard minimum and interval must be positive")
        self.path = path
        self.minimum_bytes = minimum_bytes
        self.interval = interval
        self.next_check = 0.0
        self.failure: LocalStorageError | None = None

    def check(self, *, force: bool = False) -> None:
        if self.failure is not None:
            raise self.failure
        now = time.monotonic()
        if not force and now < self.next_check:
            return
        usage = shutil.disk_usage(self.path)
        self.next_check = now + self.interval
        if usage.free < self.minimum_bytes:
            self.failure = LocalStorageError(
                "insufficient_local_disk_space: benchmark artifact filesystem has "
                f"{usage.free // (1024 * 1024)} MiB free; minimum is "
                f"{self.minimum_bytes // (1024 * 1024)} MiB. Free space or expand "
                "the filesystem before starting a new run; this is not RDS storage.",
                {"category": "insufficient_local_disk_space", "free_bytes": usage.free,
                 "minimum_free_bytes": self.minimum_bytes, "total_bytes": usage.total},
            )
            raise self.failure
