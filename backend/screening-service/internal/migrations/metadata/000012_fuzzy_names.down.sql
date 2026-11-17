BEGIN;
DROP INDEX screening.source_names_fuzzy_lookup;
ALTER TABLE screening.source_names DROP COLUMN fuzzy_name;
DROP FUNCTION screening.name_key_v1(text);
COMMIT;
