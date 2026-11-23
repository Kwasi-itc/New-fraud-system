BEGIN;

-- 1. Register All Ghana Sovereign Datasets
INSERT INTO screening.source_datasets(owner_scope, id, label, category, enabled)
VALUES
  ('00000000-0000-0000-0000-000000000000', 'gh-pep-executive', 'Ghana Executive PEPs', 'pep_evidence', true),
  ('00000000-0000-0000-0000-000000000000', 'gh-pep-parliament', 'Ghana Parliament PEPs', 'pep_evidence', true),
  ('00000000-0000-0000-0000-000000000000', 'gh-pep-soe', 'Ghana SOE and Judiciary PEPs', 'pep_evidence', true),
  ('00000000-0000-0000-0000-000000000000', 'gh-pep-rca', 'Ghana PEP Relatives and Associates', 'pep_evidence', true),
  ('00000000-0000-0000-0000-000000000000', 'gh-pep-local-gov', 'Ghana Sub-National & Local Gov PEPs', 'pep_evidence', true),
  ('00000000-0000-0000-0000-000000000000', 'gh-pep-parties', 'Ghana Political Party Leadership PEPs', 'pep_evidence', true),
  ('00000000-0000-0000-0000-000000000000', 'gh-pep-judiciary', 'Ghana Judicial Service Supreme Court PEPs', 'pep_evidence', true),
  ('00000000-0000-0000-0000-000000000000', 'worldbank-debarred', 'World Bank Ineligible Firms & Individuals', 'regulatory_warning', true)
ON CONFLICT (owner_scope, id) DO UPDATE SET enabled=true, label=excluded.label;

-- 2. Configure Source Connections & Legal Approval References
INSERT INTO screening.source_connections(owner_scope, dataset_id, connector, config, enabled, rights_state, rights_review_reference, max_stale)
VALUES
  ('00000000-0000-0000-0000-000000000000', 'gh-pep-executive', 'gh-pep-executive', '{"parser":"gh-pep-v1"}', true, 'approved', 'Ghana Official Executive Gazette and Presidency Appointments', interval '30 days'),
  ('00000000-0000-0000-0000-000000000000', 'gh-pep-parliament', 'gh-pep-parliament', '{"parser":"gh-pep-v1"}', true, 'approved', 'Parliament of Ghana Official Members Roster', interval '30 days'),
  ('00000000-0000-0000-0000-000000000000', 'gh-pep-soe', 'gh-pep-soe', '{"parser":"gh-pep-v1"}', true, 'approved', 'Ghana State Owned Enterprises, BoG, and Public Authorities Records', interval '30 days'),
  ('00000000-0000-0000-0000-000000000000', 'gh-pep-rca', 'gh-pep-rca', '{"parser":"gh-pep-v1"}', true, 'approved', 'Ghana Official AML/FATF Relative and Close Associate Disclosures', interval '30 days'),
  ('00000000-0000-0000-0000-000000000000', 'gh-pep-local-gov', 'gh-pep-local-gov', '{"parser":"gh-pep-v1"}', true, 'approved', 'Ministry of Local Government and Regional Coordinating Councils', interval '30 days'),
  ('00000000-0000-0000-0000-000000000000', 'gh-pep-parties', 'gh-pep-parties', '{"parser":"gh-pep-v1"}', true, 'approved', 'Electoral Commission of Ghana Certified Political Parties Register', interval '30 days'),
  ('00000000-0000-0000-0000-000000000000', 'gh-pep-judiciary', 'gh-pep-judiciary', '{"parser":"gh-pep-v1"}', true, 'approved', 'Judicial Service of Ghana Official Supreme Court and Appellate Roll', interval '30 days'),
  ('00000000-0000-0000-0000-000000000000', 'worldbank-debarred', 'worldbank-debarred', '{"parser":"worldbank-v1"}', true, 'approved', 'World Bank Procurement Sanctions Register', interval '30 days')
ON CONFLICT (owner_scope, dataset_id) DO UPDATE SET enabled=true, rights_state='approved', max_stale=interval '30 days';

-- 3. Grant Access to All Current Tenants
INSERT INTO screening.source_grants(tenant_id, owner_scope, dataset_id)
SELECT t.tenant_id, d.owner_scope, d.id
FROM screening.source_datasets d
CROSS JOIN (
  SELECT DISTINCT id AS tenant_id FROM core.tenants
  UNION
  SELECT DISTINCT tenant_id FROM screening.source_grants
) t
WHERE d.id IN ('gh-pep-executive', 'gh-pep-parliament', 'gh-pep-soe', 'gh-pep-rca', 'gh-pep-local-gov', 'gh-pep-parties', 'gh-pep-judiciary', 'worldbank-debarred')
ON CONFLICT (tenant_id, owner_scope, dataset_id) DO NOTHING;

-- Local snapshot readiness is recorded by source-import -snapshot-reference
-- together with its publication and intake receipt. Registration must not
-- refresh unrelated sanctions feeds or overwrite quarantine/freshness state.

COMMIT;
