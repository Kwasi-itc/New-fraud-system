"""
Active Live Scraper for Senior Political Party Officials in Ghana
Statutory Mandate: Anti-Money Laundering Act, 2020 (Act 1044), Section 59(1)(f) & FATF Rec 12
Powered by Scrapling (Selector) + curl_cffi for live extraction from Wikipedia infoboxes.

Extracts live leadership offices for:
1. New Patriotic Party (NPP)
2. National Democratic Congress (NDC)
3. Convention People's Party (CPP)
4. People's National Convention (PNC)

No prepopulated/hardcoded officials are used. If a page is unreachable or yields
no leadership entries, it is skipped with a warning; if zero total records are
extracted, the script exits non-zero.

Output: gh_pep_parties.json
"""

import json
import os
import re
import sys
import time
from typing import Any, Dict, List

try:
    import curl_cffi.requests as requests
    from scrapling import Selector
except ImportError:
    print("[ERROR] Required libraries missing. Run: pip install scrapling curl_cffi")
    sys.exit(1)

HEADERS = {
    "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
    "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
}

PARTIES_CONFIG = [
    {
        "code": "NPP",
        "name": "New Patriotic Party (NPP)",
        "wiki_url": "https://en.wikipedia.org/wiki/New_Patriotic_Party",
        "site_url": "https://newpatrioticparty.org",
    },
    {
        "code": "NDC",
        "name": "National Democratic Congress (NDC)",
        "wiki_url": "https://en.wikipedia.org/wiki/National_Democratic_Congress_(Ghana)",
        "site_url": "https://ndc.org.gh",
    },
    {
        "code": "CPP",
        "name": "Convention People's Party (CPP)",
        "wiki_url": "https://en.wikipedia.org/wiki/Convention_People%27s_Party",
        "site_url": "https://ec.gov.gh",
    },
    {
        "code": "PNC",
        "name": "People's National Convention (PNC)",
        "wiki_url": "https://en.wikipedia.org/wiki/People%27s_National_Convention_(Ghana)",
        "site_url": "https://ec.gov.gh",
    },
]


def clean_wiki_text(text: str) -> str:
    cleaned = re.sub(r"\[\d+\]", "", text)
    cleaned = re.sub(r"\(disputed.*?\)", "", cleaned, flags=re.IGNORECASE)
    return " ".join(cleaned.split()).strip()


def scrape_party_live(p_cfg: Dict[str, Any]) -> List[Dict[str, Any]]:
    code = p_cfg["code"]
    name = p_cfg["name"]
    wiki_url = p_cfg["wiki_url"]

    print(f"[*] Actively scraping live {code} leadership from {wiki_url} ...")
    try:
        res = requests.get(wiki_url, impersonate="chrome120", headers=HEADERS, timeout=15)
    except Exception as exc:
        print(f"[WARN] Live fetch failed for {wiki_url}: {exc}")
        return []

    if res.status_code != 200:
        print(f"[WARN] HTTP {res.status_code} for {wiki_url}")
        return []

    scraped_officials: List[Dict[str, Any]] = []
    sel = Selector(res.text)
    infobox = sel.css("table.infobox")
    if infobox:
        for tr in infobox[0].css("tr"):
            th = tr.css("th")
            td = tr.css("td")
            if th and td:
                label = clean_wiki_text(" ".join("".join(th[0].xpath(".//text()").getall()).split()))
                raw_lines = [clean_wiki_text(x) for x in td[0].xpath(".//text()").getall()]
                val = "\n".join(x for x in raw_lines if x)

                if not val or len(val) < 3:
                    continue

                if any(k in label.lower() for k in ["leader", "chair", "secretary", "president", "candidate"]):
                    for line in val.split("\n"):
                        clean_line = line.strip()
                        if len(clean_line) < 3:
                            continue
                        scraped_officials.append({
                            "name": clean_line,
                            "role": f"{label}, {name}",
                            "party": name,
                            "partyCode": code,
                            "ref": wiki_url,
                            "status": "active",
                        })

    print(f"[+] Extracted {len(scraped_officials)} live leadership offices from {code} infobox.")
    return scraped_officials


def format_party_record(idx: int, item: Dict[str, Any], listed_date: str) -> Dict[str, Any]:
    name = item["name"]
    role = item["role"]
    status = item["status"]
    party = item["party"]
    party_code = item["partyCode"]
    ref_url = item["ref"]

    clean_name = (
        re.sub(r"\(.*?\)", "", name)
        .replace("Hon.", "")
        .replace("Dr.", "")
        .replace("Lawyer", "")
        .replace(", Esq.", "")
        .strip()
    )
    aliases = [clean_name] if clean_name != name else []
    parts = clean_name.split()
    if len(parts) >= 2:
        aliases.append(parts[-1])

    return {
        "id": f"gh-pep-party-{idx:04d}",
        "name": clean_name or name,
        "type": "Person",
        "aliases": sorted(set(a for a in aliases if a)),
        "source_reference": ref_url,
        "source_type": "pep_evidence",
        "properties": {
            "country": ["gh"],
            "nationality": ["gh"],
            "countryText": ["Ghana"],
            "nationalityText": ["Ghanaian"],
            "programs": [
                "Politically Exposed Person (PEP)",
                "Active Office Holder",
                role,
                f"Political Party: {party}",
            ],
            "listedAt": [listed_date],
            "sourceStatus": [
                f"Ghana PEP; Role: {role}; Status: {status}; Party: {party_code}; Statutory Mandate: Act 1044 Sec 59(1)(f)"
            ],
            "sanctionsImposed": [
                "Enhanced Due Diligence (EDD) Required; AML Act 1044 Active PEP Screening (Section 59(1)(f))"
            ],
            "address": ["Ghana"],
        },
    }


def main():
    print("========================================================================")
    print("   ACTIVE LIVE SCRAPER: SENIOR POLITICAL PARTY OFFICIALS (ACT 1044)     ")
    print("========================================================================")

    all_officials: List[Dict[str, Any]] = []
    for cfg in PARTIES_CONFIG:
        all_officials.extend(scrape_party_live(cfg))
        time.sleep(1)

    unique_officials: List[Dict[str, Any]] = []
    seen = set()
    for o in all_officials:
        norm_name = re.sub(r"\(.*?\)", "", o["name"]).replace("Dr.", "").replace("Hon.", "").strip().lower()
        if norm_name and norm_name not in seen:
            seen.add(norm_name)
            unique_officials.append(o)

    if not unique_officials:
        print("[ERROR] Zero political party officials extracted from live sources. Refusing to write empty snapshot.")
        sys.exit(1)

    listed_date = time.strftime("%Y-%m-%d", time.gmtime())
    pep_records = [format_party_record(idx, o, listed_date) for idx, o in enumerate(unique_officials, 1)]

    payload = {
        "version": 2,
        "source": "gh-pep-parties",
        "parser": "gh-pep-v1",
        "records": pep_records,
    }

    out_root = "gh_pep_parties.json"
    with open(out_root, "w", encoding="utf-8") as f:
        json.dump(payload, f, indent=2, ensure_ascii=False)
    print(f"\n[✓] Saved {len(pep_records)} live Senior Party PEP records to {out_root}")

    out_scripts = os.path.join("scripts", "gh_pep_parties.json")
    if os.path.isdir("scripts"):
        with open(out_scripts, "w", encoding="utf-8") as f:
            json.dump(payload, f, indent=2, ensure_ascii=False)
        print(f"[✓] Synced to {out_scripts}")


if __name__ == "__main__":
    main()
