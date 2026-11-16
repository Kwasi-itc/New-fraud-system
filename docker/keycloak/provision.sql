\set ON_ERROR_STOP on
\set kc_user fraud_keycloak
\getenv kc_user KC_DB_USERNAME
\getenv provider_password KC_DB_PASSWORD
BEGIN;
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';
-- Provider-owned schema in the existing application database. Never reset an
-- existing role's password or adopt a schema owned by somebody else.
SELECT format('CREATE ROLE %I LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD %L', :'kc_user', :'provider_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = :'kc_user')
\gexec
SELECT format(
  $do$
  DO $inner$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = %L
               AND (rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls OR NOT rolcanlogin))
       OR EXISTS (SELECT 1 FROM pg_auth_members m JOIN pg_roles r ON r.oid = m.member
                  WHERE r.rolname = %L) THEN
      RAISE EXCEPTION 'Existing provider role has unexpected privileges; reconcile explicitly';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_namespace n JOIN pg_roles r ON r.oid = n.nspowner
               WHERE n.nspname = 'keycloak' AND r.rolname <> %L) THEN
      RAISE EXCEPTION 'keycloak schema belongs to another owner; reconcile explicitly';
    END IF;
  END $inner$;
  $do$,
  :'kc_user', :'kc_user', :'kc_user'
)
\gexec
SELECT format('GRANT %I TO CURRENT_USER', :'kc_user')
\gexec
SELECT format('CREATE SCHEMA IF NOT EXISTS keycloak AUTHORIZATION %I', :'kc_user')
\gexec
SELECT format('GRANT CONNECT ON DATABASE %I TO %I', current_database(), :'kc_user')
\gexec
COMMIT;