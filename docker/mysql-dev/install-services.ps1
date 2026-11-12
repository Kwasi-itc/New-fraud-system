# Owner installers used by docker/setup.ps1 for first setup and explicit upgrades.
param([string]$UsersFile, [switch]$CreateMissingUsers)
$ErrorActionPreference = 'Stop'
if ($CreateMissingUsers -and !$UsersFile) { throw '-CreateMissingUsers requires -UsersFile' }
if ($UsersFile) { $UsersFile = (Resolve-Path -LiteralPath $UsersFile).Path }
$repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
function Setup([string]$RelativePath, [string[]]$Extra = @()) {
    Write-Output "Starting $RelativePath"
    & powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $repo $RelativePath) @Extra
    if ($LASTEXITCODE -ne 0) { throw "MySQL platform setup stopped at $RelativePath. Correct that failure and rerun docker/setup.ps1 -Install; application data and secrets are retained." }
}
Setup 'backend/ingestion-service/scripts/setup-mysql.ps1'
Setup 'backend/decision-engine-service/scripts/setup-mysql.ps1'
$authArguments = @('-SkipProviderStartup')
if ($UsersFile) { $authArguments += @('-UsersFile',$UsersFile) }
if ($CreateMissingUsers) { $authArguments += '-CreateMissingUsers' }
Setup 'backend/auth-service/scripts/setup-mysql.ps1' $authArguments
Setup 'backend/case-manager-service/scripts/setup-mysql.ps1'
Setup 'backend/screening-service/scripts/setup-mysql.ps1'
Setup 'frontend/scripts/setup-mysql.ps1'
