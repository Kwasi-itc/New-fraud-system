from __future__ import annotations

import csv
import hashlib
import json
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Iterator
from zoneinfo import ZoneInfo, ZoneInfoNotFoundError

from ..domain import TransactionEvent
from ..manifest import TransactionStream
from .base import AdapterError


class ProductionTransactionCSVV1Adapter:
    name = "production_transaction_csv_v1"
    required_headers = {
        "source_date_created",
        "source_trans_id",
        "merchant_id",
        "product_id",
        "amount",
        "currency",
    }

    def iter_events(self, stream: TransactionStream, files: list[Path]) -> Iterator[TransactionEvent]:
        try:
            source_timezone = ZoneInfo(stream.timezone)
        except ZoneInfoNotFoundError as exc:
            raise AdapterError(f"unknown timezone {stream.timezone!r} for stream {stream.id!r}") from exc

        for path in files:
            with path.open("r", encoding="utf-8-sig", newline="") as handle:
                reader = csv.DictReader(handle)
                headers = set(reader.fieldnames or [])
                missing = self.required_headers - headers
                if missing:
                    raise AdapterError(f"{path} is missing required columns: {sorted(missing)}")
                for row_number, row in enumerate(reader, start=2):
                    try:
                        occurred_at = datetime.strptime(row["source_date_created"].strip(), "%Y-%m-%d %H:%M:%S")
                        occurred_at = occurred_at.replace(tzinfo=source_timezone).astimezone(timezone.utc)
                        amount = float(row["amount"])
                        fees = _optional_float(row.get("fees"))
                    except (TypeError, ValueError) as exc:
                        raise AdapterError(f"invalid row at {path}:{row_number}: {exc}") from exc

                    source_transaction_id = _clean(row.get("source_trans_id"))
                    third_party_id = _clean(row.get("thirdparty_id"))
                    fallback = f"{path.stem}:{row_number}"
                    source_key = source_transaction_id or third_party_id or fallback
                    object_id = _object_id(stream.id, path, row_number, source_key)
                    source_direction = _clean(row.get("transtype"))
                    expected_direction = {"inflow": "incoming", "outflow": "outgoing"}.get(
                        (source_direction or "").lower()
                    )
                    if expected_direction and expected_direction != stream.direction:
                        raise AdapterError(
                            f"direction mismatch at {path}:{row_number}: row is {source_direction!r}, "
                            f"stream maps to {stream.direction!r}"
                        )
                    raw_account_ref = _clean(row.get("accountref"))
                    raw_account_name = _clean(row.get("accountname"))
                    account_ref = (
                        _clean(row.get("source_account_no"))
                        or _clean(row.get("paymentmsisdn"))
                        or _json_value(raw_account_ref, "idNumber")
                        or raw_account_ref
                    )
                    account_name = _json_value(raw_account_name, "name") or raw_account_name
                    transaction_id = object_id
                    fields: dict[str, Any] = {
                        "object_id": object_id,
                        "transaction_id": transaction_id,
                        "date": occurred_at.isoformat().replace("+00:00", "Z"),
                        "amount": amount,
                        "fees": fees,
                        "currency": _clean(row.get("currency")),
                        "country": _country_code(row.get("country")),
                        "channel": stream.channel,
                        "direction": stream.direction,
                        "system_type": stream.system_type,
                        "stream_id": stream.id,
                        "processor": _clean(row.get("processor")),
                        "transaction_type": _clean(row.get("transtype")),
                        "payment_type": _clean(row.get("payment_type_id")),
                        "channel_id": _clean(row.get("channel_id")),
                        "source_id": _clean(row.get("source_id")),
                        "thirdparty_id": third_party_id,
                        "source_account_no": _clean(row.get("source_account_no")),
                        "source_trans_id": source_transaction_id,
                        "terminal_id": _clean(row.get("terminal_id")),
                        "merchant_id": _clean(row.get("merchant_id")),
                        "product_id": _clean(row.get("product_id")),
                        "sub_merchant_id": _clean(row.get("sub_merchant_id")),
                        "account_ref": account_ref,
                        "account_name": account_name,
                        "payment_msisdn": _clean(row.get("paymentmsisdn")),
                        "narration": _clean(row.get("narration")),
                        "raw_account_ref": raw_account_ref,
                        "raw_account_name": raw_account_name,
                        "raw_timestamp": _clean(row.get("source_date_created")),
                        "source_file": path.name,
                    }
                    yield TransactionEvent(occurred_at, stream.id, object_id, fields, path, row_number)


class UniwalletCSVV1Adapter(ProductionTransactionCSVV1Adapter):
    """Backward-compatible name for manifests created before the final extract."""

    name = "uniwallet_csv_v1"


def _object_id(stream_id: str, path: Path, row_number: int, source_key: str) -> str:
    identity = f"{stream_id}:{path.parent.name}/{path.name}:{row_number}:{source_key}"
    digest = hashlib.sha256(identity.encode()).hexdigest()[:32]
    return f"production-replay:{stream_id}:{digest}"


def _clean(value: Any) -> str | None:
    if value is None:
        return None
    result = str(value).strip()
    return result or None


def _optional_float(value: Any) -> float | None:
    result = _clean(value)
    return None if result is None else float(result)


def _json_value(value: str | None, key: str) -> str | None:
    if not value or not value.startswith("{"):
        return None
    try:
        parsed = json.loads(value)
    except json.JSONDecodeError:
        return None
    item = parsed.get(key) if isinstance(parsed, dict) else None
    return _clean(item)


def _country_code(value: Any) -> str | None:
    country = _clean(value)
    if country and country.upper() in {"GH", "GHANA"}:
        return "GH"
    return country.upper() if country else None
