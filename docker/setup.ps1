param(
    [ValidateRange(1024,65535)][int]$Port = 8088,
    [switch]$Install,
    [switch]$SkipBuild,
    [string]$UsersFile,
    [switch]$CreateMissingUsers
)
$ErrorActionPreference = 'Stop'
if ($CreateMissingUsers -and !$UsersFile) { throw '-CreateMissingUsers requires -UsersFile' }
if ($UsersFile) { $UsersFile = (Resolve-Path -LiteralPath $UsersFile).Path }
$repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$local = Join-Path $repo '.local/mysql-dev'
$compose = Join-Path $PSScriptRoot 'compose.yaml'
$savedPort = $env:MYSQL_GATEWAY_PORT
$savedOrigin = $env:FRAUD_APP_ORIGIN
$savedUnified = $env:FRAUD_UNIFIED_PLATFORM
$savedIgnoreOrphans = $env:COMPOSE_IGNORE_ORPHANS
function Checked([string]$Program, [string[]]$Arguments) {
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Program failed. Correct the error above and rerun docker/setup.ps1. Existing data and credentials are retained." }
}
try {
    $env:MYSQL_GATEWAY_PORT = [string]$Port
    $env:FRAUD_APP_ORIGIN = "http://localhost:$Port"
    $env:FRAUD_UNIFIED_PLATFORM = '1'
    # Owner installers intentionally use subsets of this same project.
    $env:COMPOSE_IGNORE_ORPHANS = 'true'
    Checked 'node' @((Join-Path $repo 'backend/auth-service/scripts/setup-local-provider.mjs'))
    Checked 'node' @((Join-Path $PSScriptRoot 'mysql-dev/consolidate-provider.mjs'))
    Checked 'node' @((Join-Path $PSScriptRoot 'keycloak/configure-gateway-issuer.mjs'))

    $required = @('auth-runtime/config.json','case-runtime/config.json','screening-runtime/config.json','ingestion-runtime/config.json','decision-runtime/config.json','frontend.env','plans')
    $missing = @($required | Where-Object { -not (Test-Path -LiteralPath (Join-Path $local $_)) })
    $networkAligned = & node (Join-Path $PSScriptRoot 'mysql-dev/check-runtime-network.mjs')
    if ($LASTEXITCODE -ne 0) { throw 'Cannot verify restricted runtime account hosts' }
    $referenceId = & docker ps -a --filter 'name=^fraud-mysql-dev-mysql-reference-1$' --format '{{.ID}}'
    if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect the reference database' }
    $referencePersistent = $false
    if ($referenceId) {
        $referenceRaw = & docker inspect $referenceId --format '{{json .Mounts}}'
        if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect the reference database mounts' }
        $referenceMounts = $referenceRaw | ConvertFrom-Json
        $referencePersistent = @($referenceMounts | Where-Object { $_.Destination -eq '/var/lib/mysql' -and $_.Type -eq 'volume' -and $_.Name -eq 'fraud-mysql-dev_mysql_dev_reference_data' }).Count -eq 1
    }
    # The previous tmpfs reference loses its owner schema on every Docker stop.
    # Install it once into its own durable volume before normal runtime startup.
    $runInstall = $Install -or $missing.Count -or !$referencePersistent -or $networkAligned -ne 'true'
    if ($runInstall) {
        $arguments = @('-NoProfile','-ExecutionPolicy','Bypass','-File',(Join-Path $PSScriptRoot 'mysql-dev/install-services.ps1'))
        if ($UsersFile) { $arguments += @('-UsersFile',$UsersFile) }
        if ($CreateMissingUsers) { $arguments += '-CreateMissingUsers' }
        Checked 'powershell' $arguments
    } else {
        # Reuse installed schemas and backend images during a configuration-only
        # consolidation. Owner installers run only on first setup or -Install.
        Checked 'node' @((Join-Path $repo 'frontend/scripts/configure-mysql-login.mjs'))
        Checked 'node' @((Join-Path $repo 'frontend/scripts/prepare-mysql.mjs'))
        if (!$SkipBuild) { Checked 'docker' @('compose','-f',$compose,'build','frontend') }
    }
    Checked 'docker' @('compose','-f',$compose,'config','--quiet')
    # Remove the old proxy project before claiming its port inside the platform.
    # That file declares only the proxy and an external application network.
    $legacyGateway = & docker ps -a --filter 'label=com.docker.compose.project=fraud-mysql-gateway' --format '{{.ID}}'
    if ($LASTEXITCODE -ne 0) { throw 'Cannot inspect the legacy gateway project' }
    if ($legacyGateway) {
        Checked 'docker' @('compose','-p','fraud-mysql-gateway','-f',(Join-Path $repo 'compose.mysql-gateway.yaml'),'down')
    }
    # Load changed runtime files and avoid stale health states when the database
    # container needs recreation (for example when removing its old host port).
    $runtime = @('api-gateway','frontend','data-model-service','data-model-worker','data-model-author','ingestion-service','ingestion-worker','decision-engine-service','decision-engine-worker','decision-local-providers','auth-service','auth-worker','case-manager-service','case-manager-worker','screening-service','screening-worker')
    Checked 'docker' (@('compose','-f',$compose,'stop') + $runtime)
    Checked 'docker' @('compose','-f',$compose,'up','-d','--no-build','--wait','mysql','mysql-reference','keycloak','auth-mail')
    # Runtime dependencies deliberately exclude installer jobs. Starting a
    # retained platform must not replay grants/migrations underneath API traffic.
    Checked 'docker' @('compose','-f',$compose,'up','-d','--no-build','--wait')
    if ($UsersFile -and !$runInstall) {
        $userArguments = @((Join-Path $repo 'backend/auth-service/scripts/setup-mysql-users.mjs'),'--file',$UsersFile,'--data-model-url',"http://localhost:$Port/services/data-model",'--apply')
        if ($CreateMissingUsers) { $userArguments += '--create-missing' }
        Checked 'node' $userArguments
    }
    Checked 'node' @((Join-Path $PSScriptRoot 'mysql-dev/verify-gateway.mjs'),'--port',[string]$Port)
    Write-Output "Platform ready: http://localhost:$Port/ ; API references: http://localhost:$Port/services/"
    Write-Output "Docker Desktop project: fraud-mysql-dev. Only port $Port is published; Keycloak: http://localhost:$Port/identity/"
} finally {
    $env:MYSQL_GATEWAY_PORT = $savedPort
    $env:FRAUD_APP_ORIGIN = $savedOrigin
    $env:FRAUD_UNIFIED_PLATFORM = $savedUnified
    $env:COMPOSE_IGNORE_ORPHANS = $savedIgnoreOrphans
}
