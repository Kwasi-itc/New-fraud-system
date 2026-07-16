from __future__ import annotations

import unittest

import httpx

from production_replay.api_client import ServiceClients, ServiceConfig


class APIClientTests(unittest.IsolatedAsyncioTestCase):
    async def test_ingestion_retry_reuses_the_same_idempotency_key(self) -> None:
        attempts = 0
        observed_keys: list[str | None] = []

        async def handler(request: httpx.Request) -> httpx.Response:
            nonlocal attempts
            attempts += 1
            observed_keys.append(request.headers.get("Idempotency-Key"))
            if attempts == 1:
                return httpx.Response(503, json={"error": {"code": "busy", "message": "busy"}})
            return httpx.Response(200, json={"result": {"object_id": "tx", "action": "created", "revision_id": "r1"}})

        clients = ServiceClients(ServiceConfig("http://data", "http://ingestion", "http://decision"))
        await clients.ingestion.aclose()
        clients.ingestion = httpx.AsyncClient(base_url="http://ingestion", transport=httpx.MockTransport(handler))
        try:
            _response, used_attempts = await clients.ingest_one(
                "tenant", "transactions", {"object_id": "tx"}, "stable-key", max_attempts=3
            )
        finally:
            await clients.close()

        self.assertEqual(used_attempts, 2)
        self.assertEqual(observed_keys, ["stable-key", "stable-key"])


if __name__ == "__main__":
    unittest.main()
