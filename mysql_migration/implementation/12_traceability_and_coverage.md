# Traceability and coverage

This register connects all 35 audit families to replacement, decision, module, task and verification. It proves documentation coverage, not completed implementation or runtime parity. Exact source paths/lines are retained in [02](02_postgresql_feature_replacements.md) and the audit callsite/schema catalogs.

| Finding | Replacement | ADR | Module plans | Implementation task | Verification |
|---|---|---|---|---|---|
| PG-01 | R-01 | ADR-06 | [07](modules/07_shared_database_and_workers.md) | DB-01 | POC-01 |
| PG-02 | R-01 | ADR-01 | [07](modules/07_shared_database_and_workers.md), [08](modules/08_infrastructure_and_operations.md) | DB-01, OP-01 | POC-01, PERF-09 |
| PG-03 | R-02 | ADR-02 | [01](modules/01_data_model_service.md), [02](modules/02_ingestion_service.md), [03](modules/03_decision_engine_service.md) | SC-01, DM-01, IN-01, DE-01 | POC-04 |
| PG-04 | R-03 | ADR-03 | [01](modules/01_data_model_service.md), [07](modules/07_shared_database_and_workers.md), [08](modules/08_infrastructure_and_operations.md) | SC-01, OP-01 | POC-04 |
| PG-05 | R-03 | ADR-03 | [01](modules/01_data_model_service.md) | DM-01, SC-01 | POC-04 |
| PG-06 | R-04 | ADR-03 | [01](modules/01_data_model_service.md), [02](modules/02_ingestion_service.md) | DM-01, IN-01 | POC-04 |
| PG-07 | R-04 | ADR-03 | [01](modules/01_data_model_service.md) | DM-01 | POC-04 |
| PG-08 | R-05 | ADR-06 | [02](modules/02_ingestion_service.md), [07](modules/07_shared_database_and_workers.md) | DB-01, IN-01 | POC-05 |
| PG-09 | R-05 | ADR-06 | [02](modules/02_ingestion_service.md) | IN-01 | POC-05 |
| PG-10 | R-06 | ADR-04 | [07](modules/07_shared_database_and_workers.md) | Q-01 | POC-02 |
| PG-11 | R-07 | ADR-06 | [05](modules/05_case_management_and_investigation.md), [06](modules/06_authentication_and_authorization.md), [07](modules/07_shared_database_and_workers.md) | DB-01, AU-01, CM-01 | POC-08, POC-09 |
| PG-12 | R-07 | ADR-06 | [07](modules/07_shared_database_and_workers.md) | DB-01 | POC-01, POC-06 |
| PG-13 | R-08 | ADR-06 | [04](modules/04_screening_service.md) | SR-01 | POC-06 |
| PG-14 | R-06 | ADR-04 | [04](modules/04_screening_service.md), [05](modules/05_case_management_and_investigation.md), [06](modules/06_authentication_and_authorization.md), [07](modules/07_shared_database_and_workers.md) | Q-01, SR-01, CM-01, AU-01 | POC-02, POC-06 |
| PG-15 | R-09 | ADR-07 | [03](modules/03_decision_engine_service.md), [04](modules/04_screening_service.md) | DE-01, SR-01 | POC-06 |
| PG-16 | R-10 | ADR-05, ADR-07 | [04](modules/04_screening_service.md), [05](modules/05_case_management_and_investigation.md) | SC-01, SR-01, CM-01 | POC-03, POC-07 |
| PG-17 | R-11 | ADR-07 | [06](modules/06_authentication_and_authorization.md) | AU-01 | POC-08 |
| PG-18 | R-09 | ADR-07 | [03](modules/03_decision_engine_service.md), [04](modules/04_screening_service.md), [05](modules/05_case_management_and_investigation.md), [06](modules/06_authentication_and_authorization.md) | CM-01, SR-01, DE-01, AU-01 | POC-07 |
| PG-19 | R-12 | ADR-07 | [05](modules/05_case_management_and_investigation.md) | CM-01 | POC-09 |
| PG-20 | R-13 | ADR-08, ADR-09 | [01](modules/01_data_model_service.md), [02](modules/02_ingestion_service.md), [03](modules/03_decision_engine_service.md), [04](modules/04_screening_service.md), [05](modules/05_case_management_and_investigation.md) | SC-01, DM-01, IN-01, DE-01, SR-01, CM-01 | POC-10 |
| PG-21 | R-13 | ADR-08 | [01](modules/01_data_model_service.md), [04](modules/04_screening_service.md), [06](modules/06_authentication_and_authorization.md) | SC-01, AU-01, DM-01, SR-01 | POC-10 |
| PG-22 | R-14 | ADR-08 | [07](modules/07_shared_database_and_workers.md) | SC-01, DB-01, DT-01 | POC-01 |
| PG-23 | R-14 | ADR-08 | [07](modules/07_shared_database_and_workers.md) | SC-01, DB-01, DT-01 | POC-01, POC-08 |
| PG-24 | R-14 | ADR-08 | [07](modules/07_shared_database_and_workers.md) | SC-01, DB-01, DT-01 | POC-01, POC-10 |
| PG-25 | R-15 | ADR-05, ADR-09 | [03](modules/03_decision_engine_service.md), [04](modules/04_screening_service.md), [05](modules/05_case_management_and_investigation.md) | SC-01, DE-01, SR-01, CM-01 | POC-03, POC-11 |
| PG-26 | R-16 | ADR-09 | [03](modules/03_decision_engine_service.md), [05](modules/05_case_management_and_investigation.md) | DE-01, CM-01 | POC-10, POC-11 |
| PG-27 | R-03 | ADR-03, ADR-12 | [01](modules/01_data_model_service.md), [08](modules/08_infrastructure_and_operations.md) | DM-01, OP-01, PF-01 | POC-04, PERF-03 |
| PG-28 | R-01 | ADR-06 | [07](modules/07_shared_database_and_workers.md), [09](modules/09_frontend_integrations_and_tests.md) | DB-01, V-01 | POC-01, POC-05 |
| PG-29 | R-17 | ADR-11 | [06](modules/06_authentication_and_authorization.md), [08](modules/08_infrastructure_and_operations.md) | AU-02, OP-01 | V-AU, PERF-07 |
| PG-30 | R-17 | ADR-10 | [08](modules/08_infrastructure_and_operations.md) | OP-01, DT-01 | PERF-09 |
| PG-31 | R-17 | ADR-12 | [09](modules/09_frontend_integrations_and_tests.md) | V-01, PF-01 | POC-01?11, PERF-01?09 |
| PG-32 | R-02 | ADR-02, ADR-06 | [02](modules/02_ingestion_service.md), [03](modules/03_decision_engine_service.md), [08](modules/08_infrastructure_and_operations.md) | IN-01, DE-01, OP-01 | POC-05, POC-10 |
| PG-33 | R-15 | ADR-09 | [04](modules/04_screening_service.md), [05](modules/05_case_management_and_investigation.md) | SR-01, CM-01 | POC-11 |
| PG-34 | R-13 | ADR-09, ADR-12 | [02](modules/02_ingestion_service.md), [03](modules/03_decision_engine_service.md), [05](modules/05_case_management_and_investigation.md) | IN-01, DE-01, CM-01, PF-01 | POC-10, PERF-01, PERF-06 |
| PG-35 | R-12 | ADR-05, ADR-07 | [04](modules/04_screening_service.md), [05](modules/05_case_management_and_investigation.md), [06](modules/06_authentication_and_authorization.md) | SC-01, AU-01, SR-01, CM-01, DT-01 | POC-03, POC-07, POC-08, POC-09 |

