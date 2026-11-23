import sys
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from crawl_mmdces import approved_url, main, parse_table


def row(name="Wisdom Bondieu Seade", index=0, district="Keta Municipal", link="/home/AllDLeaders/182"):
    return (f"<TR class='data' id='grid_DXDataRow{index}'><td><span>{name}</span></td>"
            f"<td>MALE</td><td>VOLTA</td><td>{district}</td><td><a href='{link}'>View</a></td></TR>")


def page(rows, count=1, pages=1):
    return f"<b>Page 1 of {pages} ({count} items)</b><table>{rows}</table>"


class CollectorTests(unittest.TestCase):
    def test_real_name_and_exact_office_with_reordered_attributes_and_nested_markup(self):
        records, rejected, count = parse_table(page(row("Wisdom&nbsp;Bondieu Seade")))
        self.assertEqual((count, rejected), (1, []))
        self.assertEqual(records[0]["name"], "Wisdom Bondieu Seade")
        self.assertEqual(records[0]["role"], "Municipal Chief Executive, Keta Municipal")
        self.assertEqual(records[0]["source_reference"], "https://www.ghanadistricts.com/home/AllDLeaders/182")

    def test_office_placeholder_is_not_a_person(self):
        records, rejected, count = parse_table(page(row() + row("North Dayi DCE", 1, "North Dayi District")
                                                   + row("Kwabre East MCD", 2, "Kwabre East Municipal"), 3))
        self.assertEqual(len(records), 1)
        self.assertEqual(rejected[0]["reason"], "office_placeholder_not_person")
        self.assertEqual(len(rejected), 2)
        self.assertEqual(count, 3)

    def test_truncated_or_paginated_or_challenge_pages_fail(self):
        for html in [page(row(), 2), page(row(), 1, 2), "<html>Access denied</html>", page(row()[:-5])]:
            with self.subTest(html=html), self.assertRaises(ValueError):
                parse_table(html)

    def test_invalid_columns_unknown_region_duplicate_rows_and_untrusted_links_fail(self):
        for html in [page(row().replace("VOLTA", "UNKNOWN")),
                     page(row().replace("<td>MALE</td>", "")),
                     page(row() + row(), 2),
                     page(row(link="https://evil.example/home/AllDLeaders/182"))]:
            with self.subTest(html=html), self.assertRaises(ValueError):
                parse_table(html)

    def test_redirects_stay_on_the_approved_https_source(self):
        for url in ["http://ghanadistricts.com/", "https://evil.example/", "https://ghanadistricts.com:8443/",
                    "https://user:pass@ghanadistricts.com/"]:
            with self.subTest(url=url), self.assertRaises(ValueError):
                approved_url(url)

    def test_bad_replay_and_network_failure_preserve_last_good_manifest(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "capture.json"
            output.write_text("last good capture", encoding="utf-8")
            html = Path(directory) / "challenge.html"
            html.write_text("<html>Access denied</html>", encoding="utf-8")
            with patch.object(sys, "argv", ["collector", "--input-html", str(html), "--output", str(output)]):
                with self.assertRaises(ValueError):
                    main()
            with patch.object(sys, "argv", ["collector", "--output", str(output)]), patch("crawl_mmdces.build_opener", side_effect=TimeoutError):
                with self.assertRaises(TimeoutError):
                    main()
            self.assertEqual(output.read_text(encoding="utf-8"), "last good capture")


if __name__ == "__main__":
    unittest.main()
