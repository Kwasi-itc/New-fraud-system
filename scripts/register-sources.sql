BEGIN;
INSERT INTO screening.source_datasets(owner_scope, id, label, category, enabled)
VALUES('00000000-0000-0000-0000-000000000000', 'worldbank-debarred', 'World Bank Ineligible Firms & Individuals', 'regulatory_warning', true)
ON CONFLICT (owner_scope, id) DO NOTHING;

INSERT INTO screening.source_connections(owner_scope, dataset_id, connector, config, enabled, rights_state, rights_review_reference)
VALUES('00000000-0000-0000-0000-000000000000', 'worldbank-debarred', 'worldbank-debarred', '{"parser":"worldbank-v1"}', true, 'approved', 'World Bank Procurement Sanctions')
ON CONFLICT (owner_scope, dataset_id) DO UPDATE SET enabled=true, rights_state='approved';
COMMIT;

