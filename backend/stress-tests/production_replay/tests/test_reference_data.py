from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from openpyxl import Workbook

from production_replay.reference_data import load_merchant_products, load_merchants, load_merchant_watchlist, load_staff_lists


class ReferenceDataTests(unittest.TestCase):
    def test_merchant_latest_version_and_product_final_batch_win(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            merchant_a = root / "merchant-a.json"
            merchant_b = root / "merchant-b.json"
            merchant_a.write_text(json.dumps([{"id": "m1", "companyName": "Old", "createdAt": "2025-01-01T00:00:00Z"}]), encoding="utf-8")
            merchant_b.write_text(json.dumps([{"id": "m1", "companyName": "New", "updatedAt": "2026-01-01T00:00:00Z"}]), encoding="utf-8")
            product_a = root / "product-a.json"
            product_b = root / "product-b.json"
            product_a.write_text(json.dumps([{"merchantProductId": "p1", "merchantId": "m1", "name": "Old"}]), encoding="utf-8")
            product_b.write_text(json.dumps([{"merchantProductId": "p1", "merchantId": "m1", "name": "New"}]), encoding="utf-8")

            merchants = load_merchants([merchant_a, merchant_b])
            products = load_merchant_products([product_a, product_b])

            self.assertEqual(merchants.records[0]["company_name"], "New")
            self.assertEqual(merchants.stats.duplicate_rows, 1)
            self.assertEqual(merchants.stats.conflicting_keys, 1)
            self.assertEqual(products.records[0]["name"], "New")

    def test_staff_values_are_split_and_normalized(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "staff.csv"
            path.write_text(
                "NO.,STAFF_NO,NAME,EMAIL,MSISDN\n"
                "1,itc001,Test,TEST@EXAMPLE.COM,0240000000 / 233200000001\n",
                encoding="utf-8",
            )
            staff = load_staff_lists(path)
            self.assertEqual(staff.staff_numbers, ("ITC001",))
            self.assertEqual(staff.emails, ("test@example.com",))
            self.assertEqual(staff.msisdns, ("233200000001", "233240000000"))

    def test_merchant_watchlist_names_are_normalized_and_deduplicated(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "merchants.xlsx"
            workbook = Workbook()
            sheet = workbook.active
            sheet.append(["ID", "Name"])
            sheet.append(["1", " Example   Merchant "])
            sheet.append(["2", "example merchant"])
            sheet.append(["3", None])
            workbook.save(path)
            workbook.close()

            watchlist = load_merchant_watchlist(path)
            assert watchlist is not None
            self.assertEqual(watchlist.names, ("example merchant",))
            self.assertEqual(watchlist.source_rows, 3)
            self.assertEqual(watchlist.duplicate_names, 1)
            self.assertEqual(watchlist.missing_names, 1)


if __name__ == "__main__":
    unittest.main()
