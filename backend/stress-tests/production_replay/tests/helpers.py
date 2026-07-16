from __future__ import annotations

import csv
import json
from pathlib import Path
from typing import Any


HEADERS = [
    "transtype",
    "payment_type_id",
    "source_id",
    "thirdparty_id",
    "source_date_created",
    "year",
    "month",
    "source_account_no",
    "source_trans_id",
    "processor",
    "channel_id",
    "terminal_id",
    "merchant_id",
    "product_id",
    "sub_merchant_id",
    "accountref",
    "accountname",
    "paymentmsisdn",
    "narration",
    "currency",
    "amount",
    "fees",
    "country",
]


def transaction_row(**overrides: str) -> dict[str, str]:
    row = {
        "transtype": "inflow",
        "payment_type_id": "MOMO",
        "source_id": "MTN",
        "thirdparty_id": "third-1",
        "source_date_created": "2026-02-01 10:00:00",
        "year": "2026",
        "month": "02",
        "source_account_no": "233200000001",
        "source_trans_id": "tx-1",
        "processor": "uniwallet_v2",
        "channel_id": "MOMO",
        "terminal_id": "233200000001",
        "merchant_id": "merchant-1",
        "product_id": "merchant-product-1",
        "sub_merchant_id": "0",
        "accountref": "233200000001",
        "accountname": "Test Account",
        "paymentmsisdn": "233200000001",
        "narration": "Test",
        "currency": "GHS",
        "amount": "10.50",
        "fees": "0.25",
        "country": "GHANA",
    }
    row.update(overrides)
    return row


def write_transactions(path: Path, rows: list[dict[str, str]]) -> None:
    with path.open("w", encoding="utf-8", newline="") as handle:
        writer = csv.DictWriter(handle, fieldnames=HEADERS)
        writer.writeheader()
        writer.writerows(rows)


def write_minimal_sources(root: Path) -> None:
    (root / "merchants.json").write_text(
        json.dumps([{"id": "merchant-1", "companyName": "Merchant", "createdAt": "2026-01-01T00:00:00Z"}]),
        encoding="utf-8",
    )
    (root / "products.json").write_text(
        json.dumps(
            [
                {
                    "merchantProductId": "merchant-product-1",
                    "productId": "catalog-1",
                    "merchantId": "merchant-1",
                }
            ]
        ),
        encoding="utf-8",
    )
    (root / "staff.csv").write_text("NO.,STAFF_NO,NAME,EMAIL,MSISDN\n1,ITC001,Test,test@example.com,233200000001\n", encoding="utf-8")


def manifest_data(streams: list[dict[str, Any]]) -> dict[str, Any]:
    return {
        "version": 1,
        "timezone": "Africa/Accra",
        "reference_data": {
            "merchant_globs": ["merchants.json"],
            "merchant_product_globs": ["products.json"],
            "staff_csv": "staff.csv",
        },
        "transaction_streams": streams,
    }


def stream(stream_id: str, pattern: str, **overrides: str) -> dict[str, Any]:
    value: dict[str, Any] = {
        "id": stream_id,
        "adapter": "production_transaction_csv_v1",
        "globs": [pattern],
        "channel": "wallet",
        "direction": "incoming",
        "system_type": "wallet_transfer",
    }
    value.update(overrides)
    return value
