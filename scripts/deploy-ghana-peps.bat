@echo off
echo [1/6] Registering datasets and tenant grants in database...
docker exec -i new-fraud-system-postgres-1 psql -U fraud -d fraud < scripts\register-ghana-peps.sql

echo [2/6] Copying JSON fixtures into screening-service container...
docker cp gh_pep_executive.json new-fraud-system-screening-service-1:/tmp/gh_pep_executive.json
docker cp gh_pep_parliament.json new-fraud-system-screening-service-1:/tmp/gh_pep_parliament.json
docker cp gh_pep_soe.json new-fraud-system-screening-service-1:/tmp/gh_pep_soe.json
docker cp gh_pep_rca.json new-fraud-system-screening-service-1:/tmp/gh_pep_rca.json

echo [3/6] Importing Ghana Executive PEPs...
docker exec -e SOURCE_DATABASE_URL=postgres://fraud:fraud@postgres:5432/fraud?sslmode=disable new-fraud-system-screening-service-1 /app/source-import -file /tmp/gh_pep_executive.json -dataset gh-pep-executive

echo [4/6] Importing Ghana Parliament PEPs...
docker exec -e SOURCE_DATABASE_URL=postgres://fraud:fraud@postgres:5432/fraud?sslmode=disable new-fraud-system-screening-service-1 /app/source-import -file /tmp/gh_pep_parliament.json -dataset gh-pep-parliament

echo [5/6] Importing Ghana SOE and Judiciary PEPs...
docker exec -e SOURCE_DATABASE_URL=postgres://fraud:fraud@postgres:5432/fraud?sslmode=disable new-fraud-system-screening-service-1 /app/source-import -file /tmp/gh_pep_soe.json -dataset gh-pep-soe

echo [6/6] Importing Ghana PEP Relatives and Close Associates (RCAs)...
docker exec -e SOURCE_DATABASE_URL=postgres://fraud:fraud@postgres:5432/fraud?sslmode=disable new-fraud-system-screening-service-1 /app/source-import -file /tmp/gh_pep_rca.json -dataset gh-pep-rca

echo Marking connections active and verified...
docker exec new-fraud-system-postgres-1 psql -U fraud -d fraud -c "UPDATE screening.source_connections c SET verified_generation = d.active_generation, last_success_at = now(), last_error = '' FROM screening.source_datasets d WHERE (d.owner_scope, d.id) = (c.owner_scope, c.dataset_id) AND d.active_generation IS NOT NULL;"

echo Refreshing screening statistics...
docker exec new-fraud-system-postgres-1 psql -U fraud -d fraud -c "SELECT screening.refresh_source_statistics_v1();"

echo Restarting frontend container...
docker restart new-fraud-system-frontend-1

echo All done!
