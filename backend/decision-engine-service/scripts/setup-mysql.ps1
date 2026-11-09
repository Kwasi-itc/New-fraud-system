param([string]$TenantId)
$ErrorActionPreference = 'Stop'
$service = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$repo = [IO.Path]::GetFullPath((Join-Path $service '../..'))
$local = Join-Path $repo '.local/mysql-dev'
$ingestionEnv = Join-Path $local 'ingestion.env'
$ready = $false
try { $ready = (Invoke-RestMethod http://localhost:8081/readyz -TimeoutSec 5).status -eq 'ready' } catch { }
if (-not $ready -or -not (Test-Path -LiteralPath $ingestionEnv)) {
    & (Join-Path $service '../ingestion-service/scripts/setup-mysql.ps1')
}
if (-not $TenantId) {
    foreach ($line in [IO.File]::ReadAllLines($ingestionEnv)) {
        if ($line -match '^INGESTION_TENANT_ID=(.+)$') { $TenantId = $Matches[1] }
    }
}
if (-not $TenantId -or [guid]::Parse($TenantId) -eq [guid]::Empty) { throw 'A valid prepared tenant ID is required' }
if ((Invoke-RestMethod "http://localhost:8080/v1/tenants/$TenantId").tenant.status -ne 'active') { throw 'Selected data-model tenant must be active' }
foreach ($name in @('decision-api-password.txt','decision-worker-password.txt','decision-runtime-token.txt','decision-review-session.txt','decision-provider-password.txt','decision-provider-token.txt')) {
    $path = Join-Path $local $name
    if (-not (Test-Path -LiteralPath $path)) {
        $bytes = New-Object byte[] 32
        $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
        try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
        $value = [BitConverter]::ToString($bytes).Replace('-','').ToLowerInvariant()
        $stream = [IO.File]::Open($path,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::None)
        try { $raw = [Text.Encoding]::ASCII.GetBytes($value); $stream.Write($raw,0,$raw.Length) } finally { $stream.Dispose() }
    }
    if ([IO.File]::ReadAllText($path) -cnotmatch '^[a-f0-9]{64}$') { throw "Invalid existing secret: $name" }
}
$null = New-Item -ItemType Directory -Path (Join-Path $local 'decision-runtime') -Force
$subnets = & docker network inspect fraud-mysql-dev_default --format '{{range .IPAM.Config}}{{println .Subnet}}{{end}}'
if ($LASTEXITCODE -ne 0) { throw 'Shared MySQL Docker network unavailable' }
$subnet = @($subnets | Where-Object { $_ -match '^\d+\.\d+\.\d+\.\d+/\d+$' })
if ($subnet.Count -ne 1) { throw 'Exactly one shared IPv4 subnet is required for restricted account hosts' }
$scope = Join-Path $local 'decision.env'
[IO.File]::WriteAllText($scope,"DECISION_TENANT_ID=$TenantId`nDECISION_MYSQL_SUBNET=$($subnet[0])`n",(New-Object Text.UTF8Encoding($false)))
$compose = @('compose','--env-file',(Join-Path $local 'compose.env'),'--env-file',$ingestionEnv,'--env-file',$scope,'-f',(Join-Path $service 'docker-compose-mysql.yml'))
function Invoke-Compose([string[]]$Arguments) {
    & docker @compose @Arguments
    if ($LASTEXITCODE -ne 0) { throw "Compose failed: $($Arguments -join ' ')" }
}
Invoke-Compose -Arguments @('stop','decision-engine-service','decision-engine-worker','decision-local-providers')
Invoke-Compose -Arguments @('build','migrate-decisions')
Invoke-Compose -Arguments @('run','--rm','--no-deps','migrate-decisions')
Invoke-Compose -Arguments @('up','-d','--no-deps','--wait','decision-local-providers','decision-engine-worker','decision-engine-service')
$docs = Invoke-WebRequest -UseBasicParsing http://localhost:8082/docs -TimeoutSec 10
if ($docs.StatusCode -ne 200 -or $docs.Content -notmatch 'SwaggerUIBundle') { throw 'Decision Swagger is unavailable' }
Write-Output 'Ready: http://localhost:8082/docs ; capabilities: http://localhost:8082/capabilities'
Write-Output "Tenants: all active registered tenants ; default fixture tenant: $TenantId ; shared MySQL: fraud"
$configured = Get-Content -LiteralPath (Join-Path $local 'decision-runtime/config.json') -Raw | ConvertFrom-Json
Write-Output "Development reviewer: $($configured.ReviewerID) ; review inbox: $($configured.ReviewInboxID)"
