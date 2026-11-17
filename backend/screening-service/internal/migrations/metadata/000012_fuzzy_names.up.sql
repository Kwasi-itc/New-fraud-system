BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Shared extensions are intentionally retained by the down migration.
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;
CREATE EXTENSION IF NOT EXISTS unaccent WITH SCHEMA public;
-- Versioned projection. Changing the unaccent dictionary requires a new profile
-- and rebuilding the projection, never an in-place interpretation change.
CREATE FUNCTION screening.name_key_v1(value text) RETURNS text
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE AS $$
 SELECT btrim(regexp_replace(public.unaccent('public.unaccent'::regdictionary,value),'[^[:alnum:]]+',' ','g'))
$$;
ALTER TABLE screening.source_names ADD COLUMN fuzzy_name text
 GENERATED ALWAYS AS (screening.name_key_v1(normalized)) STORED;
CREATE INDEX source_names_fuzzy_lookup ON screening.source_names USING gin(fuzzy_name public.gin_trgm_ops);
COMMIT;
