-- Existing data is checked before adding tenant-consistent relationships.
-- This migration never deletes or repairs data automatically; a failed preflight
-- requires explicit data repair before retrying. All DDL is transactional.
BEGIN;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM case_manager.inboxes child
    LEFT JOIN case_manager.inboxes parent ON parent.id = child.escalation_inbox_id AND parent.tenant_id = child.tenant_id
    WHERE child.escalation_inbox_id IS NOT NULL AND parent.id IS NULL) THEN
    RAISE EXCEPTION 'Tenant relationship preflight failed: inboxes.escalation_inbox_id; repair orphan/cross-tenant references before retrying';
  END IF;
END $$;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM case_manager.inbox_users child
    LEFT JOIN case_manager.inboxes parent ON parent.id = child.inbox_id AND parent.tenant_id = child.tenant_id
    WHERE child.inbox_id IS NOT NULL AND parent.id IS NULL) THEN
    RAISE EXCEPTION 'Tenant relationship preflight failed: inbox_users.inbox_id; repair orphan/cross-tenant references before retrying';
  END IF;
END $$;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM case_manager.cases child
    LEFT JOIN case_manager.inboxes parent ON parent.id = child.inbox_id AND parent.tenant_id = child.tenant_id
    WHERE child.inbox_id IS NOT NULL AND parent.id IS NULL) THEN
    RAISE EXCEPTION 'Tenant relationship preflight failed: cases.inbox_id; repair orphan/cross-tenant references before retrying';
  END IF;
END $$;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM case_manager.case_decisions child
    LEFT JOIN case_manager.cases parent ON parent.id = child.case_id AND parent.tenant_id = child.tenant_id
    WHERE child.case_id IS NOT NULL AND parent.id IS NULL) THEN
    RAISE EXCEPTION 'Tenant relationship preflight failed: case_decisions.case_id; repair orphan/cross-tenant references before retrying';
  END IF;
END $$;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM case_manager.case_screenings child
    LEFT JOIN case_manager.cases parent ON parent.id = child.case_id AND parent.tenant_id = child.tenant_id
    WHERE child.case_id IS NOT NULL AND parent.id IS NULL) THEN
    RAISE EXCEPTION 'Tenant relationship preflight failed: case_screenings.case_id; repair orphan/cross-tenant references before retrying';
  END IF;
END $$;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM case_manager.case_tags child
    LEFT JOIN case_manager.cases parent ON parent.id = child.case_id AND parent.tenant_id = child.tenant_id
    WHERE child.case_id IS NOT NULL AND parent.id IS NULL) THEN
    RAISE EXCEPTION 'Tenant relationship preflight failed: case_tags.case_id; repair orphan/cross-tenant references before retrying';
  END IF;
END $$;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM case_manager.case_files child
    LEFT JOIN case_manager.cases parent ON parent.id = child.case_id AND parent.tenant_id = child.tenant_id
    WHERE child.case_id IS NOT NULL AND parent.id IS NULL) THEN
    RAISE EXCEPTION 'Tenant relationship preflight failed: case_files.case_id; repair orphan/cross-tenant references before retrying';
  END IF;
END $$;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM case_manager.case_contributors child
    LEFT JOIN case_manager.cases parent ON parent.id = child.case_id AND parent.tenant_id = child.tenant_id
    WHERE child.case_id IS NOT NULL AND parent.id IS NULL) THEN
    RAISE EXCEPTION 'Tenant relationship preflight failed: case_contributors.case_id; repair orphan/cross-tenant references before retrying';
  END IF;
END $$;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM case_manager.case_events child
    LEFT JOIN case_manager.cases parent ON parent.id = child.case_id AND parent.tenant_id = child.tenant_id
    WHERE child.case_id IS NOT NULL AND parent.id IS NULL) THEN
    RAISE EXCEPTION 'Tenant relationship preflight failed: case_events.case_id; repair orphan/cross-tenant references before retrying';
  END IF;
END $$;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM case_manager.ai_case_reviews child
    LEFT JOIN case_manager.cases parent ON parent.id = child.case_id AND parent.tenant_id = child.tenant_id
    WHERE child.case_id IS NOT NULL AND parent.id IS NULL) THEN
    RAISE EXCEPTION 'Tenant relationship preflight failed: ai_case_reviews.case_id; repair orphan/cross-tenant references before retrying';
  END IF;
