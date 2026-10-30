# Slide 3: Current architecture

[Reading guide](README.md) | [Previous: slide 2](02-executive-summary.md) | [Next: slide 4](04-application-scope.md)

Companion to slide 3 in the [migration presentation](../POSTGRES_TO_MYSQL_MIGRATION_PRESENTATION.md). Examples are illustrative unless identified as repository evidence; replacement designs are proposals.

The application has several services, but they connect to one logical PostgreSQL database named `fraud`. "Logical" refers to the named database the application uses; it does not specify the number of physical machines or replicas.

**Server, database, schema, and table**

Think of a server as a building, a database as a filing cabinet, a schema as a folder, and a table as a structured collection of records.

```text
PostgreSQL server
└── fraud database
    ├── core
    ├── core_ingestion
    ├── screening
    ├── case_manager
    │   └── cases
    ├── identity
    ├── tenant-specific schemas
    ├── River queue schemas
    └── keycloak (local provider configuration)
```

The five named application schemas are not necessarily the total number of schemas. Tenant, queue, and provider schemas also matter.

**What is a namespace?**

A namespace organizes names. PostgreSQL calls its database namespaces schemas. In `case_manager.cases`, `case_manager` is the schema and `cases` is the table. Another schema could also contain a table called `cases` without a naming collision.

Schemas can have permissions, but organization alone does not establish tenant authorization. Also, `core` contains objects owned by more than one service.

**Dynamic tenant schemas**

A tenant is a customer or organization. The data-model service can create physical tables and fields based on that tenant's model while the application operates. For example, one tenant might define merchants and transactions, while another defines customers and transfers. These are illustrative models.

**Independent migrations**

A migration is a versioned structural change. Each service tracks which of its changes have run. Because the database is shared, dependent changes still need coordinated ordering.

**River and Keycloak**

River stores and processes background jobs through the configured PostgreSQL integration. Keycloak is the identity provider; its local configuration uses the database with a separate schema. Keycloak-owned tables and the application's `identity` tables serve different purposes.

**How to explain this slide aloud**

"Our services share the same database but organize their tables into schemas. Tenant models, background jobs, and the local identity provider also depend on that database."

Evidence: [deployment](../docker-compose.yml), [tenant schema manager](../backend/data-model-service/internal/tenantdb/postgres/schema_manager.go).

---

[Reading guide](README.md) | [Previous: slide 2](02-executive-summary.md) | [Next: slide 4](04-application-scope.md)
