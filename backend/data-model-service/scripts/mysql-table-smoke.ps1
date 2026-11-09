param(
    [Parameter(Mandatory=$true)][guid]$TenantId,
    [string]$Name = 'transactions',
    [string]$Alias = 'Transactions',
    [string]$Description = 'Card and transfer transactions'
)
# Creates/replays the named local table. Does not delete records or reset data.
$ErrorActionPreference = 'Stop'
$base = 'http://localhost:8080'
$uri = "$base/v1/tenants/$TenantId/tables"
$body = @{name=$Name;alias=$Alias;description=$Description;semantic_type='entity'} | ConvertTo-Json -Compress
$first = Invoke-RestMethod -Method Post -Uri $uri -ContentType 'application/json' -Body $body -TimeoutSec 75
$second = Invoke-RestMethod -Method Post -Uri $uri -ContentType 'application/json' -Body $body -TimeoutSec 75
if (-not $first.table.id -or $first.table.id -ne $second.table.id -or $first.table.created_at -ne $second.table.created_at) { throw 'Creation replay changed result' }
$id = $first.table.id
$fields = Invoke-RestMethod "$base/v1/tables/$id/fields"
foreach ($required in @('object_id','updated_at')) {
    if (@($fields.fields | Where-Object {$_.name -eq $required}).Count -ne 1) { throw "Missing mandatory field: $required" }
}
foreach ($path in @("/v1/tenants/$TenantId/tables", "/v1/tenants/$TenantId/links", "/v1/tenants/$TenantId/pivots", "/v1/tables/$id/options", "/v1/tables/$id/navigation-options", "/v1/tenants/$TenantId/schema-change-log", "/v1/tenants/$TenantId/schema-migrations", "/v1/tenants/$TenantId/index-jobs", "/v1/tenants/$TenantId/data-model")) {
    $null = Invoke-RestMethod "$base$path"
}
$changed = @{name=$Name;alias='Different command';description=$Description;semantic_type='entity'} | ConvertTo-Json -Compress
try {
    $null = Invoke-WebRequest -UseBasicParsing -Method Post -Uri $uri -ContentType 'application/json' -Body $changed -TimeoutSec 75
    throw 'Changed creation command accepted'
} catch {
    if (-not $_.Exception.Response -or [int]$_.Exception.Response.StatusCode -ne 409) { throw }
}
Write-Output "PASS: table creation/replay, changed-command conflict, mandatory fields and metadata reads. Retained table: $id"