END $$;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM case_manager.suspicious_activity_reports child
    LEFT JOIN case_manager.cases parent ON parent.id = child.case_id AND parent.tenant_id = child.tenant_id
    WHERE child.case_id IS NOT NULL AND parent.id IS NULL) THEN
    RAISE EXCEPTION 'Tenant relationship preflight failed: suspicious_activity_reports.case_id; repair orphan/cross-tenant references before retrying';
  END IF;
END $$;

DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM case_manager.case_tags child
    LEFT JOIN case_manager.tags parent ON parent.id = child.tag_id AND parent.tenant_id = child.tenant_id
    WHERE child.tag_id IS NOT NULL AND parent.id IS NULL) THEN
    RAISE EXCEPTION 'Tenant relationship preflight failed: case_tags.tag_id; repair orphan/cross-tenant references before retrying';
  END IF;
END $$;

ALTER TABLE case_manager.inboxes ADD CONSTRAINT inboxes_tenant_id_unique UNIQUE (tenant_id, id);
ALTER TABLE case_manager.cases ADD CONSTRAINT cases_tenant_id_unique UNIQUE (tenant_id, id);
ALTER TABLE case_manager.tags ADD CONSTRAINT tags_tenant_id_unique UNIQUE (tenant_id, id);
ALTER TABLE case_manager.inboxes ADD CONSTRAINT inboxes_escalation_tenant_fk
  FOREIGN KEY (tenant_id, escalation_inbox_id) REFERENCES case_manager.inboxes (tenant_id, id) ON DELETE NO ACTION;
ALTER TABLE case_manager.inbox_users ADD CONSTRAINT inbox_users_inbox_tenant_fk
  FOREIGN KEY (tenant_id, inbox_id) REFERENCES case_manager.inboxes (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE case_manager.cases ADD CONSTRAINT cases_inbox_tenant_fk
  FOREIGN KEY (tenant_id, inbox_id) REFERENCES case_manager.inboxes (tenant_id, id) ON DELETE NO ACTION;
ALTER TABLE case_manager.case_decisions ADD CONSTRAINT case_decisions_case_tenant_fk
  FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE case_manager.case_screenings ADD CONSTRAINT case_screenings_case_tenant_fk
  FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE case_manager.case_tags ADD CONSTRAINT case_tags_case_tenant_fk
  FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE case_manager.case_files ADD CONSTRAINT case_files_case_tenant_fk
  FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE case_manager.case_contributors ADD CONSTRAINT case_contributors_case_tenant_fk
  FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE case_manager.case_events ADD CONSTRAINT case_events_case_tenant_fk
  FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE case_manager.ai_case_reviews ADD CONSTRAINT ai_case_reviews_case_tenant_fk
  FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE case_manager.suspicious_activity_reports ADD CONSTRAINT suspicious_activity_reports_case_tenant_fk
  FOREIGN KEY (tenant_id, case_id) REFERENCES case_manager.cases (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE case_manager.case_tags ADD CONSTRAINT case_tags_tag_tenant_fk
  FOREIGN KEY (tenant_id, tag_id) REFERENCES case_manager.tags (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE case_manager.inboxes ADD CONSTRAINT inboxes_name_nonempty CHECK (btrim(name) <> '');
ALTER TABLE case_manager.inboxes ADD CONSTRAINT inboxes_status_valid CHECK (status IN ('active', 'archived'));
ALTER TABLE case_manager.inboxes ADD CONSTRAINT inboxes_escalation_not_self CHECK (escalation_inbox_id IS NULL OR escalation_inbox_id <> id);
ALTER TABLE case_manager.cases ADD CONSTRAINT cases_name_nonempty CHECK (btrim(name) <> '');
ALTER TABLE case_manager.case_files ADD CONSTRAINT case_files_metadata_valid CHECK (btrim(file_name) <> '' AND btrim(storage_key) <> '' AND file_size >= 0);
ALTER TABLE case_manager.tags ADD CONSTRAINT tags_name_nonempty CHECK (btrim(name) <> '');

COMMIT;
