"""Scoped Docker worker outage with a durable restoration lease and independent watchdog."""
from __future__ import annotations

import argparse
import asyncio
import json
import math
import os
import re
import subprocess
import sys
import time
from pathlib import Path

from production_replay.benchmark_reporting import write_json_atomic


class DockerWorker:
    def __init__(self, identity: str, project: str) -> None:
        if not re.fullmatch(r"[a-f0-9]{64}", identity) or not re.fullmatch(r"system-tests-[a-z0-9-]{1,60}", project):
            raise ValueError("invalid_disposable_worker_identity")
        self.identity, self.project = identity, project

    def command(self, *args):
        result = subprocess.run(["docker", *args], capture_output=True, text=True, timeout=15)
        if result.returncode:
            raise RuntimeError("worker_control_failed")
        return result.stdout

    def inspect(self):
        data = json.loads(self.command("inspect", self.identity))[0]
        labels = data["Config"]["Labels"]
        if (data["Id"] != self.identity or labels.get("system-tests.disposable") != "true"
                or labels.get("system-tests.project") != self.project
                or labels.get("com.docker.compose.project") != self.project
                or not labels.get("com.docker.compose.service", "").endswith("-worker")):
            raise ValueError("worker_target_not_in_disposable_project")
        return data["State"]

    def stop(self):
        if not self.inspect()["Running"]:
            raise ValueError("worker_was_not_running")
        self.command("stop", "--time", "2", self.identity)
        if self.inspect()["Running"]:
            raise RuntimeError("worker_stop_not_verified")

    def restore(self):
        if not self.inspect()["Running"]:
            self.command("start", self.identity)
        if not self.inspect()["Running"]:
            raise RuntimeError("worker_restore_not_verified")


def read_lease(path):
    state = json.loads(path.read_text())
    if (state.get("schema_version") != 1 or type(state.get("expires_at")) not in (int, float)
            or not math.isfinite(state["expires_at"])):
        raise ValueError("invalid_restoration_lease")
    DockerWorker(state["container_id"], state["project"])
    return state


def watchdog(path: Path):
    state = read_lease(path)
    worker = DockerWorker(state["container_id"], state["project"])
    worker.inspect()
    evidence = path.with_suffix(".watchdog.json")
    write_json_atomic(evidence, {"status": "ready", "pid": os.getpid()})
    while True:
        state = read_lease(path)
        if state["status"] == "restored" or time.time() >= state["expires_at"]:
            try:
                worker.restore()
                write_json_atomic(evidence, {"status": "restored", "verified_at": time.time()})
                return
            except Exception as exc:
                write_json_atomic(evidence, {"status": "restore_failed", "error_type": type(exc).__name__})
                raise
        time.sleep(0.25)


async def bounded_control(operation):
    # Wait for any in-progress Docker command before handling cancellation/restoration.
    task = asyncio.create_task(asyncio.to_thread(operation))
    try:
        return await asyncio.shield(task)
    except asyncio.CancelledError:
        await task
        raise


async def run_outage(worker: DockerWorker, journal: Path, trigger: asyncio.Event, *, duration: float,
                     trigger_timeout: float = 300) -> dict:
    if not math.isfinite(duration) or not 0 < duration <= 300 or not math.isfinite(trigger_timeout) or trigger_timeout <= 0:
        raise ValueError("invalid_outage_duration")
    await asyncio.wait_for(trigger.wait(), trigger_timeout)
    if not (await bounded_control(worker.inspect))["Running"]:
        raise ValueError("worker_not_running_before_fault")
    with journal.open("x"):
        pass
    state = {"schema_version": 1, "status": "armed", "container_id": worker.identity, "project": worker.project,
             # Arming can take 10s; stop's inspect/stop/inspect can take 45s.
             # Expiry must not race a still-pending stop command.
             "duration": duration, "expires_at": time.time() + duration + 90,
             "applied": False, "completed_duration": False, "restored": False, "passed": False}
    write_json_atomic(journal, state)
    child = subprocess.Popen([sys.executable, "-m", "system_tests.worker_outage", "--watchdog", str(journal.resolve())],
        cwd=Path(__file__).resolve().parents[1], stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL, creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0)
    try:
        ready = journal.with_suffix(".watchdog.json")
        deadline = time.monotonic() + 10
        while not ready.exists():
            if child.poll() is not None or time.monotonic() > deadline:
                raise RuntimeError("watchdog_failed_to_arm")
            await asyncio.sleep(0.05)
        if json.loads(ready.read_text())["status"] != "ready":
            raise RuntimeError("watchdog_not_ready")
        state.update(status="applying", apply_started_at=time.time())
        write_json_atomic(journal, state)
        await bounded_control(worker.stop)
        state.update(status="active", applied=True, stopped_at=time.time())
        write_json_atomic(journal, state)
        await asyncio.sleep(duration)
        state["completed_duration"] = True
    finally:
        try:
            await bounded_control(worker.restore)
            state.update(status="restored", restored=True, restored_at=time.time())
        except BaseException as exc:
            state.update(status="restore_failed", restore_error_type=type(exc).__name__)
        write_json_atomic(journal, state)
        # A failed parent restoration leaves the independently armed watchdog running.
        if state["restored"]:
            try:
                code = await asyncio.to_thread(child.wait, 20)
                state["watchdog_exit_code"] = code
                evidence = json.loads(journal.with_suffix(".watchdog.json").read_text())
                state["passed"] = state["applied"] and state["completed_duration"] and code == 0 and evidence["status"] == "restored"
            except (OSError, ValueError, subprocess.TimeoutExpired) as exc:
                state["watchdog_error_type"] = type(exc).__name__
            write_json_atomic(journal, state)
    if not state["passed"]:
        raise RuntimeError("worker_outage_or_restoration_not_verified")
    return state


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--watchdog", type=Path, required=True)
    watchdog(parser.parse_args().watchdog)
