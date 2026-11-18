#!/usr/bin/env bash
set -e

# ==============================================================================
# Screening Module - Master Seeding & Deployment Script
# Automatically seeds all Ghana Sovereign PEPs, MMDCEs, and Political Parties
# ==============================================================================

POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-new-fraud-system-postgres-1}"
SCREENING_CONTAINER="${SCREENING_CONTAINER:-new-fraud-system-screening-service-1}"
DATABASE_URL="${DATABASE_URL:-postgres://fraud:fraud@postgres:5432/fraud?sslmode=disable}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

echo "==> [1/5] Checking and building required JSON fixtures..."
cd "$ROOT_DIR"

if [ ! -f "gh_pep_executive.json" ] || [ ! -f "gh_pep_parliament.json" ] || [ ! -f "gh_pep_soe.json" ] || [ ! -f "gh_pep_rca.json" ]; then
    echo "    Building core Ghana PEP datasets (Executive, Parliament, SOE, RCA)..."
    node scripts/build-unified-ghana-peps.mjs
fi

if [ ! -f "gh_pep_local_gov.json" ] || [ ! -f "gh_pep_parties.json" ]; then
    echo "    Building sub-national & political party datasets..."
    node scripts/scrape-and-build-subnational-peps.mjs
fi

echo "==> [2/5] Registering datasets and tenant grants in PostgreSQL..."
docker exec -i "$POSTGRES_CONTAINER" psql -U fraud -d fraud < "$SCRIPT_DIR/register-all-ghana-peps.sql"

echo "==> [3/5] Copying fixtures into screening-service container..."
docker cp gh_pep_executive.json "$SCREENING_CONTAINER:/tmp/gh_pep_executive.json"
docker cp gh_pep_parliament.json "$SCREENING_CONTAINER:/tmp/gh_pep_parliament.json"
docker cp gh_pep_soe.json "$SCREENING_CONTAINER:/tmp/gh_pep_soe.json"
docker cp gh_pep_rca.json "$SCREENING_CONTAINER:/tmp/gh_pep_rca.json"
docker cp gh_pep_local_gov.json "$SCREENING_CONTAINER:/tmp/gh_pep_local_gov.json"
docker cp gh_pep_parties.json "$SCREENING_CONTAINER:/tmp/gh_pep_parties.json"

echo "==> [4/5] Ingesting all datasets via screening-service /app/source-import..."
for dataset in gh-pep-executive gh-pep-parliament gh-pep-soe gh-pep-rca gh-pep-local-gov gh-pep-parties; do
    file_name="${dataset//-/_}.json"
    echo "    Ingesting $dataset from /tmp/$file_name..."
    docker exec -e SOURCE_DATABASE_URL="$DATABASE_URL" "$SCREENING_CONTAINER" \
        /app/source-import -file "/tmp/$file_name" -dataset "$dataset"
done

echo "==> [5/5] Activating generations and refreshing screening statistics..."
docker exec "$POSTGRES_CONTAINER" psql -U fraud -d fraud -c "
UPDATE screening.source_connections c 
SET verified_generation = d.active_generation, last_success_at = now(), last_error = '' 
FROM screening.source_datasets d 
WHERE (d.owner_scope, d.id) = (c.owner_scope, c.dataset_id) 
  AND d.active_generation IS NOT NULL;
SELECT screening.refresh_source_statistics_v1();
"

echo "==> Current Active Record Counts:"
docker exec "$POSTGRES_CONTAINER" psql -U fraud -d fraud -c "
SELECT d.id, count(r.record_id) as records 
FROM screening.source_datasets d 
JOIN screening.source_records r ON r.owner_scope = d.owner_scope AND r.dataset_id = d.id AND r.generation = d.active_generation 
GROUP BY d.id 
ORDER BY records DESC;
"

echo "==> Seeding completed successfully!"
