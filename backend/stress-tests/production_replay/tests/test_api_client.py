from __future__ import annotations

import json
import unittest

import httpx

from production_replay.api_client import APIError, ServiceClients, ServiceConfig


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

    async def test_request_reports_exception_type_when_httpx_message_is_empty(self) -> None:
        async def handler(request: httpx.Request) -> httpx.Response:
            raise httpx.ReadTimeout("", request=request)

        clients = ServiceClients(ServiceConfig("http://data", "http://ingestion", "http://decision"))
        await clients.decision_engine.aclose()
        clients.decision_engine = httpx.AsyncClient(base_url="http://decision", transport=httpx.MockTransport(handler))
        try:
            with self.assertRaises(APIError) as ctx:
                await clients.request(clients.decision_engine, "POST", "/v1/test", 200, json={"ok": True})
        finally:
            await clients.close()

        self.assertIn("ReadTimeout", str(ctx.exception))
        self.assertIn("POST /v1/test failed:", str(ctx.exception))

    async def test_record_ingested_sends_request_mode_and_async_options(self) -> None:
        observed_request: dict[str, object] = {}

        async def handler(request: httpx.Request) -> httpx.Response:
            observed_request["path"] = request.url.path
            observed_request["body"] = json.loads(request.content)
            return httpx.Response(202, json={"deferred": True, "async_decision_execution": {"id": "execution-1"}})

        clients = ServiceClients(ServiceConfig("http://data", "http://ingestion", "http://decision"))
        await clients.decision_engine.aclose()
        clients.decision_engine = httpx.AsyncClient(base_url="http://decision", transport=httpx.MockTransport(handler))
        try:
            response, status_code, payload = await clients.record_ingested(
                "tenant",
                "tx-1",
                {"amount": 12500, "currency": "USD"},
                mode="async",
                wait_timeout_ms=300,
                callback_url="https://api.example.com/webhooks/decision-results",
            )
        finally:
            await clients.close()

        expected_payload = {
            "object_id": "tx-1",
            "object_type": "transactions",
            "mode": "async",
            "fields": {"amount": 12500, "currency": "USD"},
            "wait_timeout_ms": 300,
            "callback_url": "https://api.example.com/webhooks/decision-results",
            "source": "production_replay",
        }
        self.assertEqual(observed_request["path"], "/v1/tenants/tenant/ingestion-events/record-ingested")
        self.assertEqual(observed_request["body"], expected_payload)
        self.assertEqual(payload, expected_payload)
        self.assertEqual(status_code, 202)
        self.assertTrue(response["deferred"])


if __name__ == "__main__":
    unittest.main()
