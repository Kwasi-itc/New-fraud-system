# Phase 8 matching investigation and evaluation

Evaluated 2026-11-14/15 UTC. Existing tasks: MAT-01, QLT-01, RSK-01. **Quality acceptance remains blocked.** Frozen name scorers, corpora and runtime thresholds were preserved. The work adds corpus audits, diagnostic slices and calibration feasibility checks; it does not relabel the holdout or deploy a new scoring policy.

## Measurements and acceptance shortfall

The same 4,800-query/240-family v2 corpus was evaluated against both existing name profiles. Holdout contains 2,400 queries: 1,182 determinate positives, 1,176 determinate negatives and 42 indeterminate rows. Precision is candidate-level identity precision; FPR/FNR are query-level rates. They have different denominators and must not be conflated.

| Profile / threshold | Retrieval recall | Scored identity recall | Precision | Query FPR | Query FNR | Partial queries |
| --- | --- | --- | --- | --- | --- | --- |
| Existing name-fuzzy-v1 / calibration F1 threshold 80 | 100% | 96.954% | 24.285% | 10.289% | 3.046% | 60 |
| Existing name-token-v2 / calibration F1 threshold 79 | 100% | 97.124% | 24.265% | 10.544% | 2.876% | 60 |
| Either name profile / unchanged request default 85 | 100% | 89.171% | 23.569% | 2.381% | 10.829% | 60 |
| Proposed engineering floors | ≥99.5% | ≥99% | ≥95% | ≤1% | ≤1% | 0 |

The Phase 8 remeasurements reproduce the earlier failing result, rather than establish an accuracy improvement. At v2 threshold 79, recall is 1.876 percentage points below its floor, precision 70.735 points below, FPR 9.544 points above, and FNR 1.876 points above. Raising the threshold to 85 reduces query false alarms but loses substantial recall and still fails precision/completeness. **No threshold from 50 through 100 meets all proposed point floors on calibration.** No accepted threshold is selected; runtime configuration remains unchanged.

Candidate retrieval and final classification are measured separately. Retrieval finds all 1,182 expected holdout identities before classification. The independent small-corpus trigram oracle checks 40 development queries across ten strata and finds no above-threshold retrieval losses. That limited oracle is not proof of recall on official data. The 50-result presentation bound produces 60 incomplete holdout cases; an incomplete result remains an error at decision time, never a clean decision. Wilson intervals in raw reports are descriptive only: repeated families and cross-partition collisions invalidate a claim of independent-query acceptance confidence.

## Corpus truth and independence audit

The read-only audit of unchanged v2 bytes found:

- 137 groups of identical complete inputs with conflicting determinate labels, involving 443 queries.
- 368 identical-input groups spanning development/calibration/holdout, despite family-ID separation.
- 84 exact normalized name/alias groups shared by different records; 59 span family partitions.
- 337 determinate positive queries whose exact name/alias is also attached to another record.
- Zero invalid relevant-record references; structural referential integrity is not the same as correct identity truth.
- 4,320 queries with neither identifiers nor date evidence. The corpus has no adequate conflicting-identifier negative stratum.
- Synthetic category labels are confounded with entity type: sanctions/regulatory rows are Person; PEP/watchlist rows are Organization. `quality-global`/`quality-private` are fictional dataset labels, not official sources.

These defects mean a deterministic name-only request can be expected to return different identities depending on a hidden family label unavailable to the provider. Other rows assign the same synthetic native-script alias to distinct identities. Exact alias matches then count as false positives against single-family labels, although the supplied evidence cannot distinguish those records. This is a failure of the evaluation's determinacy/independence assumptions, alongside genuine fuzzy ambiguity; it must not be hidden by deleting difficult aliases or changing holdout truth.

## False-positive investigation

The diagnostic counts below include **all determinate partitions**, at v2's existing calibration threshold 79. They explain 7,162 false-positive candidate observations, not 7,162 unique people or negative queries. Slices overlap; never sum the rows.

