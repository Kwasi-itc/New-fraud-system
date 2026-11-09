# Retains smoke records and upload history in the selected local scope.
param([string]$Base = 'http://localhost:8081')
$ErrorActionPreference = 'Stop'
$repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../..'))
$scope = @{}
foreach ($line in [IO.File]::ReadAllLines((Join-Path $repo '.local/mysql-dev/ingestion.env'))) {
    if ($line -match '^(INGESTION_TENANT_ID|INGESTION_OBJECT_TYPE)=(.+)$') { $scope[$Matches[1]] = $Matches[2] }
}
$tenant = $scope['INGESTION_TENANT_ID']
$object = $scope['INGESTION_OBJECT_TYPE']
if (-not $tenant -or -not $object) { throw 'Run setup-mysql.ps1 first' }
function Call-API([string]$Method, [string]$Path, $Body, [string]$Key) {
    $parameters = @{ Method = $Method; Uri = "$Base$Path"; TimeoutSec = 30 }
    if ($null -ne $Body) { $parameters.ContentType = 'application/json'; $parameters.Body = ($Body | ConvertTo-Json -Depth 12 -Compress) }
    if ($Key) { $parameters.Headers = @{ 'Idempotency-Key' = $Key } }
    return Invoke-RestMethod @parameters
}
foreach ($page in @(@{path='/docs'; text='SwaggerUIBundle'}, @{path='/openapi.yaml'; text='openapi: 3.0.3'}, @{path='/redoc'; text='redoc.standalone.js'})) {
    $response = Invoke-WebRequest -UseBasicParsing -Uri "$Base$($page.path)"
    $content = $response.Content
    if ($content -is [byte[]]) { $content = [Text.Encoding]::UTF8.GetString($content) }
    if ($response.StatusCode -ne 200 -or -not $content.Contains($page.text)) { throw "Unavailable documentation: $($page.path)" }
}
if ((Call-API 'Get' '/readyz' $null '').status -ne 'ready') { throw 'MySQL API not ready' }
$path = "/v1/tenants/$tenant/ingest/$object"
$id = 'local-smoke-' + [guid]::NewGuid().ToString('N')
$key = [guid]::NewGuid().ToString()
# System object_id works with custom tables that have no other required fields.
$body = @{object_id=$id}
$first = Call-API 'Post' $path $body $key
if ($first.result.object_id -ne $id -or $first.result.action -ne 'created') { throw 'Record creation failed' }
$again = Call-API 'Post' $path $body $key
if (-not $again.result.replayed) { throw 'Idempotent replay failed' }
$null = Call-API 'Patch' $path $body ([guid]::NewGuid().ToString())
$read = Call-API 'Get' "/v1/tenants/$tenant/records/$object/$id" $null ''
if ($read.record.object_id -ne $id) { throw 'Record readback failed' }
try {
    $null = Call-API 'Post' $path @{object_id="$id-conflict"} $key
    throw 'Changed idempotent request accepted'
} catch {
    if (-not $_.Exception.Response -or [int]$_.Exception.Response.StatusCode -ne 409) { throw }
}
try {
    $null = Call-API 'Post' "/v1/tenants/$([guid]::NewGuid())/ingest/$object" $body ''
    throw 'Foreign tenant accepted'
} catch {
    if (-not $_.Exception.Response -or [int]$_.Exception.Response.StatusCode -ne 403) { throw }
}
try {
    $null = Invoke-WebRequest -UseBasicParsing -Uri "$Base/docs" -Headers @{Origin='http://evil.example'}
    throw 'Foreign browser origin accepted'
} catch {
    if (-not $_.Exception.Response -or [int]$_.Exception.Response.StatusCode -ne 403) { throw }
}

Add-Type -AssemblyName System.Net.Http
$client = New-Object Net.Http.HttpClient
$multipart = New-Object Net.Http.MultipartFormDataContent
$csv = New-Object Net.Http.StringContent("object_id`n$id-csv`n")
$csv.Headers.ContentType = New-Object Net.Http.Headers.MediaTypeHeaderValue('text/csv')
$multipart.Add($csv, 'file', 'local-smoke.csv')
try {
    $response = $client.PostAsync("$Base$path/csv", $multipart).GetAwaiter().GetResult()
    $wire = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
    if (-not $response.IsSuccessStatusCode) { throw "CSV submission failed: $wire" }
    $upload = ($wire | ConvertFrom-Json).upload_log
    if (-not $upload.id) { throw "No upload identity returned: $wire" }
    $response.Dispose()
} finally { $multipart.Dispose(); $client.Dispose() }
$deadline = [DateTime]::UtcNow.AddSeconds(45)
do {
    $log = (Call-API 'Get' "/v1/upload-logs/$($upload.id)" $null '').upload_log
    if ($log.status -eq 'completed') { break }
    if ($log.status -eq 'failed') { throw "CSV worker failed: $($log.error_message)" }
    Start-Sleep -Seconds 1
} while ([DateTime]::UtcNow -lt $deadline)
if ($log.status -ne 'completed' -or $log.successful_rows -ne 1) { throw 'CSV worker did not complete one row' }
$read = Call-API 'Get' "/v1/tenants/$tenant/records/$object/$id-csv" $null ''
if ($read.record.object_id -ne "$id-csv") { throw 'Worker record readback failed' }
Write-Output "PASS: docs, readiness, create/PATCH/read, replay/conflict, tenant and origin rejection, CSV worker. Tenant: $tenant ; retained records: $id, $id-csv"
