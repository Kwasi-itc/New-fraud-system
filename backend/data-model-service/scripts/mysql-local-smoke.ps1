# Runs against the local development API. Retains one named test tenant;
# never deletes records or resets the shared database.
$ErrorActionPreference = 'Stop'
$base = 'http://localhost:8080'
$docs = Invoke-WebRequest -UseBasicParsing "$base/docs"
if ($docs.StatusCode -ne 200 -or $docs.Content -notmatch 'SwaggerUIBundle') { throw 'Swagger page unavailable' }
$spec = Invoke-RestMethod "$base/openapi.json"
if ($spec.openapi -ne '3.0.3') { throw 'OpenAPI document unavailable' }
$ready = Invoke-RestMethod "$base/readyz"
if ($ready.database -ne 'mysql' -or $ready.status -ne 'ready') { throw 'MySQL not ready' }
$catalog = Invoke-RestMethod "$base/v1/tenants"
$matches = @($catalog.tenants | Where-Object { $_.external_key -eq 'local-compose-smoke' })
if ($matches.Count -gt 1) { throw 'Ambiguous smoke tenant; inspect catalogue' }
if ($matches.Count -eq 0) {
    $created = Invoke-RestMethod -Method Post -Uri "$base/v1/tenants" -ContentType 'application/json' -Body '{"name":"MySQL Compose smoke test","external_key":"local-compose-smoke"}'
    $record = $created.tenant
} else {
    $record = $matches[0]
}
if (-not $record.id -or $record.status -ne 'pending') { throw 'Expected pending smoke tenant' }
$lookup = Invoke-RestMethod "$base/v1/tenants/$($record.id)"
if ($lookup.tenant.id -ne $record.id -or $lookup.tenant.external_key -ne $record.external_key) { throw 'Tenant lookup returned incorrect record' }
$missingID = [guid]::NewGuid().ToString()
try {
    $null = Invoke-WebRequest -UseBasicParsing "$base/v1/tenants/$missingID"
    throw 'Nonexistent tenant accepted'
} catch {
    if (-not $_.Exception.Response -or [int]$_.Exception.Response.StatusCode -ne 404) { throw }
}
$model = Invoke-RestMethod "$base/v1/tenants/$($record.id)/data-model"
if (-not $model.data_model) { throw 'Model read failed' }
$catalog = Invoke-RestMethod "$base/v1/tenants"
if ($catalog.tenants.id -notcontains $record.id) { throw 'Registered tenant not retained' }
try {
    $null = Invoke-WebRequest -UseBasicParsing "$base/v1/tenants" -Headers @{Origin='http://evil.example'}
    throw 'Foreign origin accepted'
} catch {
    if (-not $_.Exception.Response -or [int]$_.Exception.Response.StatusCode -ne 403) { throw }
}
Write-Output "PASS: docs, OpenAPI, MySQL readiness, registration/catalogue/model reads and foreign-origin rejection. Retained tenant: $($record.id)"
