from __future__ import annotations

import asyncio
import json
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from typing import Any
from unittest.mock import patch

import httpx

from production_replay.benchmark_observation import (
    RuntimeObserver, async_main, capture_environment, endpoint_identity, project_metrics,
)


class ObservationTests(unittest.IsolatedAsyncioTestCase):
    async def test_samples_services_without_storing_tokens_strings_or_payloads(self) -> None:
        requests = []

        async def handler(request: httpx.Request) -> httpx.Response:
            requests.append(request)
            envelope = "runtime_metrics" if "runtime" in request.url.path else "read_metrics"
            return httpx.Response(200, json={envelope: {
                "db_pool": {"AcquiredConns": 3, "password": "SECRET"},
                "pressure": {"Status": "SECRET", "load": 25},
                "endpoints": {"SECRET": {"requests": 5}},
                "payload": {"name": "SECRET"},
            }})

        with TemporaryDirectory() as directory:
            output = Path(directory) / "metrics.ndjson"
            async with RuntimeObserver(output, {"decision": "http://decision", "ingestion": "http://ingestion"},
                                       auth_token="SECRET", transport=httpx.MockTransport(handler)) as observer:
                await observer.sample()
                self.assertTrue(observer.summary()["valid"])
            raw = output.read_text()
            self.assertNotIn("SECRET", raw)
            sample = json.loads(raw)
            self.assertEqual(sample["services"]["decision"]["metrics"]["db_pool"]["AcquiredConns"], 3)
        self.assertEqual(len(requests), 2)
        self.assertEqual(requests[0].headers["Authorization"], "Bearer SECRET")

    async def test_failed_remote_capture_is_observable_and_not_valid(self) -> None:
        async def handler(request: httpx.Request) -> httpx.Response:
            return httpx.Response(503, text="SECRET")

        with TemporaryDirectory() as directory:
            output = Path(directory) / "metrics.ndjson"
            async with RuntimeObserver(output, {"decision": "http://decision"},
                                       transport=httpx.MockTransport(handler)) as observer:
                await observer.sample()
                self.assertFalse(observer.summary()["valid"])
                self.assertEqual(observer.summary()["incomplete_samples"], 1)
            self.assertNotIn("SECRET", output.read_text())
            self.assertEqual(json.loads(output.read_text())["services"]["decision"]["http_status"], 503)

    async def test_invalid_metrics_shape_is_an_error_not_zero_metrics(self) -> None:
        async def handler(request: httpx.Request) -> httpx.Response:
            return httpx.Response(200, json={"runtime_metrics": {}})

        with TemporaryDirectory() as directory:
            async with RuntimeObserver(Path(directory) / "metrics.ndjson", {"decision": "http://decision"},
                                       transport=httpx.MockTransport(handler)) as observer:
                await observer.sample()
                self.assertFalse(observer.summary()["valid"])

    async def test_remote_deadline_is_bounded(self) -> None:
        async def handler(request: httpx.Request) -> httpx.Response:
            await asyncio.Event().wait()
            raise AssertionError("unreachable")

        with TemporaryDirectory() as directory:
            async with RuntimeObserver(Path(directory) / "metrics.ndjson", {"decision": "http://decision"},
                                       timeout=0.05, transport=httpx.MockTransport(handler)) as observer:
                await asyncio.wait_for(observer.sample(), 1.0)
                self.assertFalse(observer.summary()["valid"])

    async def test_periodic_samples_and_final_sample_accompany_workload(self) -> None:
        async def workload() -> int:
            await asyncio.sleep(0.15)
            return 42

        with TemporaryDirectory() as directory:
            async with RuntimeObserver(Path(directory) / "metrics.ndjson", {}, interval=0.02) as observer:
                value = await observer.run_during(workload)
                self.assertEqual(value, 42)
                self.assertGreaterEqual(observer.samples, 3)

    async def test_workload_exception_does_not_leave_observer_running(self) -> None:
        async def workload() -> None:
            raise RuntimeError("broken workload")

        before = set(asyncio.all_tasks())
        with TemporaryDirectory() as directory:
            async with RuntimeObserver(Path(directory) / "metrics.ndjson", {}) as observer:
                with self.assertRaisesRegex(RuntimeError, "broken workload"):
                    await observer.run_during(workload)
        self.assertEqual(set(asyncio.all_tasks()) - before, set())

    async def test_standalone_capture_is_read_only_and_refuses_existing_destination(self) -> None:
        with TemporaryDirectory() as directory:
            output = Path(directory) / "capture"
            code = await async_main(["--output", str(output), "--samples", "1"])
            self.assertEqual(code, 0)
            summary = json.loads((output / "observation-summary.json").read_text())
            self.assertEqual(summary["status"], "completed")
            self.assertEqual(summary["coverage"], ["observer_host", "observer_process"])
            environment = json.loads((output / "environment.json").read_text())
            self.assertIn("not_remote", environment["scope"])
            with self.assertRaises(FileExistsError):
                await async_main(["--output", str(output), "--samples", "1"])


class ObservationContractTests(unittest.TestCase):
    def test_environment_endpoint_identity_omits_credentials_and_query(self) -> None:
        self.assertEqual(endpoint_identity("https://user:SECRET@example.com:8443/private?token=SECRET"),
                         "https://example.com:8443")
        self.assertEqual(endpoint_identity("http://[::1]:8080"), "http://[::1]:8080")

    def test_nonfinite_values_and_strings_are_not_metrics(self) -> None:
        value = project_metrics({"runtime_metrics": {"db_pool": {
            "count": 1, "password": "SECRET", "invalid": float("nan"),
        }}}, "runtime_metrics")
        self.assertEqual(value, {"db_pool": {"count": 1}})
        with self.assertRaises(ValueError):
            project_metrics({"runtime_metrics": {"db_pool": None}}, "runtime_metrics")

    def test_environment_contains_code_identity_but_not_raw_git_status(self) -> None:
        with TemporaryDirectory() as directory:
            with patch("production_replay.benchmark_observation._git", side_effect=[
                {"available": True, "value": " M private-file"},
                {"available": True, "value": "revision"},
            ]):
                value = capture_environment(Path(directory), Path(directory), {})
        self.assertTrue(value["working_tree"]["dirty"])
        self.assertEqual(value["working_tree"]["changed_entries"], 1)
        self.assertEqual(value["git_revision"]["value"], "revision")
        self.assertIn("database_scale_suite.py", value["harness_sha256"])
        self.assertNotIn("private-file", json.dumps(value))
