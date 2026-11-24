"""Collect the visible GhanaDistricts MMDCE table, not a national roster.

Writes observations only. Compile and explicitly publish a reviewed snapshot
separately. Partial tables must never withdraw absent office holders.
Uses only the Python standard library (3.10+).
"""

import argparse
from datetime import datetime, timezone
from hashlib import sha256
from html.parser import HTMLParser
import json
import os
from pathlib import Path
import re
import tempfile
from urllib.parse import urljoin, urlparse
from urllib.request import HTTPRedirectHandler, Request, build_opener

ROOT = Path(__file__).resolve().parent.parent
SOURCE_URL = "https://www.ghanadistricts.com/home/AllDistricts"
MAX_BYTES = 8 * 1024 * 1024
REGIONS = {
    "ASHANTI": "Ashanti", "G. ACCRA": "Greater Accra", "GREATER ACCRA": "Greater Accra",
    "CENTRAL": "Central", "EASTERN": "Eastern", "WESTERN": "Western",
    "WESTERN NORTH": "Western North", "VOLTA": "Volta", "OTI": "Oti",
    "BONO": "Bono", "BONO EAST": "Bono East", "AHAFO": "Ahafo",
    "NORTHERN": "Northern", "SAVANNAH": "Savannah", "NORTH EAST": "North East",
    "UPPER EAST": "Upper East", "UPPER E": "Upper East", "UPPER E.": "Upper East",
    "UPPER WEST": "Upper West", "UPPER W": "Upper West", "UPPER W.": "Upper West",
}


def approved_url(value):
    url = urlparse(value)
    if (url.scheme != "https" or url.hostname not in {"ghanadistricts.com", "www.ghanadistricts.com"}
            or url.username or url.password or url.port not in (None, 443)):
        raise ValueError("Unapproved GhanaDistricts source URL")
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
        self.text = []

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == "tr" and re.fullmatch(r"grid_DXDataRow\d+", attrs.get("id", ""), re.I):
            if self.row is not None:
                raise ValueError("Unclosed source row")
            self.row = {"row_id": attrs["id"], "cells": [], "links": []}
        if self.row is not None:
            if tag == "td":
                if self.cell is not None:
                    raise ValueError("Nested source cell")
                self.cell = []
            if tag == "a" and attrs.get("href"):
                self.row["links"].append(attrs["href"])

    def handle_data(self, value):
        self.text.append(value)
        if self.cell is not None:
            self.cell.append(value)

    def handle_endtag(self, tag):
        if tag == "td" and self.cell is not None:
            self.row["cells"].append(" ".join("".join(self.cell).split()))
            self.cell = None
        if tag == "tr" and self.row is not None:
            if self.cell is not None:
                raise ValueError("Unclosed source cell")
            self.rows.append(self.row)
            self.row = None


def parse_table(html):
    table = DistrictTable()
    table.feed(html)
    table.close()
    if table.row is not None or table.cell is not None:
        raise ValueError("Truncated source table")
    summaries = set(re.findall(r"Page\s+(\d+)\s+of\s+(\d+)\s+\((\d+)\s+items?\)",
                               " ".join(table.text), re.I))
    # Fail rather than silently accepting page one if the source adds pagination.
    if len(summaries) != 1:
        raise ValueError("Missing or inconsistent source pagination summary")
    page, pages, count = map(int, summaries.pop())
    if page != 1 or pages != 1 or count != len(table.rows) or not 1 <= count <= 1000:
        raise ValueError("Incomplete source page; pagination/count needs review")
    records, rejected, identities, row_ids = [], [], set(), set()
    for row in table.rows:
        if row["row_id"] in row_ids:
            raise ValueError("Duplicate source row")
        row_ids.add(row["row_id"])
        if len(row["cells"]) != 5 or not all(row["cells"][:4]):
            raise ValueError("Unexpected MMDCE source columns")
        name, gender, region, district = row["cells"][:4]
        if gender.upper() not in {"MALE", "FEMALE"} or region.upper() not in REGIONS:
            raise ValueError("Unrecognized gender/region; source requires review")
        assembly = re.search(r"\b(Metropolitan|Municipal|District)\b", district, re.I)
        if not assembly:
            raise ValueError("Unrecognized assembly type")
        if re.search(r"\b(?:DCE|MCE|MMDCE|MCD|DCD|Chief Executive|Coordinating Director|Vacant|Not appointed)\b", name, re.I):
            rejected.append({"row_id": row["row_id"], "name": name,
                             "district": district, "reason": "office_placeholder_not_person"})
            continue
        if len(name) > 1000 or len(name.split()) < 2 or not all(part.isalpha() or part in " .'-’" for part in name):
            raise ValueError("Invalid person name; source requires review")
        links = [urljoin(SOURCE_URL, link) for link in row["links"]]
        links = [approved_url(link) for link in links
                 if re.fullmatch(r"/home/(?:AllDLeaders|DistrictLeaders)/\d+", urlparse(link).path, re.I)]
        if len(links) != 1:
            raise ValueError("Expected one district leader evidence link")
        identity = (name.casefold(), district.casefold())
        if identity in identities:
            raise ValueError("Duplicate person/assembly observation")
        identities.add(identity)
        records.append({"name": name, "region": REGIONS[region.upper()], "district": district,
                        "role": f"{assembly[1].title()} Chief Executive, {district}",
                        "source_reference": links[0]})
    if not records:
        raise ValueError("Source contains no named office holders")
    return records, rejected, count


def atomic_json(path, value):
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
    parser.add_argument("--input-html", type=Path, help="Replay retained HTML without network access")
    parser.add_argument("--output", type=Path, help="Manifest path (default also updates the legacy root copy)")
    args = parser.parse_args()
    if args.input_html:
        with args.input_html.open("rb") as source:
            body = source.read(MAX_BYTES + 1)
        acquisition = "retained_html_replay"
    else:
        request = Request(SOURCE_URL, headers={"User-Agent": "ITC-Screening-SourceCollector/1.0", "Accept": "text/html"})
        with build_opener(SourceRedirects()).open(request, timeout=40) as response:
            approved_url(response.url)
            if response.status != 200 or response.headers.get_content_type() != "text/html":
                raise ValueError("Expected a successful HTML response")
            body = response.read(MAX_BYTES + 1)
        acquisition = "live_html_observation"
    if len(body) > MAX_BYTES:
        raise ValueError("Source HTML exceeds bounded capture size")
    records, rejected, count = parse_table(body.decode("utf-8-sig"))
    digest = sha256(body).hexdigest()
    artifact = ROOT / ".local" / "mmdce-source" / f"{digest}.html"
    artifact.parent.mkdir(parents=True, exist_ok=True)
    artifact.write_bytes(body)
    manifest = {
        "schema_version": 2, "acquisition": acquisition,
        "observed_at": datetime.now(timezone.utc).isoformat(), "source_url": SOURCE_URL,
        "artifact_sha256": digest, "artifact_path": artifact.relative_to(ROOT).as_posix(),
        "complete_national_roster": False, "pages_fetched": 1, "source_rows": count,
        "records": records, "rejected_rows": rejected,
    }
    atomic_json(args.output or ROOT / "scripts" / "crawled_mmdces.json", manifest)
    if not args.output:
        atomic_json(ROOT / "crawled_mmdces.json", manifest)
    print(f"Collected {len(records)} named people from {count} visible rows; rejected {len(rejected)} office placeholders.")
    print(f"Partial national coverage; no database publication performed. HTML SHA256: {digest}")


if __name__ == "__main__":
    main()