## Reusable test evidence

These are source suites to port/reuse, not tests run here. Other current tests remain covered through the audit callsite/file manifests; this list selects important behavior rather than claiming every test is listed.

| Module | Existing test source |
|---|---|
| data-model | [tenant_handler_test.go](../../backend/data-model-service/internal/httpapi/handlers/tenant_handler_test.go) |
| data-model | [platform_auth_integration_test.go](../../backend/data-model-service/internal/httpapi/platform_auth_integration_test.go) |
| data-model | [router_integration_test.go](../../backend/data-model-service/internal/httpapi/router_integration_test.go) |
| data-model | [integration_test.go](../../backend/data-model-service/internal/service/integration_test.go) |
| data-model | [transaction_manager_test.go](../../backend/data-model-service/internal/service/transaction_manager_test.go) |
| data-model | [repository_integration_test.go](../../backend/data-model-service/internal/store/postgres/repository_integration_test.go) |
| ingestion | [contract_reader_test.go](../../backend/ingestion-service/internal/clients/datamodel/contract_reader_test.go) |
| ingestion | [integration_test.go](../../backend/ingestion-service/internal/service/integration_test.go) |
| ingestion | [table_contract_test.go](../../backend/ingestion-service/internal/service/table_contract_test.go) |
| ingestion | [contract_integration_test.go](../../backend/ingestion-service/internal/store/postgres/contract_integration_test.go) |
| ingestion | [tenant_data_reader_test.go](../../backend/ingestion-service/internal/store/postgres/tenant_data_reader_test.go) |
| decision-engine | [screening_integration_config_test.go](../../backend/decision-engine-service/internal/app/screening_integration_config_test.go) |
| decision-engine | [payload_contract_test.go](../../backend/decision-engine-service/internal/clients/datamodel/payload_contract_test.go) |
| decision-engine | [case_contract_test.go](../../backend/decision-engine-service/internal/clients/dispatch/case_contract_test.go) |
| decision-engine | [screening_integration_auth_test.go](../../backend/decision-engine-service/internal/httpapi/screening_integration_auth_test.go) |
| decision-engine | [aggregate_http_integration_test.go](../../backend/decision-engine-service/internal/runtime/ast_eval/aggregate_http_integration_test.go) |
| decision-engine | [async_validation_integration_test.go](../../backend/decision-engine-service/internal/service/async_validation_integration_test.go) |
| decision-engine | [execution_service_integration_test.go](../../backend/decision-engine-service/internal/service/execution_service_integration_test.go) |
| decision-engine | [rule_evidence_integration_test.go](../../backend/decision-engine-service/internal/service/rule_evidence_integration_test.go) |
| decision-engine | [screening_config_integration_test.go](../../backend/decision-engine-service/internal/store/postgres/screening_config_integration_test.go) |
| decision-engine | [tenant_data_reader_test.go](../../backend/decision-engine-service/internal/store/postgres/tenant_data_reader_test.go) |
| screening | [screening_integration_config_test.go](../../backend/screening-service/internal/app/screening_integration_config_test.go) |
| screening | [manual_search_contract_test.go](../../backend/screening-service/internal/clients/provider/manual_search_contract_test.go) |
| screening | [case_exclusions_test.go](../../backend/screening-service/internal/httpapi/case_exclusions_test.go) |
| screening | [screening_integration_auth_test.go](../../backend/screening-service/internal/httpapi/screening_integration_auth_test.go) |
| screening | [case_event_outbox_integration_test.go](../../backend/screening-service/internal/store/postgres/case_event_outbox_integration_test.go) |
| screening | [continuous_identity_integration_test.go](../../backend/screening-service/internal/store/postgres/continuous_identity_integration_test.go) |
| screening | [continuous_intake_integration_test.go](../../backend/screening-service/internal/store/postgres/continuous_intake_integration_test.go) |
| screening | [continuous_journey_integration_test.go](../../backend/screening-service/internal/store/postgres/continuous_journey_integration_test.go) |
| screening | [decision_intake_integration_test.go](../../backend/screening-service/internal/store/postgres/decision_intake_integration_test.go) |
| screening | [dispatch_identity_integration_test.go](../../backend/screening-service/internal/store/postgres/dispatch_identity_integration_test.go) |
| screening | [exclusion_history_migration_test.go](../../backend/screening-service/internal/store/postgres/exclusion_history_migration_test.go) |
| screening | [execution_projection_integration_test.go](../../backend/screening-service/internal/store/postgres/execution_projection_integration_test.go) |
| screening | [investigator_journey_integration_test.go](../../backend/screening-service/internal/store/postgres/investigator_journey_integration_test.go) |
| screening | [manual_case_review_integration_test.go](../../backend/screening-service/internal/store/postgres/manual_case_review_integration_test.go) |
| screening | [manual_search_integration_test.go](../../backend/screening-service/internal/store/postgres/manual_search_integration_test.go) |
| screening | [object_exclusions_migration_test.go](../../backend/screening-service/internal/store/postgres/object_exclusions_migration_test.go) |
| screening | [review_consistency_integration_test.go](../../backend/screening-service/internal/store/postgres/review_consistency_integration_test.go) |
| screening | [scenario_address_worker_integration_test.go](../../backend/screening-service/internal/store/postgres/scenario_address_worker_integration_test.go) |
| screening | [scenario_authoring_journey_integration_test.go](../../backend/screening-service/internal/store/postgres/scenario_authoring_journey_integration_test.go) |
| screening | [scenario_worker_journey_integration_test.go](../../backend/screening-service/internal/store/postgres/scenario_worker_journey_integration_test.go) |
| case-manager | [continuous_intake_integration_test.go](../../backend/case-manager-service/internal/store/postgres/continuous_intake_integration_test.go) |
| case-manager | [decision_membership_integration_test.go](../../backend/case-manager-service/internal/store/postgres/decision_membership_integration_test.go) |
| case-manager | [decision_screening_integration_test.go](../../backend/case-manager-service/internal/store/postgres/decision_screening_integration_test.go) |
| case-manager | [decision_selection_integration_test.go](../../backend/case-manager-service/internal/store/postgres/decision_selection_integration_test.go) |
| case-manager | [explorer_integration_test.go](../../backend/case-manager-service/internal/store/postgres/explorer_integration_test.go) |
| case-manager | [identity_integration_test.go](../../backend/case-manager-service/internal/store/postgres/identity_integration_test.go) |
| case-manager | [lifecycle_integration_test.go](../../backend/case-manager-service/internal/store/postgres/lifecycle_integration_test.go) |
| case-manager | [logging_integration_test.go](../../backend/case-manager-service/internal/store/postgres/logging_integration_test.go) |
| case-manager | [manual_screening_integration_test.go](../../backend/case-manager-service/internal/store/postgres/manual_screening_integration_test.go) |
| case-manager | [phase1_access_integration_test.go](../../backend/case-manager-service/internal/store/postgres/phase1_access_integration_test.go) |
| case-manager | [phase1_completion_integration_test.go](../../backend/case-manager-service/internal/store/postgres/phase1_completion_integration_test.go) |
| case-manager | [phase1_integration_test.go](../../backend/case-manager-service/internal/store/postgres/phase1_integration_test.go) |
| case-manager | [phase2_intake_integration_test.go](../../backend/case-manager-service/internal/store/postgres/phase2_intake_integration_test.go) |
| case-manager | [phase3_workspace_integration_test.go](../../backend/case-manager-service/internal/store/postgres/phase3_workspace_integration_test.go) |
| case-manager | [phase4_backlog_integration_test.go](../../backend/case-manager-service/internal/store/postgres/phase4_backlog_integration_test.go) |
| case-manager | [phase4_evidence_integration_test.go](../../backend/case-manager-service/internal/store/postgres/phase4_evidence_integration_test.go) |
| case-manager | [phase4_migrations_integration_test.go](../../backend/case-manager-service/internal/store/postgres/phase4_migrations_integration_test.go) |
| case-manager | [phase4_operations_integration_test.go](../../backend/case-manager-service/internal/store/postgres/phase4_operations_integration_test.go) |
| case-manager | [phase4_slow_delivery_integration_test.go](../../backend/case-manager-service/internal/store/postgres/phase4_slow_delivery_integration_test.go) |
| case-manager | [phase4_worker_integration_test.go](../../backend/case-manager-service/internal/store/postgres/phase4_worker_integration_test.go) |
| case-manager | [phase5_analytics_integration_test.go](../../backend/case-manager-service/internal/store/postgres/phase5_analytics_integration_test.go) |
| case-manager | [phase5_reports_integration_test.go](../../backend/case-manager-service/internal/store/postgres/phase5_reports_integration_test.go) |
| case-manager | [shared_load_integration_test.go](../../backend/case-manager-service/internal/store/postgres/shared_load_integration_test.go) |
| auth | [login_tenant_test.go](../../backend/auth-service/internal/platform/login_tenant_test.go) |
| auth | [sessions_test.go](../../backend/auth-service/internal/platform/sessions_test.go) |

## Coverage boundaries

All six services, shared investigator, queues/workers, frontend/HTTP contracts, operational tooling, provider and tests have module ownership. Additional object mapping is in [15](15_target_object_map.md). No application ORM, PostgreSQL search extension or RLS is fabricated from an absent source dependency. The live schema, external CI, runtime grants, actual data distribution and dependency-generated River/provider DDL remain B-01/AU-02 work.

The original source audit remains unchanged. The historical October14 prototype was reverted and no passing result from it is counted here. Source changes after the captured audit require revalidation of affected links/claims; do not overwrite user changes to force a matching hash.
