"""Collect live MMDA leaders across all 261 districts from GhanaDistricts.

1. Fetches https://www.ghanadistricts.com/Home/DistrictLeaders/1 to discover
   all 16 regions and all 261 district IDs/names from the #Region_Districts menu.
2. Fetches https://www.ghanadistricts.com/Home/DistrictLeaders/<id> concurrently
   for every district and parses the DevExpress grid rows (Name, Position,
   Gender, Start Date), rejecting office placeholders like 'Adansi North DCE'.
3. Writes observations atomically to crawled_mmdces.json.

Uses only the Python standard library (3.10+).
"""

import argparse
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime, timezone
from html.parser import HTMLParser
import json
import os
from pathlib import Path
import re
import tempfile
from urllib.parse import urlparse
from urllib.request import HTTPRedirectHandler, Request, build_opener

ROOT = Path(__file__).resolve().parent.parent
SEED_URL = "https://www.ghanadistricts.com/Home/DistrictLeaders/1"
MAX_BYTES = 8 * 1024 * 1024
ALLOWED_HOSTS = {"ghanadistricts.com", "www.ghanadistricts.com"}

PLACEHOLDER_RE = re.compile(
    r"\b(?:DCE|MCE|MMDCE|MCD|DCD|Chief Executive|Coordinating Director|Presiding Member|Vacant|Not appointed)\b",
    re.I,
)


def approved_url(value: str) -> str:
    url = urlparse(value)
    if (
        url.scheme != "https"
        or url.hostname not in ALLOWED_HOSTS
        or url.username
        or url.password
        or url.port not in (None, 443)
    ):
        raise ValueError(f"Unapproved GhanaDistricts URL: {value}")
    return value


