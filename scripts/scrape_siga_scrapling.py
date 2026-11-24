"""
Ghana State Interests and Governance Authority (SIGA - Act 990) & SOE Live Scraper
Powered by Scrapling (Selector) + curl_cffi for TLS-fingerprinted live extraction.

Extracts strictly live records from:
1. SIGA Board of Directors (https://siga.gov.gh/the-board/)
2. SIGA Executive Management (https://siga.gov.gh/management-team/)
3. GNPC Board & Executive (https://www.gnpcghana.com/about-us/board)

No prepopulated/hardcoded fallback lists are used. If a portal is unreachable or
returns zero records, it is logged and skipped; if zero total records are extracted
across all portals, the script exits non-zero.

Output: crawled_siga_peps.json
"""

import json
import os
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
    "Accept-Language": "en-US,en;q=0.9",
}


def fetch_page_with_scrapling(url: str, timeout: int = 15) -> str:
    """Fetch live web page using curl_cffi with Chrome 120 TLS fingerprint impersonation."""
    try:
        res = requests.get(url, impersonate="chrome120", headers=HEADERS, timeout=timeout)
        if res.status_code == 200:
            return res.text
        print(f"[WARN] Live fetch for {url} returned HTTP {res.status_code}")
    except Exception as e:
        print(f"[WARN] Live fetch failed for {url}: {e}")
    return ""


def parse_board_members(html: str) -> List[Dict[str, str]]:
    """Parse Board of Directors using Scrapling Selector."""
    if not html:
        return []
    sel = Selector(html)
    members = []
    for h in sel.css("h2, h3, h4"):
        name = h.text.strip()
        if name and len(name) > 4 and not any(
            k in name.lower()
            for k in ["board", "about", "resources", "links", "footer", "menu", "contact", "navigation", "quick"]
        ):
            role = "Board Member, State Interests and Governance Authority (SIGA)"
            if "chair" in name.lower():
                role = "Board Chair, State Interests and Governance Authority (SIGA)"
            elif "director" in name.lower():
                role = "Director-General, State Interests and Governance Authority (SIGA)"
            members.append({
                "name": name,
                "role": role,
                "status": "active",
                "source": "https://siga.gov.gh/the-board/",
            })
    return members


def parse_mgmt_members(html: str) -> List[Dict[str, str]]:
    """Parse Management Executives using Scrapling Selector."""
    if not html:
        return []
    sel = Selector(html)
    execs = []
    for h in sel.css("h2, h3, h4"):
        name = h.text.strip()
        if name and len(name) > 4 and not any(
            k in name.lower()
            for k in ["management", "about", "resources", "links", "footer", "menu", "contact", "navigation", "quick", "team"]
        ):
            execs.append({
                "name": name,
                "role": "Senior Executive, State Interests and Governance Authority (SIGA)",
                "status": "active",
                "source": "https://siga.gov.gh/management-team/",
            })
    return execs


def scrape_gnpc_board() -> List[Dict[str, str]]:
    """Scrape live Board of Directors from Ghana National Petroleum Corporation (GNPC)."""
    url = "https://www.gnpcghana.com/about-us/board"
    print(f"\n[*] Probing live SOE: GNPC Board ({url}) ...")
    html = fetch_page_with_scrapling(url, timeout=12)
    if not html:
        print("[WARN] Live GNPC probe unreachable/throttled; skipping GNPC.")
        return []

    sel = Selector(html)
    gnpc_members = []
    for p in sel.css("p"):
        txt = p.text.strip()
        if any(k in txt.lower() for k in ["board chairman", "board member", "chief executive"]):
            parent = p.xpath("..")
            if parent:
                name_parts = parent[0].xpath(".//h5//text()").getall()
                raw_name = " ".join("".join(name_parts).split())
                if raw_name:
                    gnpc_members.append({
                        "name": raw_name,
                        "role": f"{txt}, Ghana National Petroleum Corporation (GNPC)",
                        "status": "active",
                        "source": url,
                    })

    print(f"[+] Extracted {len(gnpc_members)} live GNPC Board Members via Scrapling.")
    return gnpc_members


