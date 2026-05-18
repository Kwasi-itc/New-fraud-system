# Async Index Architecture TODO

## Core infrastructure

- [x] Add `IndexJob` domain model, types, statuses, and validation helpers
- [x] Add additive metadata migration to harden `core.index_jobs`
- [x] Add `IndexJobRepository` port
- [x] Implement PostgreSQL index job repository
- [x] Expose index jobs through the transaction manager

## API and service layer

- [x] Add `IndexJobService`
- [x] Add DTOs for index jobs
- [x] Add handlers for create/list/get/retry index jobs
- [x] Wire index job routes into the router

## Worker execution

- [x] Extend schema manager for managed secondary indexes
- [x] Add polling index worker runner
- [x] Add `cmd/worker`
- [x] Add worker config for polling behavior

## Product integration

- [x] Decide and implement navigation option metadata model
- [x] Add navigation option CRUD
- [x] Enqueue index jobs from navigation option creation
- [x] Populate assembled navigation options in data-model reads

## Verification

- [x] Add repository tests for index jobs
- [x] Add service tests for enqueue and retry behavior
- [x] Add worker tests for success and failure transitions
- [x] Run targeted tests

## Remaining hardening from the full plan

- [x] Add index introspection for managed secondary indexes
- [x] Add repair and reconcile-triggered job creation
- [x] Add richer retry and backoff behavior beyond manual retry
