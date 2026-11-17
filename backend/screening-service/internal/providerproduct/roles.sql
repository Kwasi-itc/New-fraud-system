-- Explicit owner-run deployment contract. No public HTTP role administration.
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='screening_provider_reader') THEN CREATE ROLE screening_provider_reader NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE; END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='screening_provider_collector') THEN CREATE ROLE screening_provider_collector NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE; END IF;
END $$;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname IN('screening_provider_reader','screening_provider_collector') AND (rolcanlogin OR rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls)) THEN
  RAISE EXCEPTION 'Existing provider capability role has unexpected privileges';
 END IF;
END $$;
GRANT USAGE ON SCHEMA screening,provider_api TO screening_provider_reader;
GRANT SELECT ON provider_api.installation,provider_api.clients,provider_api.credentials,provider_api.client_datasets TO screening_provider_reader;
GRANT SELECT,INSERT,UPDATE ON provider_api.usage_minutes TO screening_provider_reader;
GRANT SELECT ON screening.source_datasets,screening.source_versions,screening.source_records,screening.source_names,screening.source_identifiers,screening.source_grants,screening.source_publications,screening.source_readiness_v1,screening.private_lists TO screening_provider_reader;
GRANT SELECT,INSERT ON screening.source_read_cursors TO screening_provider_reader;
DO $$ BEGIN
 IF to_regclass('core.tenants') IS NOT NULL THEN
  GRANT USAGE ON SCHEMA core TO screening_provider_reader;
  GRANT SELECT(id,status) ON core.tenants TO screening_provider_reader;
 END IF;
END $$;
GRANT USAGE ON SCHEMA screening TO screening_provider_collector;
GRANT SELECT,INSERT,UPDATE ON screening.source_connections,screening.source_import_runs,screening.source_datasets,screening.source_versions TO screening_provider_collector;
GRANT SELECT ON screening.source_grants,screening.source_readiness_v1,screening.source_retention_policies,screening.source_retention_holds,screening.private_imports TO screening_provider_collector;
GRANT SELECT,INSERT,DELETE ON screening.source_records,screening.source_names,screening.source_identifiers,screening.source_artifacts,screening.source_version_artifacts TO screening_provider_collector;
GRANT UPDATE ON screening.source_version_artifacts TO screening_provider_collector;
-- Artifact retention claims use FOR UPDATE SKIP LOCKED before deletion.
GRANT UPDATE ON screening.source_artifacts TO screening_provider_collector;
GRANT SELECT,INSERT ON screening.source_publications,screening.source_lifecycle_audit TO screening_provider_collector;
GRANT USAGE ON SEQUENCE screening.source_publications_id_seq,screening.source_lifecycle_audit_id_seq TO screening_provider_collector;
GRANT EXECUTE ON FUNCTION screening.refresh_source_statistics_v1() TO screening_provider_collector;
-- Shared mode's publication trigger inserts its durable workflow handoff.
DO $$ BEGIN
 IF to_regclass('screening.source_publication_delivery') IS NOT NULL THEN
  GRANT INSERT ON screening.source_publication_delivery TO screening_provider_collector;
 END IF;
END $$;
