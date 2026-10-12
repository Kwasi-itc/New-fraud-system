"""Kill an outage controller and verify independent restoration of a disposable fixture."""
from __future__ import annotations

import argparse
import asyncio
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import uuid

from production_replay.benchmark_reporting import write_json_atomic
from .postgres_pilot import docker, verify_container
from .worker_outage import DockerWorker, run_outage


async def controller(identity: str, project: str, journal: Path):
    trigger = asyncio.Event()
    trigger.set()
    await run_outage(DockerWorker(identity, project), journal, trigger, duration=10)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--controller-id")
    parser.add_argument("--project")
    args = parser.parse_args()
    if args.controller_id:
        asyncio.run(controller(args.controller_id, args.project, args.output / "fault.json"))
        return
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    project = "system-tests-watchdog-" + uuid.uuid4().hex[:12]
    name = project + "-worker"
    identity = child = None
    state = {"passed": False, "cleanup_verified": False, "project": project,
             "scope": "controller_hard_kill_restoration_not_business_processing"}
    write_json_atomic(output / "summary.json", state)
    try:
        # Reuse PostgreSQL's image only as a lightweight inert worker fixture.
        # No database starts, host ports or persistent mounts are created.
        identity = docker("run", "--detach", "--name", name, "--memory", "64m", "--cpus", "0.25",
            "--label", "system-tests.disposable=true", "--label", f"system-tests.project={project}",
            "--label", f"com.docker.compose.project={project}", "--label", "com.docker.compose.service=watchdog-worker",
            "--entrypoint", "sleep", "postgres:16", "infinity", timeout=240)
        verify_container(identity, name)
        worker = DockerWorker(identity, project)
        child = subprocess.Popen([sys.executable, "-m", "system_tests.watchdog_pilot", "--output", str(output),
            "--controller-id", identity, "--project", project], cwd=Path(__file__).resolve().parents[1],
            stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0)
        journal = output / "fault.json"
        deadline = time.monotonic() + 60
        while True:
            if child.poll() is not None or time.monotonic() >= deadline:
                raise RuntimeError("controller_did_not_apply_fault")
            if journal.exists() and journal.stat().st_size:
                lease = json.loads(journal.read_text())
                if lease["status"] == "active":
                    break
            time.sleep(0.1)
        if worker.inspect()["Running"]:
            raise RuntimeError("fixture_was_not_stopped")
        child.kill()
        child.wait(timeout=10)
        state.update(controller_killed=True, controller_exit_code=child.returncode)
        write_json_atomic(output / "summary.json", state)
        print("Controller killed while fixture stopped; waiting for independent lease restoration", flush=True)
        # Wait beyond the recorded lease, bounded even if wall time changes.
        deadline = time.monotonic() + 135
        evidence_path = journal.with_suffix(".watchdog.json")
        while time.monotonic() < deadline:
            evidence = json.loads(evidence_path.read_text())
            if evidence["status"] == "restored":
                if not worker.inspect()["Running"]:
                    raise RuntimeError("watchdog_did_not_restore_running_state")
                state.update(passed=True, watchdog=evidence)
                break
            if evidence["status"] == "restore_failed":
                raise RuntimeError("watchdog_restore_failed")
            time.sleep(0.25)
        if not state["passed"]:
            raise TimeoutError("independent_restoration_timeout")
    except BaseException as exc:
        state.update(passed=False, error_type=type(exc).__name__)
        raise
    finally:
        if child is not None and child.poll() is None:
            child.kill()
            child.wait(timeout=10)
        if identity:
            try:
                # Disarm any still-running watchdog before removing its verified fixture.
                journal = output / "fault.json"
                if journal.exists() and journal.stat().st_size:
                    lease = json.loads(journal.read_text())
                    DockerWorker(identity, project).restore()
                    lease["status"] = "restored"
                    write_json_atomic(journal, lease)
                    watchdog_deadline = time.monotonic() + 35
                    evidence_path = journal.with_suffix(".watchdog.json")
                    while time.monotonic() < watchdog_deadline:
                        if evidence_path.exists() and json.loads(evidence_path.read_text())["status"] in {"restored", "restore_failed"}:
                            break
                        time.sleep(0.25)
                    else:
                        raise TimeoutError("watchdog_cleanup_not_confirmed")
                verify_container(identity, name)
                docker("rm", "--force", identity)
                state["cleanup_verified"] = True
            except BaseException as exc:
                state.update(passed=False, cleanup_error_type=type(exc).__name__)
        write_json_atomic(output / "summary.json", state)
    if not state["passed"]:
        raise SystemExit(1)
    print("Independent watchdog restored fixture after controller kill; fixture removed", flush=True)


if __name__ == "__main__":
    main()
