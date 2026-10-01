from __future__ import annotations

import asyncio
import json
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import AsyncMock, MagicMock, patch

from production_replay.benchmark_observation import RuntimeObserver
from production_replay.postgres_observation import PostgresObserver, observation_sql


class PostgresTests(unittest.IsolatedAsyncioTestCase):
    async def test_cancellation_reaps_process_and_propagates(self) -> None:
        process = MagicMock(returncode=None)
        started = asyncio.Event()
        calls = 0

        async def communicate():
            nonlocal calls
            calls += 1
            if calls == 1:
                started.set()
                await asyncio.Event().wait()
            return b"", b""

        process.communicate = communicate
        with patch("asyncio.create_subprocess_exec", AsyncMock(return_value=process)):
            task = asyncio.create_task(PostgresObserver(["psql"], {}, 5)())
            await started.wait()
            task.cancel()
            with self.assertRaises(asyncio.CancelledError):
                await task
        process.kill.assert_called_once()
        self.assertEqual(calls, 2)

    async def test_read_only_bounded_command_and_no_secret_artifacts(self) -> None:
        process = MagicMock(returncode=0)
        process.communicate = AsyncMock(return_value=(json.dumps({"database": {}, "river": []}).encode(), b""))
        observer = PostgresObserver(["psql", "-d", "test"], {"PGPASSWORD": "secret"}, 2)
        with patch("asyncio.create_subprocess_exec", AsyncMock(return_value=process)) as start:
            result = await observer()
        self.assertEqual(result["status"], "ok")
        self.assertNotIn("secret", json.dumps(result))
        self.assertIn("default_transaction_read_only=on", start.call_args.kwargs["env"]["PGOPTIONS"])
        self.assertIn("-X", start.call_args.args)
        self.assertIn("ON_ERROR_STOP=1", start.call_args.args)
        self.assertEqual(observation_sql().count("LIMIT 1001"), 5)
        self.assertNotIn("payload", observation_sql())

    async def test_psql_failure_hides_stderr_and_marks_capture_invalid(self) -> None:
        process = MagicMock(returncode=1)
        process.communicate = AsyncMock(return_value=(b"", b"password=secret"))
        with patch("asyncio.create_subprocess_exec", AsyncMock(return_value=process)):
            result = await PostgresObserver(["psql"], {}, 1)()
        self.assertEqual(result["status"], "error")
        self.assertNotIn("secret", json.dumps(result))
        with TemporaryDirectory() as directory:
            async with RuntimeObserver(Path(directory) / "metrics", {}, database=AsyncMock(return_value=result)) as observer:
                await observer.sample()
                self.assertFalse(observer.summary()["valid"])
                self.assertIn("postgresql", observer.summary()["coverage"])

    async def test_timeout_kills_and_reaps_process(self) -> None:
        process = MagicMock(returncode=None)
        calls = 0

        async def communicate():
            nonlocal calls
            calls += 1
            if calls == 1:
                await asyncio.Event().wait()
            return b"", b""

        process.communicate = communicate
        with patch("asyncio.create_subprocess_exec", AsyncMock(return_value=process)):
            result = await PostgresObserver(["psql"], {}, 0.02)()
        self.assertEqual(result["error_type"], "TimeoutError")
        process.kill.assert_called_once()
        self.assertEqual(calls, 2)


if __name__ == "__main__":
    unittest.main()