| Slice | False-positive candidates | Interpretation |
| --- | --- | --- |
| Fictional global / private candidate datasets | 2,407 / 4,755 | Source labels do not establish OFAC/UN/UK error rates |
| Organization / Person | 6,198 / 964 | Heavily confounded with alias/category generation |
| Exact normalized strategy | 6,438 | Approximately 90% of false-positive candidate observations already have an exact normalized name/alias |
| Alias / primary-name match | 6,658 / 504 | Shared aliases dominate; suppressing all aliases would discard legitimate recall |
| Synthetic transliteration-alias stratum | 6,372 | Native-script aliases are supplied by the fixture; no automatic transliteration model is assessed |
| Common token in ≥12 retrieved records | 6,729 | Commonality is diagnostic only; it does not tune scoring |
| Query names 6–15 / ≥16 runes | 3,755 / 3,407 | No ≤5-rune acceptance coverage; length alone does not resolve shared exact aliases |
| No identifier/date query | 7,162 | Agreement rows contribute 240 true-positive observations and no false positives; conflict quality is unassessed here |
| Edit / token-capped edit / partial-token / character strategies | 181 / 119 / 415 / 9 | Secondary fuzzy ambiguity remains, but is not the dominant source of the poor candidate precision |

The report also retains entity/category/source/strategy/alias/commonality/length/identifier slices, development-only examples and default-85 comparisons. No holdout examples were used to tune a new algorithm. Independent adjudication must determine whether a shared alias is a valid review candidate, an ambiguous identity group, a synthetic alias mistake, or a true wrong-identity result.

## Separate identifier calibration

A separate executable engineering evaluation uses 90 fictional identity families and 540 queries, with 30 families/180 queries in each development/calibration/holdout partition. Names and typed identifiers are disjoint between families/partitions. Generation rules are shared, so this is **not** independent real-world identity evidence.

The unchanged `identifier-evidence-v1` profile is tested separately for exact passport/LEI/IMO retrieval without overlapping names, outer whitespace, different values/leading zeros, different namespaces, exact-name missing identifiers and same-name identifier conflicts. Same-name/different-passport cases remain indeterminate: multiple passports can belong to one person. Missing local namespaces are rejected by request validation and tested separately, rather than counted as easy true negatives. LEI/IMO checksum validation is not claimed.

At threshold 85 each partition has 90/90 positive identities retrieved/classified, 0/80 negative-query false positives, zero partial/error cases, and ten indeterminate queries excluded. All thresholds 50–100 satisfy the **point estimates** on this mechanical fixture, so it does not identify a discriminating threshold. `accepted_threshold` stays null. The holdout's descriptive precision/recall lower bound is approximately 95.91%, retrieval below the 99.5% floor, and the false-positive upper bound approximately 4.58%, above the 1% ceiling. Family correlation and artificial names further limit these intervals. **The identifier profile has separate calibration evidence now, but remains unaccepted for production quality.**

## Justified remediation and next exact actions

Engineering remediation consists of exposing conflicting truth/partition leakage, separating retrieval/classification and name/identifier calibration, retaining ambiguous labels, and refusing an infeasible acceptance threshold. Changing an exact-name/alias scorer to satisfy contradictory hidden-family labels is not justified. Both frozen name scorers and both original corpora retain their recorded SHA-256 values; the audit script never writes them.

Before quality acceptance:

1. The risk/data owners must approve review-candidate versus proven-identity label semantics, treatment of aliases, transliterations, multiple IDs and incomplete outputs. A screening hit remains evidence for review, not automatic identity proof.
2. Obtain authorized representative source and tenant examples, including source/type/category balance, near-neighbor negative names, native-script/transliterated aliases, short/common names, identifier availability/missingness/conflicts and realistic prevalence.
3. Have two independent reviewers label candidates, adjudicate disagreements and preserve provenance. Group equivalent requests, names/aliases and identity clusters before assigning partitions; do not merely split family IDs. Freeze a **new** version and hashes, leaving v1/v2 unchanged. This already-observed holdout cannot become unseen again.
4. Develop any new scorer on development data only; select a policy on calibration under the approved recall/precision/FPR/FNR/completeness and uncertainty constraints, then freeze before a once-only unseen evaluation. If no policy is feasible, keep the gate closed and report the shortfall.
5. Qualify name and identifier profiles independently, with supported/unsupported slices explicit, before granting any production quality acceptance.

Machine evidence is indexed by the [acceptance register](INTERNAL_PROVIDER_ACCEPTANCE.md). No official-source accuracy, production threshold, commercial permission or activation approval follows from these tests.
