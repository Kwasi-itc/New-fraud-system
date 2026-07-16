from __future__ import annotations

from .base import AdapterError, TransactionAdapter
from .uniwallet_csv_v1 import ProductionTransactionCSVV1Adapter, UniwalletCSVV1Adapter


_ADAPTERS: dict[str, TransactionAdapter] = {
    ProductionTransactionCSVV1Adapter.name: ProductionTransactionCSVV1Adapter(),
    UniwalletCSVV1Adapter.name: UniwalletCSVV1Adapter(),
}


def get_adapter(name: str) -> TransactionAdapter:
    try:
        return _ADAPTERS[name]
    except KeyError as exc:
        supported = ", ".join(sorted(_ADAPTERS))
        raise AdapterError(f"unsupported transaction adapter {name!r}; supported adapters: {supported}") from exc


__all__ = ["AdapterError", "TransactionAdapter", "get_adapter"]
