BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Large snapshots need current planner statistics before they become searchable.
-- This bounded owner contract grants maintenance of exactly three projections;
-- it does not grant table ownership, pg_maintain or data mutation authority.
CREATE FUNCTION screening.refresh_source_statistics_v1() RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp
SET lock_timeout='2s' SET statement_timeout='15s' AS $$
BEGIN
 IF NOT pg_try_advisory_xact_lock(1800415344,1) THEN RETURN false; END IF;
 ANALYZE screening.source_records;
 ANALYZE screening.source_names;
 ANALYZE screening.source_identifiers;
 RETURN true;
END $$;
REVOKE ALL ON FUNCTION screening.refresh_source_statistics_v1() FROM PUBLIC;
COMMIT;