def format_pep_record(idx: int, item: Dict[str, str], listed_date: str) -> Dict[str, Any]:
    """Format a PEP entry strictly adhering to sourcedata.Record whitelist."""
    name = item["name"]
    role = item["role"]
    status = item.get("status", "active")
    source_url = item.get("source", "https://siga.gov.gh")

    clean_name = (
        name.replace("Prof.", "")
        .replace("Dr.", "")
        .replace("Mr.", "")
        .replace("Mrs.", "")
        .replace("Ms.", "")
        .replace("Hon.", "")
        .replace(", Esq.", "")
        .strip()
    )
    aliases = [clean_name] if clean_name != name else []
    parts = clean_name.split()
    if len(parts) >= 2:
        aliases.append(parts[-1])

    return {
        "id": f"gh-pep-soe-{idx:04d}",
        "name": clean_name or name,
        "type": "Person",
        "aliases": sorted(set(a for a in aliases if a)),
        "source_reference": source_url,
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
            ],
            "listedAt": [listed_date],
            "sourceStatus": [
                f"Ghana PEP; Role: {role}; Status: {status}; Oversight: State Interests and Governance Authority (Act 990)"
            ],
            "sanctionsImposed": [
                "Enhanced Due Diligence (EDD) Required; AML Act 1044 Active PEP Screening"
            ],
            "address": ["Accra, Ghana"],
        },
    }


def main():
    print("========================================================================")
    print("    GHANA SOE & SIGA (ACT 990) LIVE PEP CRAWLER - SCRAPLING             ")
    print("========================================================================")

    board_url = "https://siga.gov.gh/the-board/"
    print(f"[*] Probing live SIGA Board of Directors: {board_url} ...")
    board_data = parse_board_members(fetch_page_with_scrapling(board_url, timeout=12))
    print(f"[+] Extracted {len(board_data)} live SIGA Board Members.")

    mgmt_url = "https://siga.gov.gh/management-team/"
    print(f"\n[*] Probing live SIGA Executive Management: {mgmt_url} ...")
    mgmt_data = parse_mgmt_members(fetch_page_with_scrapling(mgmt_url, timeout=12))
    print(f"[+] Extracted {len(mgmt_data)} live SIGA Management Executives.")

    gnpc_data = scrape_gnpc_board()

    all_people = []
    seen = set()
    for item in board_data + mgmt_data + gnpc_data:
        c_name = (
            item["name"]
            .replace("Prof.", "")
            .replace("Dr.", "")
            .replace("Mr.", "")
            .replace("Mrs.", "")
            .replace("Ms.", "")
            .replace("Hon.", "")
            .strip()
            .lower()
        )
        if c_name and c_name not in seen:
            seen.add(c_name)
            all_people.append(item)

    if not all_people:
        print("[ERROR] Zero SOE/SIGA records extracted from live sources. Refusing to write empty snapshot.")
        sys.exit(1)

    listed_date = time.strftime("%Y-%m-%d", time.gmtime())
    pep_records = [format_pep_record(idx, p, listed_date) for idx, p in enumerate(all_people, 1)]

    payload = {
        "version": 2,
        "source": "gh-pep-siga",
        "parser": "gh-pep-v1",
        "description": "State Interests and Governance Authority (Act 990) Live SOE Leadership PEPs",
        "last_crawled_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "total_records": len(pep_records),
        "records": pep_records,
    }

    output_path = "crawled_siga_peps.json"
    with open(output_path, "w", encoding="utf-8") as f:
        json.dump(payload, f, indent=2, ensure_ascii=False)
    print(f"[✓] Saved {len(pep_records)} live SOE / SIGA PEP records to {output_path}")

    scripts_path = os.path.join("scripts", "crawled_siga_peps.json")
    if os.path.isdir("scripts"):
        with open(scripts_path, "w", encoding="utf-8") as f:
            json.dump(payload, f, indent=2, ensure_ascii=False)
        print(f"[✓] Synced to {scripts_path}")


if __name__ == "__main__":
    main()
