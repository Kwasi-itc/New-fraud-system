param([switch]$SkipProviderStartup, [string]$UsersFile, [switch]$CreateMissingUsers)
$ErrorActionPreference = 'Stop'
if ($CreateMissingUsers -and !$UsersFile) { throw '-CreateMissingUsers requires -UsersFile' }
if ($UsersFile) { $UsersFile = (Resolve-Path -LiteralPath $UsersFile).Path }
$service = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$repo = [IO.Path]::GetFullPath((Join-Path $service '../..'))
$local = Join-Path $repo '.local/mysql-dev'
& (Join-Path $repo 'docker/mysql-dev/setup.ps1')
function Checked([string]$Program,[string[]]$Arguments) {
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Program failed; see output above" }
}
Checked 'node' @((Join-Path $service 'scripts/setup-local-provider.mjs'))
if (!$SkipProviderStartup) {
    Checked 'node' @((Join-Path $repo 'docker/mysql-dev/consolidate-provider.mjs'))
    Checked 'node' @((Join-Path $repo 'docker/keycloak/configure-gateway-issuer.mjs'))
}
$compose = @('compose','--profile','install','--env-file',(Join-Path $local 'compose.env'),'-f',(Join-Path $service 'docker-compose-mysql.yml'))
Checked 'docker' ($compose + @('up','-d','--wait','mysql','mysql-reference'))
Checked 'docker' ($compose + @('stop','auth-service','auth-worker'))
Checked 'node' @((Join-Path $service 'scripts/prepare-mysql.mjs'),'--reconcile-network')
Checked 'docker' ($compose + @('build','migrate-auth'))
Checked 'docker' ($compose + @('run','--rm','--no-deps','migrate-auth'))
Checked 'docker' ($compose + @('up','-d','--no-deps','--wait','auth-mail','auth-service','auth-worker'))
if ($UsersFile) {
    $userArguments = @((Join-Path $service 'scripts/setup-mysql-users.mjs'),'--file',$UsersFile,'--apply')
    if ($CreateMissingUsers) { $userArguments += '--create-missing' }
    Checked 'node' $userArguments
}
Write-Output 'Auth API: http://localhost:8087/docs ; local invitation mailbox: http://localhost:8027'
Write-Output 'Proxy credential: ignored .local/mysql-dev/auth-proxy-token.txt. Real sessions and membership permissions remain required.'
