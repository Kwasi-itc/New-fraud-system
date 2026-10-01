"""Atomic, privacy-minimal progress artifacts for long benchmark runs."""
from __future__ import annotations

import asyncio
import json
import os
import tempfile
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


def write_json_atomic(path: Path, value: Any) -> None:
    # A sibling temporary file keeps replacement on the same filesystem.
    temporary: Path | None = None
    try:
        with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", dir=path.parent, delete=False) as handle:
            temporary = Path(handle.name)
            json.dump(value, handle, indent=2, sort_keys=True, default=str, allow_nan=False)
            handle.write("\n")
        os.replace(temporary, path)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def _now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


class RunReport:
    def __init__(self, output: Path) -> None:
        self.output = output
        self.state: dict[str, Any] = {
            "schema_version": 2,
            "status": "running",
            "stage": "preparing",
            "started_at": _now(),
            "phases": [],
            "acceptance": {"passed": False, "evaluated": False},
            "scope": "database_volume_regression_not_production_qualification",
        }
        self.phase: dict[str, Any] | None = None

    def __enter__(self) -> RunReport:
        self.save()
        return self

    def __exit__(self, exc_type: Any, exc: BaseException | None, traceback: Any) -> None:
        if exc is not None:
            self.state["status"] = (
                "interrupted" if isinstance(exc, (KeyboardInterrupt, asyncio.CancelledError)) else "failed"
            )
            # Exception messages can contain response payloads, PII, or credential-bearing commands.
            self.state["error"] = {"type": type(exc).__name__}
            self.state["acceptance"] = {"passed": False, "evaluated": False}
            if self.phase is not None:
                self.phase["status"] = self.state["status"]
            self.state["finished_at"] = _now()
            self.save()

    def save(self) -> None:
        self.state["updated_at"] = _now()
        self.state["current_phase"] = self.phase
        if self.phase is not None:
            write_json_atomic(self.output / f"{self.phase['phase']}.json", self.phase)
        write_json_atomic(self.output / "summary.json", self.state)

    def stage(self, name: str) -> None:
        self.state["stage"] = name
        if self.phase is not None:
            self.phase["stage"] = name
        self.save()

    def start_phase(self, name: str, description: str) -> None:
        self.phase = {"phase": name, "description": description, "status": "running"}
        self.stage("database_reset")

    def update_phase(self, **values: Any) -> None:
        if self.phase is None:
            raise RuntimeError("no active benchmark phase")
        self.phase.update(values)
        self.save()

    def finish_phase(self, result: dict[str, Any]) -> None:
        completed = {**result, "status": "completed"}
        write_json_atomic(self.output / f"{result['phase']}.json", completed)
        self.state["phases"].append(completed)
        self.phase = None
        self.save()

    def finish(self, acceptance: dict[str, Any]) -> None:
        self.state.update(status="completed", stage="finished", completed_at=_now(), acceptance=acceptance)
        self.save()
