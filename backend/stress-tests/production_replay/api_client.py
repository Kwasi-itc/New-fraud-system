from __future__ import annotations

import asyncio
import json
import time
from dataclasses import dataclass
from typing import Any

import httpx


class APIError(RuntimeError):
    def __init__(self, message: str, *, status_code: int | None = None) -> None:
        super().__init__(message)
        self.status_code = status_code


@dataclass(frozen=True)
class ServiceConfig:
    data_model_url: str
    ingestion_url: str
    decision_engine_url: str
    auth_token: str | None = None
    timeout_seconds: float = 30.0
    max_connections: int = 500


class ServiceClients:
    def __init__(self, config: ServiceConfig) -> None:
        headers = {"Authorization": f"Bearer {config.auth_token}"} if config.auth_token else {}
        timeout = httpx.Timeout(config.timeout_seconds)
        limits = httpx.Limits(
            max_connections=max(config.max_connections, 20),
            max_keepalive_connections=max(min(config.max_connections, 200), 20),
        )
        self.data_model = httpx.AsyncClient(
            base_url=config.data_model_url.rstrip("/"), headers=headers, timeout=timeout, limits=limits
        )
        self.ingestion = httpx.AsyncClient(
            base_url=config.ingestion_url.rstrip("/"), headers=headers, timeout=timeout, limits=limits
        )
        self.decision_engine = httpx.AsyncClient(
            base_url=config.decision_engine_url.rstrip("/"), headers=headers, timeout=timeout, limits=limits
        )

    async def close(self) -> None:
        await asyncio.gather(self.data_model.aclose(), self.ingestion.aclose(), self.decision_engine.aclose())

    async def __aenter__(self) -> "ServiceClients":
        return self

    async def __aexit__(self, *_args: object) -> None:
        await self.close()

    async def wait_until_ready(self, timeout_seconds: float = 60.0) -> None:
        await asyncio.gather(
            self._wait_for(self.data_model, "data-model", timeout_seconds),
            self._wait_for(self.ingestion, "ingestion", timeout_seconds),
            self._wait_for(self.decision_engine, "decision-engine", timeout_seconds),
        )

    async def _wait_for(self, client: httpx.AsyncClient, name: str, timeout_seconds: float) -> None:
        deadline = time.monotonic() + timeout_seconds
        detail = "not attempted"
        while time.monotonic() < deadline:
            try:
                response = await client.get("/readyz")
                if response.status_code == 200:
                    return
                detail = f"HTTP {response.status_code}"
            except httpx.HTTPError as exc:
                detail = str(exc)
            await asyncio.sleep(0.5)
        raise APIError(f"{name} was not ready within {timeout_seconds:g}s: {detail}")

    async def request(
        self,
        client: httpx.AsyncClient,
        method: str,
        path: str,
        expected: int | set[int] | tuple[int, ...],
        **kwargs: Any,
    ) -> dict[str, Any]:
        try:
            response = await client.request(method, path, **kwargs)
        except httpx.HTTPError as exc:
            raise APIError(f"{method} {path} failed: {exc}") from exc
        expected_codes = {expected} if isinstance(expected, int) else set(expected)
        if response.status_code not in expected_codes:
            detail = _response_detail(response)
            raise APIError(
                f"{method} {response.request.url} returned {response.status_code}, expected {sorted(expected_codes)}: {detail}",
                status_code=response.status_code,
            )
        if not response.content:
            return {}
        try:
            value = response.json()
        except json.JSONDecodeError as exc:
            raise APIError(f"{method} {response.request.url} returned invalid JSON") from exc
        if not isinstance(value, dict):
            raise APIError(f"{method} {response.request.url} returned a non-object JSON response")
        return value

    async def ingest_batch(
        self,
        tenant_id: str,
        object_type: str,
        records: list[dict[str, Any]],
        idempotency_key: str,
        max_attempts: int = 3,
    ) -> dict[str, Any]:
        if len(records) > 500:
            raise ValueError("ingestion batches cannot exceed 500 records")
        path = f"/v1/tenants/{tenant_id}/ingest/{object_type}/batch"
        last_error: APIError | None = None
        for attempt in range(1, max_attempts + 1):
            try:
                return await self.request(
                    self.ingestion,
                    "POST",
                    path,
                    200,
                    json=records,
                    headers={"Idempotency-Key": idempotency_key},
                )
            except APIError as exc:
                last_error = exc
                if attempt == max_attempts or (exc.status_code is not None and exc.status_code not in {429, 500, 502, 503, 504}):
                    raise
                await asyncio.sleep(0.25 * (2 ** (attempt - 1)))
        assert last_error is not None
        raise last_error

    async def ingest_one(
        self,
        tenant_id: str,
        object_type: str,
        fields: dict[str, Any],
        idempotency_key: str,
        max_attempts: int = 3,
    ) -> tuple[dict[str, Any], int]:
        path = f"/v1/tenants/{tenant_id}/ingest/{object_type}"
        last_error: APIError | None = None
        for attempt in range(1, max_attempts + 1):
            try:
                response = await self.request(
                    self.ingestion,
                    "POST",
                    path,
                    200,
                    json=fields,
                    headers={"Idempotency-Key": idempotency_key},
                )
                return response, attempt
            except APIError as exc:
                last_error = exc
                if attempt == max_attempts or (exc.status_code is not None and exc.status_code not in {429, 500, 502, 503, 504}):
                    raise
                await asyncio.sleep(0.1 * (2 ** (attempt - 1)))
        assert last_error is not None
        raise last_error

    async def decide_once(self, tenant_id: str, object_id: str, fields: dict[str, Any]) -> dict[str, Any]:
        return await self.request(
            self.decision_engine,
            "POST",
            f"/v1/tenants/{tenant_id}/ingestion-events/record-ingested",
            200,
            json={"object_id": object_id, "object_type": "transactions", "fields": fields, "source": "production_replay"},
        )

    async def create_async_decision_execution(
        self,
        tenant_id: str,
        object_id: str,
        fields: dict[str, Any],
        idempotency_key: str,
        wait_timeout_ms: int = 0,
        callback_url: str = "",
    ) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "object_type": "transactions",
            "idempotency_key": idempotency_key,
            "wait_timeout_ms": wait_timeout_ms,
            "items": [
                {
                    "object_id": object_id,
                    "object_type": "transactions",
                    "fields": fields,
                }
            ],
        }
        if callback_url:
            payload["callback_url"] = callback_url
        return await self.request(
            self.decision_engine,
            "POST",
            f"/v1/tenants/{tenant_id}/async-decision-executions",
            201,
            json=payload,
        )


def _response_detail(response: httpx.Response) -> str:
    text = response.text.replace("\n", " ").strip()
    return text[:1_000] + ("..." if len(text) > 1_000 else "")
