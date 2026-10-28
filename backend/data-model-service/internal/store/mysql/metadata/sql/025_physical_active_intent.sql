ALTER TABLE {{database}}.data_model_physical_tables
 ADD CONSTRAINT data_model_physical_active_intent
 FOREIGN KEY (tenant_id,table_id,active_operation)
 REFERENCES {{database}}.data_model_schema_change_intents(tenant_id,table_id,operation_id) ON DELETE RESTRICT
