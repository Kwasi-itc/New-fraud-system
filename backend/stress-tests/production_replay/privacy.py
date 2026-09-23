from __future__ import annotations

import hashlib
import hmac
from dataclasses import replace
from typing import Any

from .domain import TransactionEvent


INTERNAL_RETAINED_FIELDS = frozenset(
    {
        "object_id",
        "transaction_id",
        "date",
        "amount",
        "channel",
        "direction",
        "system_type",
        "stream_id",
        "merchant_id",
        "account_ref",
    }
)
TOKENIZED_FIELDS = frozenset({"account_ref"})
EXPLICITLY_DROPPED_PII_FIELDS = frozenset(
    {
        "source_account_no",
        "source_trans_id",
        "thirdparty_id",
        "terminal_id",
        "account_name",
        "payment_msisdn",
        "narration",
        "raw_account_ref",
        "raw_account_name",
    }
)


class InternalPrivacyTransformer:
    """Minimise benchmark records and deterministically pseudonymise join keys."""

    def __init__(self, key: bytes) -> None:
        if len(key) < 16:
            raise ValueError("PII key must contain at least 16 bytes")
        self._key = key

    def transform_event(self, event: TransactionEvent) -> TransactionEvent:
        object_id = self.token(event.object_id, namespace="object_id")
        fields = self.transform_fields(event.fields)
        fields["object_id"] = object_id
        fields["transaction_id"] = object_id
        return replace(event, object_id=object_id, fields=fields)

    def minimize_presanitized_event(self, event: TransactionEvent) -> TransactionEvent:
        """Minimise a row whose source identifiers were already HMAC-tokenized."""

        fields = {name: value for name, value in event.fields.items() if name in INTERNAL_RETAINED_FIELDS}
        account_ref = fields.get("account_ref")
        if account_ref is not None and not str(account_ref).startswith("hmac256:"):
            raise ValueError("pre-sanitized account_ref is not an HMAC token")
        fields["object_id"] = event.object_id
        fields["transaction_id"] = event.object_id
        return replace(event, fields=fields)

    def transform_fields(self, fields: dict[str, Any]) -> dict[str, Any]:
        result = {name: value for name, value in fields.items() if name in INTERNAL_RETAINED_FIELDS}
        for name in TOKENIZED_FIELDS:
            value = result.get(name)
            if value is not None and str(value).strip():
                result[name] = self.token(str(value), namespace=name)
            else:
                result[name] = None
        return result

    def token(self, value: str, *, namespace: str = "value") -> str:
        normalized = " ".join(value.strip().split()).casefold()
        message = f"{namespace}\0{normalized}".encode("utf-8")
        digest = hmac.new(self._key, message, hashlib.sha256).hexdigest()
        return f"hmac256:{digest}"
