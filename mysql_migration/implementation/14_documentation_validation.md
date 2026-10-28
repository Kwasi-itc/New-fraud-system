# Documentation validation

Date: 2026-10-22. Scope: implementation design documents only. Baseline source is the original audit snapshot at commit `a2d241a53e082b1ff654634c41e83ecfb7328941` plus its recorded existing working-tree changes. No application code, configuration, schema, dependency, migration or runtime was changed by this phase.

## Checks completed

- 25 Markdown documents: 16 workspace-level files including README and this report, plus nine module plans.
- 35 audit finding families have explicit replacement, ADR, module, task and validation coverage in document 12.
- 17 replacement designs retain the audit's exact source evidence; 12 ADRs remain proposed.
- 88 application table declarations have proposed physical mapping entries; later ALTERs and exact final target DDL remain SC-01 acceptance work, not silently omitted constraints.
- 17 implementation tasks remain NOT_STARTED; dependency references resolve and graph cycle check passes. Nine module checklists contain 36 unchecked subordinate items.
- 11 POC specifications and nine performance workloads remain unexecuted. No skipped test is counted as a pass.
- 424 local file/source-line links checked before this report was finalized: zero missing targets and zero out-of-range line references. A missing planned crosswalk link during drafting was resolved by creating document 12.
- All 1,668 source SHA-256 values in the audit manifest match. All 17 existing audit documents match the preserved hashes below.
- Existing historical plans were read and preserved, including the explicit October14 implementation revert. No prior prototype result is represented as a current passing test.
- Official MySQL/driver/River/Keycloak sources consulted; reference register distinguishes engine facts from application-specific proposals.

These checks establish artifact coverage, navigability and unchanged-source boundaries. They do not certify each proposed algorithm, every SQL rewrite, live privileges, installed dependency schema, business acceptance or production readiness. Link validity is not semantic proof.

## Remaining acceptance work

Live schema/data/grants/provider discovery, exact target version/hosting decisions, query compilation, real-engine tests, fault/concurrency proofs, full target column-level manifest, benchmarks, backup restore, provider migration and cutover rehearsal are NOT_STARTED. POCs may require revising these recommendations. Owner/security/operations review is required before ADR acceptance. Numeric SLOs and recovery targets must be agreed rather than inferred from historical stress reports.

Only documentation in this directory was created/modified. Original audit files were not updated because the design adds recommendations and separately qualified historical performance evidence rather than correcting a verified audit error.

## Preserved audit baseline

The hashes below were captured before writing implementation design content and rechecked at completion. Source-file hashes remain in the audit's existing file manifest.

```json
{
  "mysql_migration/01_system_inventory.md": "d199bf559696ed980994315efa11831142a9e79ff4b495f4f6a258a527103664",
  "mysql_migration/02_postgresql_usage_inventory.md": "244efd5db5837dd68909064d9f35dd0e6b8c70cabcd4f7a679e6adf1cc6bd14c",
  "mysql_migration/03_database_architecture_and_data_model.md": "1eb3b81b7e931e5985c0143b8c189a28d6184b680879b0515d28068f7d654456",
  "mysql_migration/04_service_by_service_analysis.md": "00266b577af858ea6a883886dc203fba4e2fb24e75dde7b2b80decbdf9e44049",
  "mysql_migration/05_postgresql_to_mysql_compatibility.md": "34f623f726ee051a33963769e2d5e25a4d5db66784945be76d56fe10c677eee7",
  "mysql_migration/06_schema_and_data_migration_plan.md": "3ed3bf31d401ca5b45b659d73b7c6c26429500e08f9e1dba1b1cf5ecbd6521c6",
  "mysql_migration/07_risks_dependencies_and_open_questions.md": "62d0db924cb2e432316830d63edbbb8b705a4da92ddca216f5aa45947edaf96e",
  "mysql_migration/08_testing_and_validation_strategy.md": "f2ab351f5c48078a52785e376a83103186997c756da33f1b3c79359e39a3102e",
  "mysql_migration/09_migration_roadmap_and_checklist.md": "882ca2465e6dc4735539879233eccc0f9957f577df78dd14aa457ecc7b536e28",
  "mysql_migration/10_findings_and_recommendations.md": "8c332016ad36d7b9d92905c3cc07c55ea0e2ab179ff49ae623900cb23a637237",
  "mysql_migration/11_source_evidence_catalog.md": "6050211e0106bb2467b6e319f31243ff946ea3688644066af304c049d2335f0f",
  "mysql_migration/12_schema_source_catalog.md": "2a1557fc886be67dbd2fb43191a1f5d0497941ec78dbabf7ada2f67442cc8fad",
  "mysql_migration/13_file_coverage_manifest.md": "634c9225e17db7f49d325bb5004a03f563b9d6a6f33a163a3d8069bd5349fa13",
  "mysql_migration/14_schema_object_index.md": "f3e47d7da5da38e269ee401b3bbdc577a8c078f7daa0c6eade1f24892d675ccc",
  "mysql_migration/15_coverage_and_validation.md": "6da5eb118af7b0677465092fd514ca00bd0ea8bb22342c4bcd0e71ac5dbd8393",
  "mysql_migration/16_database_callsite_index.md": "c73a6914e0c89367baade7c56f9276b059bf76176e1b60664ba692a9fedd82d2",
  "mysql_migration/README.md": "93cdf1a350f50716dfae1794380fdda145285b56111e156ba8558c197b7f3788"
}
```
