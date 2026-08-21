from __future__ import annotations

import unittest
from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

from production_replay.setup_environment import EnvironmentSetup, FieldSpec


class SetupEnvironmentProjectionTests(unittest.IsolatedAsyncioTestCase):
    async def test_operational_upgrade_creates_projection_only_after_event_conversion(self) -> None:
        class FakeClients:
            def __init__(self) -> None:
                self.data_model = object()
                self.is_event = False
                self.date_created = False
                self.operations: list[str] = []

            async def request(
                self,
                _client: object,
                method: str,
                path: str,
                _expected: int,
                **kwargs: object,
            ) -> dict[str, object]:
                payload = kwargs.get("json", {})
                if method == "GET" and path.endswith("/tables"):
                    return {
                        "tables": [
                            {
                                "id": "table-1",
                                "name": "transactions",
                                "storage_class": "operational",
                                "event_time_field": "",
                            }
                        ]
                    }
                if method == "GET" and path == "/v1/tables/table-1/fields":
                    return {"fields": []}
                if method == "POST" and path == "/v1/tables/table-1/fields":
                    field = dict(payload)  # type: ignore[arg-type]
                    if field["name"] == "date":
                        self.date_created = True
                        self.operations.append("create-date")
                    if field["name"] == "account_ref":
                        self.assert_projection_after_conversion(field)
                        self.operations.append("create-projection")
                    return {"field": field}
                if method == "PATCH" and path == "/v1/tables/table-1":
                    if not self.date_created:
                        raise AssertionError("event-time field must exist before conversion")
                    self.is_event = True
                    self.operations.append("convert-to-event")
                    return {
                        "table": {
                            "id": "table-1",
                            "name": "transactions",
                            "storage_class": "event",
                            "event_time_field": "date",
                        }
                    }
                if method == "GET" and path.endswith("/data-model"):
                    return {}
                raise AssertionError(f"unexpected request: {method} {path}")

            def assert_projection_after_conversion(self, field: dict[str, object]) -> None:
                if not self.is_event:
                    raise AssertionError("projection field was created before event conversion")
                if field.get("is_projection") is not True:
                    raise AssertionError("projection field did not carry is_projection=true")
                if field.get("aggregation_mode") != "adaptive_cache":
                    raise AssertionError("projection field did not carry its aggregation policy")

        clients = FakeClients()
        setup = EnvironmentSetup(SimpleNamespace(), clients, "tenant-1", "Replay")  # type: ignore[arg-type]
        setup._ensure_links = AsyncMock()  # type: ignore[method-assign]
        fields = {
            "transactions": (
                FieldSpec("date", "timestamp", False),
                FieldSpec("account_ref", "string", projection=True, aggregation_mode="adaptive_cache"),
            )
        }

        with patch("production_replay.setup_environment.TABLE_FIELDS", fields):
            await setup._ensure_model()

        self.assertEqual(
            clients.operations,
            ["create-date", "convert-to-event", "create-projection"],
        )


if __name__ == "__main__":
    unittest.main()
