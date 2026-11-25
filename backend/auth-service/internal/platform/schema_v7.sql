-- Identity-owned v7: permit API clients to inspect models and author scenario drafts.
-- Publication remains human-only through scenarios:publish.
UPDATE identity.capability_catalog
SET client_access='normal'
WHERE name IN ('models:read','scenarios:read','scenarios:write');

DO $$
BEGIN
 IF (SELECT count(*) FROM identity.capability_catalog
     WHERE name IN ('models:read','scenarios:read','scenarios:write')
       AND client_access='normal') <> 3 THEN
  RAISE EXCEPTION 'client authoring capabilities are missing from the capability catalog';
 END IF;
END $$;

INSERT INTO identity.schema_migrations(version) VALUES(7) ON CONFLICT DO NOTHING;
