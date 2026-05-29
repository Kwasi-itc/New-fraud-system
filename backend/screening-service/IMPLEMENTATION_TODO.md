# Screening Service Implementation TODO

## Current status

This file is now the active implementation tracker for `screening-service`.

Implementation has started with a V1 extraction slice focused on:

- service shell
- screening intake and persistence
- screening worker dispatch
- screening read and review APIs
- screening whitelist APIs
- freeform screening intake

## V1 module breakdown

### Module 1: Service shell

- [x] create Go module
- [x] create `cmd/server`
- [x] create `cmd/worker`
- [x] create `cmd/migrate`
- [x] add config loading
- [x] add database bootstrap
- [x] add health endpoints

### Module 2: Screening aggregate

- [x] define screening domain model
- [x] define screening match model
- [x] define match comment model
- [x] define whitelist model
- [x] define screening state transitions used in V1

### Module 3: Persistence

- [x] create initial schema migrations
- [x] add screening repository
- [x] add screening match repository
- [x] add screening comment repository
- [x] add whitelist repository
- [x] add transaction manager

### Module 4: Screening APIs

- [x] create screening intake endpoint
- [x] create freeform screening endpoint
- [x] create list-by-decision endpoint
- [x] create screening detail endpoint
- [x] create retry endpoint
- [x] create match review endpoint
- [x] create match comment endpoint
- [x] create whitelist create endpoint
- [x] create whitelist delete endpoint
- [x] create whitelist search endpoint

### Module 5: Worker and provider dispatch

- [x] define provider search port
- [x] add HTTP provider client
- [x] implement pending-screening worker loop
- [x] persist provider raw payloads
- [x] persist normalized screening matches
- [x] derive initial review status from provider matches

## Next implementation phases

### Phase 2: Enrichment and evidence

- [x] add provider enrichment endpoint and port behavior
- [x] persist enrichment payload updates for matches
- [x] add evidence file metadata tables
- [x] add upload/list/download file APIs
- [x] isolate blob storage behind a port

### Phase 3: Decision-engine integration tightening

- [x] replace any transitional assumptions with a dedicated decision-engine caller contract
- [x] define callback or event contract back to decision engine
- [x] add idempotency safeguards for duplicate intake requests
- [x] add provider routing by provider key instead of single provider URL

### Phase 4: Continuous screening extraction

- [x] add continuous-screening config models and persistence
- [x] add monitored-object APIs
- [x] add ingestion-backed record lookup contract
- [x] add object-added worker
- [x] add object-updated worker
- [x] add inbox and case-side-effect integration ports

### Phase 5: Dataset update processing

- [x] add dataset freshness and catalog endpoints
- [x] add dataset update job tables
- [x] add offset and retry tracking
- [x] add dataset update worker lifecycle
- [x] add re-screening lifecycle for provider dataset updates

### Phase 6: Hardening

- [x] add service-level tests for screening review flow
- [x] add repository tests
- [x] add worker tests
- [x] add handler tests for key screening endpoints
- [x] add metrics and structured operational logs
- [x] add runbook notes to README
