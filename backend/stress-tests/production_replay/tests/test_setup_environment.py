from __future__ import annotations

import unittest
from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

from production_replay.api_client import APIError
from production_replay.cli import _verify_replay_tenant
from production_replay.scenarios import build_portable_scenarios
from production_replay.setup_environment import EnvironmentSetup


class BucketClients:
    def __init__(self, list_responses: list[list[dict[str, object]]]) -> None:
        self.data_model = object()
        self.list_responses = list(list_responses)
        self.requests: list[tuple[str, str, dict[str, object]]] = []

    async def request(
        self,
        _client: object,
        method: str,
        path: str,
        _expected_codes: object,
        **kwargs: object,
    ) -> dict[str, object]:
        self.requests.append((method, path, kwargs))
        if method == "GET":
            if not self.list_responses:
                raise AssertionError("unexpected logical bucket list request")
            return {"logical_buckets": self.list_responses.pop(0)}
        if method == "POST":
            return {
                "logical_bucket": bucket_definition(
                    status="pending_index", timezone_name="Africa/Accra"
                )
            }
        raise AssertionError(f"unexpected request: {method} {path}")


class ReplayVerificationClients:
    def __init__(
        self,
        manifest: SimpleNamespace,
        logical_buckets: list[dict[str, object]],
    ) -> None:
        self.data_model = object()
        self.decision_engine = object()
        self.manifest = manifest
        self.logical_buckets = logical_buckets

    async def request(
        self,
        client: object,
        _method: str,
        _path: str,
        _expected_codes: object,
        **_kwargs: object,
    ) -> dict[str, object]:
        if client is self.data_model:
            return {
                "data_model": {
                    "tables": {"transactions": {"id": "table-1"}},
                    "logical_bucket_definitions": self.logical_buckets,
                }
            }
        return {
            "scenarios": [
                {"name": item.name, "live_iteration_id": f"live-{index}"}
                for index, item in enumerate(
                    build_portable_scenarios(self.manifest), start=1
                )
            ]
        }


def bucket_definition(
    *,
    status: str,
    timezone_name: str,
) -> dict[str, object]:
    return {
        "id": "bucket-1",
        "tenant_id": "tenant-1",
        "table_id": "table-1",
        "timestamp_field_id": "field-1",
        "timestamp_field_name": "date",
        "grain": "daily",
        "timezone": timezone_name,
        "seal_delay_seconds": 172800,
        "definition_version": 1,
        "status": status,
        "index_job_id": "job-1",
        "cache_eligible_at": "2026-07-30T12:00:00Z" if status == "active" else None,
    }


def environment(clients: BucketClients) -> EnvironmentSetup:
    setup = EnvironmentSetup(
        SimpleNamespace(timezone="Africa/Accra"),
        clients,
        "tenant-1",
        "Replay",
    )
    setup.tables = {"transactions": {"id": "table-1"}}
    setup.fields = {"transactions": {"date": {"id": "field-1"}}}
    return setup


class SetupEnvironmentTests(unittest.IsolatedAsyncioTestCase):
    @patch("production_replay.setup_environment.asyncio.sleep", new_callable=AsyncMock)
    async def test_creates_and_waits_for_daily_transaction_bucket(
        self, _sleep: AsyncMock
    ) -> None:
        clients = BucketClients(
            [
                [],
                [bucket_definition(status="pending_index", timezone_name="Africa/Accra")],
                [bucket_definition(status="activating", timezone_name="Africa/Accra")],
                [bucket_definition(status="active", timezone_name="Africa/Accra")],
            ]
        )

        result = await environment(clients)._ensure_logical_buckets(5)

        self.assertEqual(result["transactions.date"]["status"], "active")
        create_requests = [item for item in clients.requests if item[0] == "POST"]
        self.assertEqual(len(create_requests), 1)
        self.assertEqual(
            create_requests[0][2]["json"],
            {
                "timestamp_field_id": "field-1",
                "timezone": "Africa/Accra",
            },
        )

    async def test_reuses_an_active_matching_bucket(self) -> None:
        active = bucket_definition(status="active", timezone_name="Africa/Accra")
        clients = BucketClients([[active], [active]])

        result = await environment(clients)._ensure_logical_buckets(5)

        self.assertEqual(result["transactions.date"]["id"], "bucket-1")
        self.assertEqual([item for item in clients.requests if item[0] == "POST"], [])

    async def test_rejects_an_existing_bucket_with_a_different_timezone(self) -> None:
        clients = BucketClients(
            [[bucket_definition(status="active", timezone_name="UTC")]]
        )

        with self.assertRaisesRegex(APIError, "Retire the existing definition"):
            await environment(clients)._ensure_logical_buckets(5)

    async def test_replay_preflight_requires_active_matching_bucket(self) -> None:
        manifest = SimpleNamespace(
            timezone="Africa/Accra",
            transaction_streams=(
                SimpleNamespace(channel="wallet", system_type="wallet_transfer"),
            ),
        )
        clients = ReplayVerificationClients(manifest, [])

        with self.assertRaisesRegex(APIError, "active daily transactions.date"):
            await _verify_replay_tenant(clients, manifest, "tenant-1")

    async def test_replay_preflight_returns_active_bucket(self) -> None:
        manifest = SimpleNamespace(
            timezone="Africa/Accra",
            transaction_streams=(
                SimpleNamespace(channel="wallet", system_type="wallet_transfer"),
            ),
        )
        active = bucket_definition(status="active", timezone_name="Africa/Accra")
        clients = ReplayVerificationClients(manifest, [active])

        result = await _verify_replay_tenant(clients, manifest, "tenant-1")

        self.assertEqual(result["id"], "bucket-1")


if __name__ == "__main__":
    unittest.main()
