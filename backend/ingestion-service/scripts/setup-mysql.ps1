param(
    [string]$TenantId,
    [ValidatePattern('^[a-z][a-z0-9_]{0,62}$')][string]$ObjectType = 'transactions'
)
$ErrorActionPreference = 'Stop'
$service = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$repo = [IO.Path]::GetFullPath((Join-Path $service '../..'))
$local = Join-Path $repo '.local/mysql-dev'
& (Join-Path $repo 'docker/mysql-dev/setup.ps1')
$runtime = Join-Path $local 'ingestion-runtime'
$null = New-Item -ItemType Directory -Path $runtime -Force
foreach ($name in @('ingestion-api-password.txt', 'ingestion-worker-password.txt', 'ingestion-runtime-token.txt')) {
    $path = Join-Path $local $name
    if (-not (Test-Path -LiteralPath $path)) {
        $bytes = New-Object byte[] 32
        $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
        try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
        $value = [BitConverter]::ToString($bytes).Replace('-', '').ToLowerInvariant()
        $stream = [IO.File]::Open($path, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
        try { $raw = [Text.Encoding]::ASCII.GetBytes($value); $stream.Write($raw, 0, $raw.Length) } finally { $stream.Dispose() }
    }
    if ([IO.File]::ReadAllText($path) -cnotmatch '^[a-f0-9]{64}$') { throw "Invalid existing local secret: $name" }
}
$scopePath = Join-Path $local 'ingestion.env'
if (Test-Path -LiteralPath $scopePath) {
    $saved = @{}
    foreach ($line in [IO.File]::ReadAllLines($scopePath)) {
        if ($line -match '^(INGESTION_TENANT_ID|INGESTION_OBJECT_TYPE)=(.+)$') { $saved[$Matches[1]] = $Matches[2] }
    }
    if (-not $TenantId) { $TenantId = $saved['INGESTION_TENANT_ID'] }
    if (-not $PSBoundParameters.ContainsKey('ObjectType') -and $saved['INGESTION_OBJECT_TYPE']) { $ObjectType = $saved['INGESTION_OBJECT_TYPE'] }
}
if ($TenantId -and ([guid]::Parse($TenantId) -eq [guid]::Empty)) { throw 'TenantId must be nonzero' }
if ($ObjectType -cnotmatch '^[a-z][a-z0-9_]{0,62}$') { throw 'Invalid saved object type' }

$compose = @('compose', '--env-file', (Join-Path $local 'compose.env'), '-f', (Join-Path $service 'docker-compose-mysql.yml'))
function Invoke-Compose([string[]]$Arguments) {
    & docker @compose @Arguments
    if ($LASTEXITCODE -ne 0) { throw "Docker Compose failed: $($Arguments -join ' ')" }
}
function Wait-API([string]$Base) {
    $deadline = [DateTime]::UtcNow.AddMinutes(2)
    do {
        try {
            $ready = Invoke-RestMethod -Uri "$Base/readyz" -TimeoutSec 10
            if ($ready.status -eq 'ready') { return }
        } catch { }
        Start-Sleep -Seconds 2
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "API did not become ready: $Base"
}
function Invoke-Model([string]$Method, [string]$Path, $Body) {
    $parameters = @{ Method = $Method; Uri = "http://localhost:8080$Path"; TimeoutSec = 90 }
    if ($null -ne $Body) { $parameters.ContentType = 'application/json'; $parameters.Body = ($Body | ConvertTo-Json -Depth 12 -Compress) }
    # Local table/field commands derive stable operation keys when omitted.
    # Retrying 503 therefore preserves the exact command's identity.
    for ($attempt = 0; $attempt -lt 8; $attempt++) {
        try { return Invoke-RestMethod @parameters } catch {
            if (-not $_.Exception.Response -or [int]$_.Exception.Response.StatusCode -ne 503 -or $attempt -eq 7) { throw }
            Start-Sleep -Seconds 2
        }
    }
}

Invoke-Compose -Arguments @('up', '-d', '--build', 'data-model-service', 'data-model-worker')
Wait-API 'http://localhost:8080'
if (-not $TenantId) {
    $catalog = Invoke-Model 'Get' '/v1/tenants' $null
    $found = @($catalog.tenants | Where-Object { $_.external_key -ceq 'local-ingestion-development' })
    if ($found.Count -gt 1) { throw 'Ambiguous development tenant; specify -TenantId' }
    if ($found.Count -eq 0) {
        $created = Invoke-Model 'Post' '/v1/tenants' @{ name = 'Local ingestion development'; external_key = 'local-ingestion-development' }
        $TenantId = $created.tenant.id
    } else { $TenantId = $found[0].id }
}
if (-not $TenantId -or [guid]::Parse($TenantId) -eq [guid]::Empty) { throw 'No valid tenant returned' }
# Retain identity immediately, before schema setup, so interrupted runs resume.
[IO.File]::WriteAllText($scopePath, "INGESTION_TENANT_ID=$TenantId`nINGESTION_OBJECT_TYPE=$ObjectType`n", (New-Object Text.UTF8Encoding($false)))
$tenant = (Invoke-Model 'Get' "/v1/tenants/$TenantId" $null).tenant
if ($tenant.status -eq 'pending') { $null = Invoke-Model 'Post' "/v1/tenants/$TenantId/provision" $null }
elseif ($tenant.status -ne 'active') { throw 'Selected tenant must be pending or active' }
$tables = (Invoke-Model 'Get' "/v1/tenants/$TenantId/tables" $null).tables
$found = @($tables | Where-Object { $_.name -ceq $ObjectType -and -not $_.archived })
if ($found.Count -gt 1) { throw 'Ambiguous published table' }
if ($found.Count -eq 0) {
    $table = (Invoke-Model 'Post' "/v1/tenants/$TenantId/tables" @{ name = $ObjectType }).table
} else { $table = $found[0] }
if ($found.Count -eq 0 -or $tenant.external_key -ceq 'local-ingestion-development') {
    # Resume interrupted demo setup without changing existing custom tables.
    $fields = (Invoke-Model 'Get' "/v1/tables/$($table.id)/fields" $null).fields
    foreach ($field in @(@{name = 'amount'; data_type = 'float'; nullable = $true}, @{name = 'currency'; data_type = 'string'; nullable = $true})) {
        if (-not @($fields | Where-Object { $_.name -ceq $field.name -and -not $_.archived }).Count) {
            $null = Invoke-Model 'Post' "/v1/tables/$($table.id)/fields" $field
        }
    }
}
$compose = @('compose', '--env-file', (Join-Path $local 'compose.env'), '--env-file', $scopePath, '-f', (Join-Path $service 'docker-compose-mysql.yml'))
# Avoid revoking/reapplying local account grants underneath running ingestion.
Invoke-Compose -Arguments @('stop', 'ingestion-service', 'ingestion-worker')
Invoke-Compose -Arguments @('build', 'migrate-ingestion')
Invoke-Compose -Arguments @('run', '--rm', '--no-deps', 'migrate-ingestion')
Invoke-Compose -Arguments @('up', '-d', '--no-deps', '--wait', 'ingestion-service', 'ingestion-worker')
Wait-API 'http://localhost:8081'
Write-Output "Ready: http://localhost:8081/docs"
Write-Output "Tenant: $TenantId ; object type: $ObjectType"
Write-Output 'Data-model: http://localhost:8080/docs ; shared MySQL database: fraud'
