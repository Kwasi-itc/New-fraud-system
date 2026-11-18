BEGIN;
INSERT INTO screening.source_datasets(owner_scope, id, label, category, enabled)
VALUES
  ('00000000-0000-0000-0000-000000000000', 'gh-pep-local-gov', 'Ghana Sub-National & Local Gov PEPs', 'pep_evidence', true),
  ('00000000-0000-0000-0000-000000000000', 'gh-pep-parties', 'Ghana Political Party Leadership PEPs', 'pep_evidence', true)
ON CONFLICT (owner_scope, id) DO UPDATE SET enabled=true, label=excluded.label;

INSERT INTO screening.source_connections(owner_scope, dataset_id, connector, config, enabled, rights_state, rights_review_reference)
VALUES
  ('00000000-0000-0000-0000-000000000000', 'gh-pep-local-gov', 'gh-pep-local-gov', '{"parser":"gh-pep-v1"}', true, 'approved', 'Ministry of Local Government and Regional Coordinating Councils'),
  ('00000000-0000-0000-0000-000000000000', 'gh-pep-parties', 'gh-pep-parties', '{"parser":"gh-pep-v1"}', true, 'approved', 'Electoral Commission of Ghana Certified Political Parties Register')
ON CONFLICT (owner_scope, dataset_id) DO UPDATE SET enabled=true, rights_state='approved';

INSERT INTO screening.source_grants(tenant_id, owner_scope, dataset_id)
SELECT t.tenant_id, d.owner_scope, d.id
FROM screening.source_datasets d
CROSS JOIN (SELECT DISTINCT tenant_id FROM screening.source_grants) t
WHERE d.id IN ('gh-pep-local-gov', 'gh-pep-parties')
ON CONFLICT (tenant_id, owner_scope, dataset_id) DO NOTHING;

COMMIT;
