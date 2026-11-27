# MMDCE collection and local screening publication

The old `scrape-live-mmdces.mjs` copied the compiled local fixture and incorrectly
reported 17 fetched pages. The compiler never consumed actual web observations.
This is why Wisdom Bondieu Seade was missing despite appearing on GhanaDistricts.

- [x] Fetch and parse the actual HTTPS directory using Python's standard HTML parser.
- [x] Retain the downloaded HTML under ignored `.local/mmdce-source/<sha256>.html`
  and record its hash, observation time, source URL and acquisition mode in
  `scripts/crawled_mmdces.json`. The root `crawled_mmdces.json` is a compatibility copy.
- [x] Check the advertised page/row count, expected columns, regions and evidence
  links. Fail on changed pagination, malformed/empty pages or fetch errors. Preserve
  the last good manifest when collection fails.
- [x] Exclude office placeholders such as `North Dayi DCE` and `Kwabre East MCD`.
- [x] Merge observations into the canonical `gh_pep_local_gov.json` build. Keep the
  existing baseline IDs; add deterministic person-and-assembly IDs for new people.
  Match existing names/explicit aliases only within the same assembly; ambiguous
  identity matches stop the build. No fuzzy identity deduplication is performed.
- [x] Preserve previously observed people when absent from a later visible batch.
  Absence from this partial source is not evidence of resignation or withdrawal.
- [x] Preserve Emmanuel Gemegah's identity and mark his Keta office as former,
  citing the [Assembly farewell notice](https://ketama.gov.gh/2024/12/20/keta-municipal-assembly-holds-end-of-year-durbar-and-bids-farewell-to-mce/).
  Do not invent an appointment date or statutory cooling-off period for this correction.
- [ ] Verify a complete current national roster and reconcile the remaining curated
  baseline offices, dates, aliases and legal labels against authoritative evidence.

## Run and review

From the repository root, with Node and Python 3.10+ available:

```powershell
node scripts/scrape-live-mmdces.mjs
node scripts/compile-all-261-mmdces.mjs
git diff -- gh_pep_local_gov.json scripts/crawled_mmdces.json
```

`SCREENING_PYTHON` can select a Python executable. For an offline replay, pass
`--input-html <retained-file>`; its acquisition mode explicitly identifies a replay.
Collection alone never publishes to the database. `scripts/mmdce-coverage.json`
reports the capture, rejected rows and merge results. Rebuilding unchanged records
must preserve IDs and canonical content.

After review, publish **only** `gh-pep-local-gov` using the private screening-owned
`source-import` command with `-file`, `-dataset gh-pep-local-gov` and an explicit
`-snapshot-reference` identifying the capture hash and coverage limitations.
It requires an explicitly configured `SOURCE_DATABASE_URL` and an already approved
source. The importer retains the canonical JSON artifact, indexes names and publishes
atomically with its receipt. This is local snapshot intake; collection is paused
and fenced, and it does not constitute source-rights approval or scheduled live collection.
The master seed command's `--crawl` option now refreshes observations before compiling.

### Publish existing snapshots to RDS

The master command can publish its eight existing root-level JSON snapshots to the
running EC2 screening service's database:

```sh
node -- scripts/seed-all-screening-data.mjs \
  --env-file .local/new-fraud-system/compose.env --confirm-write
```

The `--` prevents Node from consuming `--env-file` as its own option. Use the
active Compose env file and, if necessary, add `--project <name>`. Its
`DATABASE_URL` must exactly match the running screening service's configuration.
The host needs Docker, the running screening service, and a locally available
`postgres:16` image. The database role needs source publisher permissions. The
command requires every listed dataset to be registered and approved with a
compatible connector and parser; it creates no sources or grants. Existing
tenant grants determine who can use the published datasets.

RDS mode does not crawl or rebuild files: review the snapshots, including
`worldbank_fixture.json`, before running it. It rejects `--crawl`. The separate
local crawl/build workflow can change snapshots; specifically, use the canonical
MMDCE compiler above instead of the master script's subnational builder when
retaining the curated baseline matters.

Each import publishes separately and pauses/fences collection for that source.
A later failure can leave earlier datasets published; inspect the reported
completed list and reconcile before retrying. The script does not change rights
approvals, grants, or unrelated connections. Do not share the env file or database
credentials. This workflow has not been validated against your EC2 deployment.

## Verification on 2026-11-20

The captured [visible table](https://www.ghanadistricts.com/home/AllDistricts)
advertised one page with 20 rows: 16 named observations and four office placeholders.
It does **not** establish coverage of all Ghana assemblies. Wisdom's office is also
listed by the [Keta Municipal Assembly](https://ketama.gov.gh/the-mce/).

The reviewed local build has 331 records: all 328 baseline identities remain, three
source-listed records were added, and only Emmanuel's existing status was corrected.
New source-listed records preserve the directory spelling and office evidence; they
do not assert unverified birth dates, appointment dates, nationality or legal sanctions.
Source-listed records may still require human identity reconciliation with the baseline.

```powershell
python -m unittest discover -s scripts/tests -p test_crawl_mmdces.py -v
node --test scripts/tests/mmdce-observations.test.mjs
```

Six parser/failure-preservation tests and six merge/identity tests pass. A second real
canonical rebuild is byte-identical. The shared local fraud catalog was published
through the guarded importer; no standalone catalog or unrelated source was imported.
A read-only live-provider check, using revalidated existing user authority and all
11 entitled lists selected at threshold 70, returned exact-name candidates for all
three additions and Emmanuel. It verified Wisdom's specific office, stable record ID
and current publication version in match evidence, plus Emmanuel's former-office
wording. The previous 328-record publication is retained. This checks retrieval and
provenance, not matching-quality acceptance or national coverage.
