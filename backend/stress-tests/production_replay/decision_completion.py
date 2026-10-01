"""Validate completion evidence without persisting sensitive response bodies."""
from __future__ import annotations

import asyncio
import math
from typing import Any

from .api_client import APIError, ServiceClients


class DecisionCompletionError(APIError):
    def __init__(self, category: str, *, unresolved: bool = True) -> None:
        super().__init__(category)
        self.category = category
        self.unresolved = unresolved


def _validate_result(
    result: Any,
    tenant_id: str,
    object_id: str,
    expected_scenarios: int | None,
    *,
    async_result: bool = False,
) -> tuple[str, ...]:
    if not isinstance(result, dict) or result.get("object_id") != object_id:
        raise DecisionCompletionError("invalid_decision_result")
    results = result.get("results")
    if not isinstance(results, list) or (expected_scenarios is not None and len(results) != expected_scenarios):
        raise DecisionCompletionError("invalid_scenario_results")
    decision_ids = []
    for item in results:
        if not isinstance(item, dict) or not isinstance(item.get("triggered"), bool):
            raise DecisionCompletionError("invalid_scenario_result")
        if item["triggered"]:
            decision = item.get("decision")
            # Async result_body serializes the domain Decision; sync uses the HTTP DTO.
            id_key, tenant_key, object_key = (
                ("ID", "TenantID", "ObjectID") if async_result else ("id", "tenant_id", "object_id")
            )
            if (
                not isinstance(decision, dict)
                or not isinstance(decision.get(id_key), str)
                or not decision[id_key]
                or decision.get(tenant_key) != tenant_id
                or decision.get(object_key) != object_id
            ):
                raise DecisionCompletionError("invalid_decision_identity")
            decision_ids.append(decision[id_key])
    if len(set(decision_ids)) != len(decision_ids):
        raise DecisionCompletionError("duplicate_decision_identity")
    return tuple(decision_ids)


async def verify_decision_completion(
    clients: ServiceClients,
    tenant_id: str,
    object_id: str,
    response: dict[str, Any],
    status_code: int,
    *,
    allow_deferred: bool = False,
    timeout_seconds: float = 60.0,
    poll_interval_seconds: float = 0.5,
    expected_scenarios: int | None = None,
) -> tuple[str, ...]:
    if any(not math.isfinite(value) or value <= 0 for value in (timeout_seconds, poll_interval_seconds)):
        raise ValueError("decision observation deadlines must be finite and positive")
    if expected_scenarios is not None and expected_scenarios <= 0:
        raise ValueError("expected scenario count must be positive")
    deferred = status_code == 202 or response.get("deferred") is True
    if not deferred:
        if status_code != 200:
            raise DecisionCompletionError("unexpected_decision_status")
        return _validate_result(response.get("result"), tenant_id, object_id, expected_scenarios)
    if not allow_deferred:
        raise DecisionCompletionError("deferred_rejected")
    execution = response.get("async_decision_execution")
    if not isinstance(execution, dict) or not isinstance(execution.get("id"), str) or not execution["id"]:
        raise DecisionCompletionError("missing_execution_id")
    execution_id = execution["id"]

    async def follow() -> tuple[str, ...]:
        current = execution
        while True:
            if (
                current.get("id") != execution_id
                or current.get("tenant_id") != tenant_id
                or current.get("object_type") != "transactions"
            ):
                raise DecisionCompletionError("invalid_execution_identity")
            status = current.get("status")
            if status == "completed":
                return _validate_result(
                    current.get("result_body"), tenant_id, object_id, expected_scenarios, async_result=True
                )
            if status == "failed":
                raise DecisionCompletionError("async_execution_failed", unresolved=False)
            if status not in {"pending", "queued", "running"}:
                raise DecisionCompletionError("unknown_execution_status")
            await asyncio.sleep(poll_interval_seconds)
            snapshot = await clients.get_async_decision_execution(tenant_id, execution_id)
            current = snapshot.get("async_decision_execution")
            if not isinstance(current, dict):
                raise DecisionCompletionError("invalid_execution_response")

    try:
        return await asyncio.wait_for(follow(), timeout=timeout_seconds)
    except TimeoutError as exc:
        raise DecisionCompletionError("decision_completion_timeout") from exc
