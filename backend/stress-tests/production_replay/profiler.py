from __future__ import annotations

from collections import Counter
from dataclasses import asdict
from datetime import datetime
from typing import Any

from .adapters import get_adapter
from .manifest import ReplayManifest
from .reference_data import load_merchant_products, load_merchants, load_merchant_watchlist, load_staff_lists


PROFILE_FIELDS = (
    "account_ref",
    "account_name",
    "payment_msisdn",
    "narration",
    "raw_account_ref",
    "raw_account_name",
    "merchant_id",
    "product_id",
    "transaction_id",
    "source_trans_id",
    "date",
    "amount",
    "currency",
    "country",
    "channel",
    "direction",
    "processor",
)


def profile_manifest(manifest: ReplayManifest) -> dict[str, Any]:
    merchants = load_merchants(manifest.merchant_files())
    products = load_merchant_products(manifest.merchant_product_files())
    staff = load_staff_lists(manifest.staff_file())
    merchant_watchlist = load_merchant_watchlist(manifest.merchant_watchlist_file())
    merchant_ids = {row["merchant_id"] for row in merchants.records}
    product_ids = {row["merchant_product_id"] for row in products.records}
    product_pairs = {(row["merchant_id"], row["merchant_product_id"]) for row in products.records}

    per_second: Counter[int] = Counter()
    per_minute: Counter[int] = Counter()
    categories: dict[str, Counter[str]] = {
        name: Counter() for name in ("stream_id", "channel", "direction", "system_type", "processor", "source_id", "currency", "country")
    }
    missing: Counter[str] = Counter()
    duplicate_source_ids_within_file = 0
    matched_merchants = 0
    matched_products = 0
    matched_pairs = 0
    embedded_newlines = Counter()
    earliest: datetime | None = None
    latest: datetime | None = None
    row_count = 0
    streams: list[dict[str, Any]] = []

    for stream in manifest.transaction_streams:
        files = manifest.stream_files(stream)
        stream_rows = 0
        stream_earliest: datetime | None = None
        stream_latest: datetime | None = None
        current_file = None
        seen_source_ids: set[str] = set()
        adapter = get_adapter(stream.adapter)
        for event in adapter.iter_events(stream, files):
            row_count += 1
            stream_rows += 1
            fields = event.fields
            event_second = int(event.occurred_at.timestamp())
            per_second[event_second] += 1
            per_minute[event_second // 60] += 1
            earliest = event.occurred_at if earliest is None or event.occurred_at < earliest else earliest
            latest = event.occurred_at if latest is None or event.occurred_at > latest else latest
            stream_earliest = event.occurred_at if stream_earliest is None or event.occurred_at < stream_earliest else stream_earliest
            stream_latest = event.occurred_at if stream_latest is None or event.occurred_at > stream_latest else stream_latest
            for name in PROFILE_FIELDS:
                if fields.get(name) in (None, ""):
                    missing[name] += 1
            for name in categories:
                value = fields.get(name)
                categories[name][str(value) if value is not None else "<missing>"] += 1
            if event.source_file != current_file:
                current_file = event.source_file
                seen_source_ids.clear()
            source_transaction_id = fields.get("source_trans_id")
            if source_transaction_id:
                if source_transaction_id in seen_source_ids:
                    duplicate_source_ids_within_file += 1
                else:
                    seen_source_ids.add(source_transaction_id)
            merchant_id = fields.get("merchant_id")
            product_id = fields.get("product_id")
            matched_merchants += int(merchant_id in merchant_ids)
            matched_products += int(product_id in product_ids)
            matched_pairs += int((merchant_id, product_id) in product_pairs)
            embedded_newlines["raw_account_ref"] += int("\n" in (fields.get("raw_account_ref") or ""))
            embedded_newlines["raw_account_name"] += int("\n" in (fields.get("raw_account_name") or ""))
        streams.append(
            {
                "id": stream.id,
                "adapter": stream.adapter,
                "channel": stream.channel,
                "direction": stream.direction,
                "system_type": stream.system_type,
                "timezone": stream.timezone,
                "file_count": len(files),
                "row_count": stream_rows,
                "earliest": _iso(stream_earliest),
                "latest": _iso(stream_latest),
            }
        )

    span_seconds = 0 if earliest is None or latest is None else int((latest - earliest).total_seconds()) + 1
    return {
        "profile_version": 1,
        "manifest": str(manifest.path),
        "source_fingerprint": manifest.source_fingerprint(),
        "reference_data": {
            "merchants": asdict(merchants.stats),
            "merchant_products": asdict(products.stats),
            "staff": {
                "source_rows": staff.source_rows,
                "staff_numbers": len(staff.staff_numbers),
                "emails": len(staff.emails),
                "msisdns": len(staff.msisdns),
            },
            "merchant_watchlist": (
                {
                    **asdict(merchant_watchlist),
                    "names": len(merchant_watchlist.names),
                    "matched_merchant_company_names": len(
                        set(merchant_watchlist.names)
                        & {row["company_name_normalized"] for row in merchants.records if row["company_name_normalized"]}
                    ),
                }
                if merchant_watchlist is not None
                else None
            ),
        },
        "transactions": {
            "streams": streams,
            "row_count": row_count,
            "earliest": _iso(earliest),
            "latest": _iso(latest),
            "span_seconds": span_seconds,
            "average_events_per_second": round(row_count / span_seconds, 4) if span_seconds else 0,
            "p95_events_per_second": _frequency_percentile(per_second, span_seconds, 95),
            "p99_events_per_second": _frequency_percentile(per_second, span_seconds, 99),
            "peak_events_per_second": max(per_second.values(), default=0),
            "peak_events_per_minute": max(per_minute.values(), default=0),
            "object_id_policy": "stream-and-source-row-versioned",
            "duplicate_source_transaction_ids_within_file": duplicate_source_ids_within_file,
            "missing_fields": dict(sorted(missing.items())),
            "embedded_newlines": dict(embedded_newlines),
            "categories": {name: dict(counter.most_common()) for name, counter in categories.items()},
            "reference_coverage": {
                "merchant_rows_matched": matched_merchants,
                "merchant_product_rows_matched": matched_products,
                "merchant_product_pair_rows_matched": matched_pairs,
                "total_rows": row_count,
            },
        },
    }


def _frequency_percentile(counter: Counter[int], span: int, percentile: int) -> int:
    if span <= 0:
        return 0
    frequencies: Counter[int] = Counter(counter.values())
    frequencies[0] += max(span - len(counter), 0)
    target = ((percentile * span) + 99) // 100
    cumulative = 0
    for value in sorted(frequencies):
        cumulative += frequencies[value]
        if cumulative >= target:
            return value
    return max(frequencies, default=0)


def _iso(value: datetime | None) -> str | None:
    return value.isoformat().replace("+00:00", "Z") if value else None
