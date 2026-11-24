-- Reviewed deployment template. Run as migration owner on the shared logical
-- database AFTER screening 000022 and auth schema v4. No passwords/login roles,
-- production execution, data grants, source activation or role memberships here.
BEGIN;
SET LOCAL lock_timeout='5s';
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='screening_source_reader') THEN CREATE ROLE screening_source_reader NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE; END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='screening_private_writer') THEN CREATE ROLE screening_private_writer NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE; END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='screening_source_collector') THEN CREATE ROLE screening_source_collector NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE; END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='screening_notice_operator') THEN CREATE ROLE screening_notice_operator NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE; END IF;
END $$;
GRANT USAGE ON SCHEMA screening TO screening_source_reader,screening_private_writer,screening_source_collector;
GRANT EXECUTE ON FUNCTION screening.refresh_source_statistics_v1() TO screening_private_writer,screening_source_collector;
GRANT SELECT ON screening.source_datasets,screening.source_versions,screening.source_records,screening.source_names,screening.source_identifiers,screening.source_grants,screening.source_publications,screening.source_readiness_v1 TO screening_source_reader;
GRANT SELECT,INSERT ON screening.source_read_cursors TO screening_source_reader;
GRANT SELECT ON screening.database_identity TO screening_private_writer;
GRANT SELECT,INSERT,UPDATE ON screening.source_datasets,screening.source_versions,screening.source_grants,screening.private_lists,screening.private_imports TO screening_private_writer;
GRANT SELECT,INSERT,DELETE ON screening.source_records,screening.source_names,screening.source_identifiers,screening.source_artifacts,screening.source_version_artifacts TO screening_private_writer;
GRANT UPDATE ON screening.source_version_artifacts TO screening_private_writer;
GRANT SELECT,INSERT ON screening.source_publications,screening.source_lifecycle_audit,screening.private_list_audit TO screening_private_writer;
GRANT INSERT ON screening.source_publication_delivery TO screening_private_writer;
GRANT USAGE ON SEQUENCE screening.source_publications_id_seq,screening.source_lifecycle_audit_id_seq,screening.private_list_audit_id_seq TO screening_private_writer;
GRANT USAGE ON SCHEMA identity TO screening_private_writer;
GRANT SELECT(id,display_name) ON identity.users TO screening_private_writer;
GRANT EXECUTE ON FUNCTION identity.authorize_capability_v1(text,uuid,uuid,bigint,text,boolean) TO screening_private_writer;
GRANT SELECT,INSERT,UPDATE ON screening.source_connections,screening.source_import_runs,screening.source_datasets,screening.source_versions TO screening_source_collector;
GRANT SELECT ON screening.source_grants TO screening_source_collector;
GRANT SELECT,INSERT,DELETE ON screening.source_records,screening.source_names,screening.source_identifiers,screening.source_artifacts,screening.source_version_artifacts TO screening_source_collector;
GRANT UPDATE ON screening.source_version_artifacts TO screening_source_collector;
GRANT SELECT,INSERT ON screening.source_publications,screening.source_lifecycle_audit TO screening_source_collector;
GRANT SELECT ON screening.source_retention_policies,screening.source_readiness_v1,screening.private_imports,screening.source_retention_holds TO screening_source_collector;
GRANT INSERT ON screening.source_publication_delivery TO screening_source_collector;
GRANT USAGE ON SEQUENCE screening.source_publications_id_seq,screening.source_lifecycle_audit_id_seq TO screening_source_collector;
-- Diagnostics/backlog monitoring is an operator read, without mutation rights.
GRANT SELECT ON screening.source_publication_delivery,screening.source_rescreen_targets,screening.source_continuous_delivery TO screening_source_collector;
GRANT screening_source_collector TO screening_notice_operator;
GRANT SELECT,INSERT,UPDATE ON screening.reviewed_notice_datasets,screening.reviewed_notice_imports TO screening_notice_operator;
GRANT SELECT,INSERT ON screening.reviewed_notice_audit TO screening_notice_operator;
GRANT SELECT,INSERT,DELETE ON screening.source_retention_holds TO screening_notice_operator;
GRANT USAGE ON SEQUENCE screening.reviewed_notice_audit_id_seq TO screening_notice_operator;
GRANT USAGE ON SCHEMA identity TO screening_notice_operator;
GRANT EXECUTE ON FUNCTION identity.authorize_capability_v1(text,uuid,uuid,bigint,text,boolean) TO screening_notice_operator;
COMMIT;
