\set ON_ERROR_STOP on
\getenv provider_password KC_DB_PASSWORD
BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';
-- Provider-owned schema in the existing application database. Never reset an
-- existing role's password or adopt a schema owned by somebody else.
SELECT format('CREATE ROLE fraud_keycloak LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD %L', :'provider_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='fraud_keycloak')
\gexec
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='fraud_keycloak'
             AND (rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls OR NOT rolcanlogin))
     OR EXISTS (SELECT 1 FROM pg_auth_members m JOIN pg_roles r ON r.oid=m.member
                WHERE r.rolname='fraud_keycloak') THEN
    RAISE EXCEPTION 'Existing provider role has unexpected privileges; reconcile explicitly';
  END IF;
  IF EXISTS (SELECT 1 FROM pg_namespace n JOIN pg_roles r ON r.oid=n.nspowner
             WHERE n.nspname='keycloak' AND r.rolname<>'fraud_keycloak') THEN
    RAISE EXCEPTION 'keycloak schema belongs to another owner; reconcile explicitly';
  END IF;
END $$;
CREATE SCHEMA IF NOT EXISTS keycloak AUTHORIZATION fraud_keycloak;
GRANT CONNECT ON DATABASE fraud TO fraud_keycloak;
COMMIT;
