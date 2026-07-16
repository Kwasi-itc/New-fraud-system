from __future__ import annotations

import asyncio
import hashlib
import json
import time
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from typing import Any

from .api_client import APIError, ServiceClients
from .manifest import ReplayManifest
from .reference_data import (
    MerchantWatchlist,
    ReferenceRecords,
    StaffLists,
    load_merchant_products,
    load_merchants,
    load_merchant_watchlist,
    load_staff_lists,
)
from .scenarios import ScenarioDef, build_portable_scenarios, stable_rule_id


@dataclass(frozen=True)
class FieldSpec:
    name: str
    data_type: str
    nullable: bool = True
    unique: bool = False


TABLE_FIELDS: dict[str, tuple[FieldSpec, ...]] = {
    "merchants": (
        FieldSpec("merchant_id", "string", False, True),
        FieldSpec("company_name", "string"),
        FieldSpec("company_name_normalized", "string"),
        FieldSpec("code", "string"),
        FieldSpec("trade_name", "string"),
        FieldSpec("alias", "string"),
        FieldSpec("country", "string"),
        FieldSpec("company_type", "string"),
        FieldSpec("status", "string"),
        FieldSpec("company_registration_number", "string"),
        FieldSpec("tax_identification_number", "string"),
        FieldSpec("created_at", "timestamp"),
        FieldSpec("updated_at_source", "timestamp"),
        FieldSpec("date_of_incorporation", "string"),
        FieldSpec("date_of_commencement", "string"),
    ),
    "merchant_products": (
        FieldSpec("merchant_product_id", "string", False, True),
        FieldSpec("catalog_product_id", "string"),
        FieldSpec("product_name", "string"),
        FieldSpec("name", "string"),
        FieldSpec("can_settle", "string"),
        FieldSpec("merchant_id", "string"),
        FieldSpec("status", "string"),
        FieldSpec("description", "string"),
    ),
    "transactions": (
        FieldSpec("transaction_id", "string", False, True),
        FieldSpec("date", "timestamp", False),
        FieldSpec("amount", "float", False),
        FieldSpec("fees", "float"),
        FieldSpec("currency", "string"),
        FieldSpec("country", "string"),
        FieldSpec("channel", "string", False),
        FieldSpec("direction", "string", False),
        FieldSpec("system_type", "string", False),
        FieldSpec("stream_id", "string", False),
        FieldSpec("processor", "string"),
        FieldSpec("transaction_type", "string"),
        FieldSpec("payment_type", "string"),
        FieldSpec("channel_id", "string"),
        FieldSpec("source_id", "string"),
        FieldSpec("thirdparty_id", "string"),
        FieldSpec("source_account_no", "string"),
        FieldSpec("source_trans_id", "string"),
        FieldSpec("terminal_id", "string"),
        FieldSpec("merchant_id", "string"),
        FieldSpec("product_id", "string"),
        FieldSpec("sub_merchant_id", "string"),
        FieldSpec("account_ref", "string"),
        FieldSpec("account_name", "string"),
        FieldSpec("payment_msisdn", "string"),
        FieldSpec("narration", "string"),
        FieldSpec("raw_account_ref", "string"),
        FieldSpec("raw_account_name", "string"),
        FieldSpec("raw_timestamp", "string"),
        FieldSpec("source_file", "string"),
    ),
}


