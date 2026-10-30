DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM core.screening_authoring_events) THEN
  RAISE EXCEPTION 'Preserve screening authoring history; use a forward migration';
 END IF;
END $$;
DROP TABLE core.screening_authoring_events;
