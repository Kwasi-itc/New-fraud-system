@echo off
echo ==============================================================================
echo [1/5] Checking and building required JSON fixtures...
echo ==============================================================================
if not exist gh_pep_executive.json (
    echo Building core Ghana PEP datasets...
    node scripts\build-unified-ghana-peps.mjs
)
if not exist gh_pep_local_gov.json (
    echo Building sub-national and party datasets...
    node scripts\scrape-and-build-subnational-peps.mjs
)

echo ==============================================================================
echo [2/5] Registering datasets and tenant grants in database...
echo ==============================================================================
docker exec -i new-fraud-system-postgres-1 psql -v ON_ERROR_STOP=1 -U fraud -d fraud < scripts\register-all-ghana-peps.sql
if errorlevel 1 exit /b 1

echo ==============================================================================
echo [3/5] Copying JSON fixtures into screening-service container...
echo ==============================================================================
docker cp gh_pep_executive.json new-fraud-system-screening-service-1:/tmp/gh_pep_executive.json
docker cp gh_pep_parliament.json new-fraud-system-screening-service-1:/tmp/gh_pep_parliament.json
docker cp gh_pep_soe.json new-fraud-system-screening-service-1:/tmp/gh_pep_soe.json
docker cp gh_pep_rca.json new-fraud-system-screening-service-1:/tmp/gh_pep_rca.json
docker cp gh_pep_local_gov.json new-fraud-system-screening-service-1:/tmp/gh_pep_local_gov.json
docker cp gh_pep_parties.json new-fraud-system-screening-service-1:/tmp/gh_pep_parties.json
docker cp gh_pep_judiciary.json new-fraud-system-screening-service-1:/tmp/gh_pep_judiciary.json
if errorlevel 1 exit /b 1

echo Copying World Bank fixture...
docker cp worldbank_fixture.json new-fraud-system-screening-service-1:/tmp/worldbank_fixture.json
if errorlevel 1 exit /b 1

echo ==============================================================================
echo [4/5] Ingesting datasets via /app/source-import...
echo ==============================================================================
echo Importing Ghana Executive PEPs...
docker exec -e SOURCE_DATABASE_URL=postgres://fraud:fraud@postgres:5432/fraud?sslmode=disable new-fraud-system-screening-service-1 /app/source-import -file /tmp/gh_pep_executive.json -dataset gh-pep-executive -snapshot-reference "Local Ghana PEP snapshot intake: gh_pep_executive.json"
if errorlevel 1 exit /b 1

echo Importing Ghana Parliament PEPs...
docker exec -e SOURCE_DATABASE_URL=postgres://fraud:fraud@postgres:5432/fraud?sslmode=disable new-fraud-system-screening-service-1 /app/source-import -file /tmp/gh_pep_parliament.json -dataset gh-pep-parliament -snapshot-reference "Local Ghana PEP snapshot intake: gh_pep_parliament.json"
if errorlevel 1 exit /b 1

echo Importing Ghana SOE and Judiciary PEPs...
docker exec -e SOURCE_DATABASE_URL=postgres://fraud:fraud@postgres:5432/fraud?sslmode=disable new-fraud-system-screening-service-1 /app/source-import -file /tmp/gh_pep_soe.json -dataset gh-pep-soe -snapshot-reference "Local Ghana PEP snapshot intake: gh_pep_soe.json"
if errorlevel 1 exit /b 1

echo Importing Ghana PEP Relatives and Close Associates (RCAs)...
docker exec -e SOURCE_DATABASE_URL=postgres://fraud:fraud@postgres:5432/fraud?sslmode=disable new-fraud-system-screening-service-1 /app/source-import -file /tmp/gh_pep_rca.json -dataset gh-pep-rca -snapshot-reference "Local Ghana PEP snapshot intake: gh_pep_rca.json"
if errorlevel 1 exit /b 1

echo Importing Ghana Sub-National MMDCEs and Regional Ministers...
docker exec -e SOURCE_DATABASE_URL=postgres://fraud:fraud@postgres:5432/fraud?sslmode=disable new-fraud-system-screening-service-1 /app/source-import -file /tmp/gh_pep_local_gov.json -dataset gh-pep-local-gov -snapshot-reference "Local Ghana PEP snapshot intake: gh_pep_local_gov.json"
if errorlevel 1 exit /b 1

echo Importing Ghana Political Party Leadership...
docker exec -e SOURCE_DATABASE_URL=postgres://fraud:fraud@postgres:5432/fraud?sslmode=disable new-fraud-system-screening-service-1 /app/source-import -file /tmp/gh_pep_parties.json -dataset gh-pep-parties -snapshot-reference "Local Ghana PEP snapshot intake: gh_pep_parties.json"
if errorlevel 1 exit /b 1

echo Importing Ghana Judicial Service Supreme Court PEPs...
docker exec -e SOURCE_DATABASE_URL=postgres://fraud:fraud@postgres:5432/fraud?sslmode=disable new-fraud-system-screening-service-1 /app/source-import -file /tmp/gh_pep_judiciary.json -dataset gh-pep-judiciary -snapshot-reference "Local Ghana PEP snapshot intake: gh_pep_judiciary.json"
if errorlevel 1 exit /b 1

echo Importing World Bank Debarred Firms and Individuals...
docker exec -e SOURCE_DATABASE_URL=postgres://fraud:fraud@postgres:5432/fraud?sslmode=disable new-fraud-system-screening-service-1 /app/source-import -file /tmp/worldbank_fixture.json -dataset worldbank-debarred -snapshot-reference "Official World Bank procurement sanctions intake"
if errorlevel 1 exit /b 1

echo ==============================================================================
echo [5/5] Activating generations and refreshing statistics...
echo ==============================================================================
docker exec new-fraud-system-postgres-1 psql -U fraud -d fraud -c "UPDATE screening.source_connections c SET verified_generation = d.active_generation, last_success_at = now(), last_error = '', rights_state = 'approved', enabled = true, max_stale = interval '30 days' FROM screening.source_datasets d WHERE (d.owner_scope, d.id) = (c.owner_scope, c.dataset_id) AND d.active_generation IS NOT NULL; SELECT screening.refresh_source_statistics_v1();"

echo All Ghana datasets successfully seeded and active!