class EnvironmentSetup:
    def __init__(self, manifest: ReplayManifest, clients: ServiceClients, tenant_id: str | None, tenant_name: str) -> None:
        self.manifest = manifest
        self.clients = clients
        self.tenant_id = tenant_id or ""
        self.tenant_name = tenant_name
        self.tables: dict[str, dict[str, Any]] = {}
        self.fields: dict[str, dict[str, dict[str, Any]]] = {}

    async def run(self, publication_timeout_seconds: float = 900.0) -> dict[str, Any]:
        await self.clients.wait_until_ready()
        await self._ensure_tenant()
        await self._ensure_no_scenario_collisions()
        await self._ensure_model()
        references = await self._load_reference_data()
        lists = await self._ensure_reference_lists(references[2], references[3])
        scenarios = await self._create_scenarios(publication_timeout_seconds)
        return {
            "setup_version": 1,
            "created_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
            "tenant_id": self.tenant_id,
            "tenant_name": self.tenant_name,
            "object_types": list(TABLE_FIELDS),
            "reference_data": {
                "merchants": asdict(references[0].stats),
                "merchant_products": asdict(references[1].stats),
                "staff": {
                    "source_rows": references[2].source_rows,
                    "staff_numbers": len(references[2].staff_numbers),
                    "emails": len(references[2].emails),
                    "msisdns": len(references[2].msisdns),
                },
                "merchant_watchlist": (
                    {
                        "source_rows": references[3].source_rows,
                        "names": len(references[3].names),
                        "duplicate_names": references[3].duplicate_names,
                        "missing_names": references[3].missing_names,
                    }
                    if references[3] is not None
                    else None
                ),
            },
            "custom_lists": lists,
            "scenarios": scenarios,
            "notes": [
                "Reference records were sent through ingestion in batches of at most 500.",
                "The harness calls the decision endpoint directly during replay, so ingestion outbox depth must be monitored externally.",
            ],
        }

    async def _ensure_tenant(self) -> None:
        if self.tenant_id:
            response = await self.clients.request(self.clients.data_model, "GET", f"/v1/tenants/{self.tenant_id}", 200)
            tenant = response["tenant"]
            self.tenant_name = tenant["name"]
        else:
            suffix = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
            response = await self.clients.request(
                self.clients.data_model,
                "POST",
                "/v1/tenants",
                201,
                json={"name": self.tenant_name, "external_key": f"production-replay-{suffix}"},
            )
            self.tenant_id = response["tenant"]["id"]
        await self.clients.request(
            self.clients.data_model,
            "POST",
            f"/v1/tenants/{self.tenant_id}/provision",
            {200, 409},
        )

    async def _ensure_model(self) -> None:
        response = await self.clients.request(
            self.clients.data_model, "GET", f"/v1/tenants/{self.tenant_id}/tables", 200
        )
        existing_tables = {item["name"]: item for item in response.get("tables", [])}
        for table_name, field_specs in TABLE_FIELDS.items():
            table = existing_tables.get(table_name)
            if table is None:
                table = (
                    await self.clients.request(
                        self.clients.data_model,
                        "POST",
                        f"/v1/tenants/{self.tenant_id}/tables",
                        201,
                        json={
                            "name": table_name,
                            "alias": table_name.replace("_", " ").title(),
                            "description": "Production replay stress-test data",
                            "semantic_type": "entity",
                        },
                    )
                )["table"]
            self.tables[table_name] = table
            fields_response = await self.clients.request(
                self.clients.data_model, "GET", f"/v1/tables/{table['id']}/fields", 200
            )
            existing_fields = {item["name"]: item for item in fields_response.get("fields", [])}
            self.fields[table_name] = {}
            for spec in field_specs:
                existing = existing_fields.get(spec.name)
                if existing is not None and existing.get("data_type") != spec.data_type:
                    raise APIError(
                        f"existing field {table_name}.{spec.name} has type {existing.get('data_type')!r}; expected {spec.data_type!r}"
                    )
                if existing is not None and spec.unique and existing.get("is_unique") is not True:
                    raise APIError(f"existing field {table_name}.{spec.name} must be unique for replay setup")
                if existing is not None and not spec.nullable and existing.get("nullable") is not False:
                    raise APIError(f"existing field {table_name}.{spec.name} must be non-nullable for replay setup")
                if existing is None:
                    existing = (
                        await self.clients.request(
                            self.clients.data_model,
                            "POST",
                            f"/v1/tables/{table['id']}/fields",
                            201,
                            json={
                                "name": spec.name,
                                "data_type": spec.data_type,
                                "nullable": spec.nullable,
                                "is_unique": spec.unique,
                            },
                        )
                    )["field"]
                self.fields[table_name][spec.name] = existing
        await self._ensure_links()
        await self.clients.request(
            self.clients.data_model, "GET", f"/v1/tenants/{self.tenant_id}/data-model", 200
        )

    async def _ensure_links(self) -> None:
        response = await self.clients.request(
            self.clients.data_model, "GET", f"/v1/tenants/{self.tenant_id}/links", 200
        )
        existing = {item["name"]: item for item in response.get("links", [])}
        links = (
            ("merchant", "merchants", "merchant_id", "transactions", "merchant_id"),
            ("merchant_product", "merchant_products", "merchant_product_id", "transactions", "product_id"),
            ("merchant_product_merchant", "merchants", "merchant_id", "merchant_products", "merchant_id"),
        )
        for name, parent_table, parent_field, child_table, child_field in links:
            payload = {
                "name": name,
                "parent_table_id": self.tables[parent_table]["id"],
                "parent_field_id": self.fields[parent_table][parent_field]["id"],
                "child_table_id": self.tables[child_table]["id"],
                "child_field_id": self.fields[child_table][child_field]["id"],
            }
            if name in existing:
                actual = existing[name]
                for key, expected in payload.items():
                    if actual.get(key) != expected:
                        raise APIError(f"existing link {name!r} is incompatible with the replay model")
                continue
            await self.clients.request(
                self.clients.data_model,
                "POST",
                f"/v1/tenants/{self.tenant_id}/links",
                201,
                json=payload,
            )

    async def _load_reference_data(
        self,
    ) -> tuple[ReferenceRecords, ReferenceRecords, StaffLists, MerchantWatchlist | None]:
        merchants = load_merchants(self.manifest.merchant_files())
        products = load_merchant_products(self.manifest.merchant_product_files())
        staff = load_staff_lists(self.manifest.staff_file())
        merchant_watchlist = load_merchant_watchlist(self.manifest.merchant_watchlist_file())
        try:
            await self._ingest_reference("merchants", merchants.records)
            await self._ingest_reference("merchant_products", products.records)
        except APIError as exc:
            raise APIError(
                f"reference ingestion failed. Confirm data-model and ingestion use the same tenant schemas in this environment: {exc}",
                status_code=exc.status_code,
            ) from exc
        return merchants, products, staff, merchant_watchlist

    async def _ingest_reference(self, object_type: str, records: list[dict[str, Any]]) -> None:
        for batch_number, start in enumerate(range(0, len(records), 500), start=1):
            batch = records[start : start + 500]
            digest = hashlib.sha256(json.dumps(batch, sort_keys=True, separators=(",", ":")).encode()).hexdigest()[:24]
            key = f"production-replay-setup:{object_type}:{batch_number}:{digest}"
            await self.clients.ingest_batch(self.tenant_id, object_type, batch, key)

    async def _ensure_reference_lists(
        self, staff: StaffLists, merchant_watchlist: MerchantWatchlist | None
    ) -> dict[str, int]:
        values: dict[str, tuple[str, tuple[str, ...]]] = {
            "fraud_staff_numbers": ("string", staff.staff_numbers),
            "fraud_staff_emails": ("email", staff.emails),
            "fraud_staff_msisdns": ("string", staff.msisdns),
        }
        if merchant_watchlist is not None:
            values["fraud_merchant_names"] = ("string", merchant_watchlist.names)
        response = await self.clients.request(
            self.clients.decision_engine,
            "GET",
            f"/v1/tenants/{self.tenant_id}/platform/custom-lists",
            200,
        )
        existing_lists = {item["name"]: item for item in response.get("custom_lists", [])}
        counts: dict[str, int] = {}
        for name, (kind, entries) in values.items():
            custom_list = existing_lists.get(name)
            if custom_list is None:
                custom_list = (
                    await self.clients.request(
                        self.clients.decision_engine,
                        "POST",
                        f"/v1/tenants/{self.tenant_id}/platform/custom-lists",
                        201,
                        json={"name": name, "description": "Internal fraud stress-test reference data", "kind": kind},
                    )
                )["custom_list"]
            elif custom_list.get("kind") != kind:
                raise APIError(f"existing custom list {name!r} has incompatible kind {custom_list.get('kind')!r}")
            current_response = await self.clients.request(
                self.clients.decision_engine,
                "GET",
                f"/v1/tenants/{self.tenant_id}/platform/custom-lists/{custom_list['id']}/entries",
                200,
            )
            current = {item["value"] for item in current_response.get("custom_list_entries", [])}
            for entry in entries:
                if entry not in current:
                    await self.clients.request(
                        self.clients.decision_engine,
                        "POST",
                        f"/v1/tenants/{self.tenant_id}/platform/custom-lists/{custom_list['id']}/entries",
                        201,
                        json={"value": entry},
                    )
            counts[name] = len(entries)
        return counts

    async def _ensure_no_scenario_collisions(self) -> None:
        definitions = build_portable_scenarios(self.manifest)
        response = await self.clients.request(
            self.clients.decision_engine, "GET", f"/v1/tenants/{self.tenant_id}/scenarios", 200
        )
        existing_names = {item["name"] for item in response.get("scenarios", [])}
        collisions = sorted(existing_names & {item.name for item in definitions})
        if collisions:
            raise APIError(
                "managed replay scenarios already exist in this tenant; use the prior setup state or a clean tenant: "
                + ", ".join(collisions)
            )

    async def _create_scenarios(self, publication_timeout_seconds: float) -> dict[str, Any]:
        definitions = build_portable_scenarios(self.manifest)
        created: list[tuple[ScenarioDef, str, str]] = []
        try:
            for definition in definitions:
                scenario = (
                    await self.clients.request(
                        self.clients.decision_engine,
                        "POST",
                        f"/v1/tenants/{self.tenant_id}/scenarios",
                        201,
                        json={"name": definition.name, "trigger_object_type": "transactions"},
                    )
                )["scenario"]
                iteration = (
                    await self.clients.request(
                        self.clients.decision_engine,
                        "POST",
                        f"/v1/tenants/{self.tenant_id}/scenarios/{scenario['id']}/iterations",
                        201,
                    )
                )["iteration"]
                created.append((definition, scenario["id"], iteration["id"]))
                thresholds = {
                    "score_review_threshold": 30,
                    "score_block_and_review_threshold": 60,
                    "score_decline_threshold": 90,
                }
                if definition.regulatory:
                    thresholds = {
                        "score_review_threshold": 0,
                        "score_block_and_review_threshold": 9_999,
                        "score_decline_threshold": 99_999,
                    }
                base = f"/v1/tenants/{self.tenant_id}/scenarios/{scenario['id']}/iterations/{iteration['id']}"
                await self.clients.request(
                    self.clients.decision_engine,
                    "PUT",
                    base,
                    200,
                    json={"trigger_formula": definition.trigger_formula, "schedule": "", **thresholds},
                )
                for display_order, rule in enumerate(definition.rules, start=1):
                    await self.clients.request(
                        self.clients.decision_engine,
                        "POST",
                        f"{base}/rules",
                        201,
                        json={
                            "display_order": display_order,
                            "name": rule.name,
                            "description": rule.description,
                            "formula": rule.formula,
                            "score_modifier": rule.score,
                            "rule_group": rule.group,
                            "stable_rule_id": stable_rule_id(rule.name),
                        },
                    )
                validation = await self.clients.request(self.clients.decision_engine, "POST", f"{base}/validate", 200)
                if validation.get("validation", {}).get("valid") is not True:
                    raise APIError(f"scenario validation failed for {definition.name}: {json.dumps(validation, default=str)}")
                await self.clients.request(self.clients.decision_engine, "POST", f"{base}/commit", 200)

            await self._prepare_publications(created, publication_timeout_seconds)
            result: dict[str, Any] = {}
            for definition, scenario_id, iteration_id in created:
                await self.clients.request(
                    self.clients.decision_engine,
                    "POST",
                    f"/v1/tenants/{self.tenant_id}/scenarios/{scenario_id}/publications",
                    200,
                    json={"action": "publish", "iteration_id": iteration_id},
                )
                result[definition.name] = {
                    "scenario_id": scenario_id,
                    "iteration_id": iteration_id,
                    "rules": [rule.name for rule in definition.rules],
                }
            return result
        except Exception:
            await self._remove_created_scenarios(created)
            raise

    async def _remove_created_scenarios(self, created: list[tuple[ScenarioDef, str, str]]) -> None:
        for _definition, scenario_id, _iteration_id in reversed(created):
            try:
                await self.clients.request(
                    self.clients.decision_engine,
                    "DELETE",
                    f"/v1/tenants/{self.tenant_id}/scenarios/{scenario_id}",
                    204,
                )
            except APIError:
                pass

    async def _prepare_publications(
        self, created: list[tuple[ScenarioDef, str, str]], timeout_seconds: float
    ) -> None:
        pending: dict[str, tuple[str, str]] = {}
        for definition, scenario_id, iteration_id in created:
            status = await self._preparation_status(scenario_id, iteration_id)
            if self._prepared(status):
                continue
            await self.clients.request(
                self.clients.decision_engine,
                "POST",
                f"/v1/tenants/{self.tenant_id}/scenarios/{scenario_id}/publications/preparation",
                202,
                json={"iteration_id": iteration_id},
            )
            pending[definition.name] = (scenario_id, iteration_id)
        deadline = time.monotonic() + timeout_seconds
        while pending and time.monotonic() < deadline:
            await asyncio.sleep(1.0)
            for name, (scenario_id, iteration_id) in list(pending.items()):
                if self._prepared(await self._preparation_status(scenario_id, iteration_id)):
                    del pending[name]
        if pending:
            raise APIError(
                f"scenario publication indexes did not finish within {timeout_seconds:g}s: {', '.join(sorted(pending))}. "
                "Confirm the data-model index worker is running."
            )

    async def _preparation_status(self, scenario_id: str, iteration_id: str) -> dict[str, Any]:
        response = await self.clients.request(
            self.clients.decision_engine,
            "GET",
            f"/v1/tenants/{self.tenant_id}/scenarios/{scenario_id}/publications/preparation",
            200,
            params={"iteration_id": iteration_id},
        )
        return response.get("preparation", response)

    @staticmethod
    def _prepared(status: dict[str, Any]) -> bool:
        return status.get("preparation_finished") is True and status.get("preparation_required") is not True