class SourceRedirects(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        approved_url(newurl)
        return super().redirect_request(req, fp, code, msg, headers, newurl)


class DistrictTable(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.rows = []
        self.row = None
        self.cell = None

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == "tr" and re.search(r"DXDataRow\d+$", attrs.get("id", ""), re.I):
            self.row = {"row_id": attrs["id"], "cells": []}
        elif self.row is not None and tag == "td":
            self.cell = []

    def handle_data(self, value):
        if self.cell is not None:
            self.cell.append(value)

    def handle_endtag(self, tag):
        if tag == "td" and self.cell is not None:
            self.row["cells"].append(" ".join("".join(self.cell).split()))
            self.cell = None
        elif tag == "tr" and self.row is not None:
            self.rows.append(self.row)
            self.row = None


def fetch_html(url: str, opener) -> str:
    approved_url(url)
    req = Request(url, headers={"User-Agent": "ITC-Screening-SourceCollector/1.0", "Accept": "text/html"})
    with opener.open(req, timeout=30) as resp:
        approved_url(resp.url)
        if resp.status != 200:
            raise ValueError(f"HTTP {resp.status} for {url}")
        body = resp.read(MAX_BYTES + 1)
    if len(body) > MAX_BYTES:
        raise ValueError(f"HTML exceeds {MAX_BYTES} bytes for {url}")
    return body.decode("utf-8-sig", errors="replace")


def discover_districts(html: str):
    """Extract (district_id, district_name, region_name) from #Region_Districts."""
    region_names = {}
    for m in re.finditer(
        r'href=["\']/Home/Region/(\d+)["\'][^>]*>.*?<span[^>]*class=["\'][^"\']*dxm-contentText[^"\']*["\'][^>]*>(.*?)</span>',
        html,
        re.S | re.I,
    ):
        rid = int(m.group(1))
        rname = " ".join(re.sub(r"<[^>]+>", "", m.group(2)).replace("&nbsp;", " ").split()).title()
        region_names[rid] = rname

    # Top-level region order in Region_Districts_DXM<idx>_ matches the menu tabs
    tab_regions = [
        " ".join(re.sub(r"<[^>]+>", "", m.group(1)).replace("&nbsp;", " ").split()).title()
        for m in re.finditer(
            r'href=["\']/Home/Region/\d+["\'][^>]*>.*?<span[^>]*class=["\'][^"\']*dxm-contentText[^"\']*["\'][^>]*>(.*?)</span>',
            html,
            re.S | re.I,
        )
    ]

    districts = {}
    for menu_m in re.finditer(
        r'id=["\']Region_Districts_DXM(\d+)_["\']([\s\S]*?)</div>\s*<div class=["\']dxm-scrollDownBtn',
        html,
        re.I,
    ):
        idx = int(menu_m.group(1))
        region = tab_regions[idx] if idx < len(tab_regions) else "Ghana"
        block = menu_m.group(2)
        for dm in re.finditer(
            r'href=["\']/Home/District/(\d+)["\'][^>]*>.*?<span[^>]*class=["\'][^"\']*dxm-contentText[^"\']*["\'][^>]*>(.*?)</span>',
            block,
            re.S | re.I,
        ):
            did = int(dm.group(1))
            dname = " ".join(re.sub(r"<[^>]+>", "", dm.group(2)).replace("&nbsp;", " ").split())
            if did not in districts:
                districts[did] = (did, dname, region)

    return sorted(districts.values(), key=lambda x: x[0])


def parse_district_leaders(html: str, did: int, district: str, region: str):
    url = f"https://www.ghanadistricts.com/Home/DistrictLeaders/{did}"
    table = DistrictTable()
    table.feed(html)
    table.close()

    records = []
    rejected = []
    for row in table.rows:
        cells = row["cells"]
        if len(cells) < 3:
            continue
        name = cells[0].strip()
        position = cells[1].strip()
        gender = cells[2].strip() if len(cells) > 2 else ""
        start_date = cells[3].strip() if len(cells) > 3 else ""

        if not name or not position:
            continue
        if PLACEHOLDER_RE.search(name):
            rejected.append({"district_id": did, "district": district, "name": name, "reason": "office_placeholder"})
            continue
        if len(name.split()) < 2:
            rejected.append({"district_id": did, "district": district, "name": name, "reason": "invalid_name"})
            continue

        records.append({
            "district_id": did,
            "name": name,
            "region": region,
            "district": district,
            "position": position,
            "gender": gender,
            "start_date": start_date,
            "role": f"{position}, {district} ({region} Region)",
            "source_reference": url,
        })
    return records, rejected


def atomic_json(path: Path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", dir=path.parent, delete=False) as target:
        temp = Path(target.name)
        json.dump(value, target, ensure_ascii=False, indent=2)
        target.write("\n")
    try:
        os.replace(temp, path)
    finally:
        temp.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--workers", type=int, default=12, help="Concurrent HTTP workers (default 12)")
    parser.add_argument("--output", type=Path, help="Output JSON path (default crawled_mmdces.json in CWD)")
    args = parser.parse_args()

    opener = build_opener(SourceRedirects())
    seed_html = fetch_html(SEED_URL, opener)
    districts = discover_districts(seed_html)
    if not districts:
        raise ValueError("Failed to discover districts from GhanaDistricts")

    all_records = []
    all_rejected = []
    failed_districts = []
    seen = set()

    def worker(item):
        did, dname, region = item
        url = f"https://www.ghanadistricts.com/Home/DistrictLeaders/{did}"
        html = seed_html if did == 1 else fetch_html(url, opener)
        return parse_district_leaders(html, did, dname, region)

    with ThreadPoolExecutor(max_workers=max(1, min(24, args.workers))) as pool:
        futures = {pool.submit(worker, d): d for d in districts}
        for fut in as_completed(futures):
            d = futures[fut]
            try:
                recs, rej = fut.result()
                all_rejected.extend(rej)
                for r in recs:
                    key = (r["name"].casefold(), r["district"].casefold(), r["position"].casefold())
                    if key not in seen:
                        seen.add(key)
                        all_records.append(r)
            except Exception as exc:
                failed_districts.append({"district_id": d[0], "district": d[1], "error": str(exc)})

    if not all_records:
        raise ValueError("Zero MMDA leader records collected across all districts")

    all_records.sort(key=lambda r: (r["district_id"], r["position"], r["name"]))
    manifest = {
        "schema_version": 3,
        "acquisition": "live_html_all_districts",
        "observed_at": datetime.now(timezone.utc).isoformat(),
        "source_url": SEED_URL,
        "districts_discovered": len(districts),
        "districts_failed": len(failed_districts),
        "records": all_records,
        "rejected_rows": all_rejected,
    }

    out_path = args.output or (Path.cwd() / "crawled_mmdces.json")
    atomic_json(out_path, manifest)
    print(
        f"Collected {len(all_records)} named MMDA leaders across {len(districts) - len(failed_districts)}/{len(districts)} districts "
        f"(rejected {len(all_rejected)} placeholders, failed {len(failed_districts)} districts)."
    )


if __name__ == "__main__":
    main()
