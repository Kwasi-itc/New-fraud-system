DROP INDEX IF EXISTS custom_list_entries_tenant_list_id_idx;

ALTER TABLE core.custom_list_entries
  DROP COLUMN IF EXISTS list_id;

DROP INDEX IF EXISTS custom_lists_tenant_name_idx;

DROP TABLE IF EXISTS core.custom_lists;
