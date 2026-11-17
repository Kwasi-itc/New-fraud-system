BEGIN;
SET LOCAL lock_timeout='5s';
ALTER INDEX screening.source_names_fuzzy_lookup SET(fastupdate=on);
COMMIT;
