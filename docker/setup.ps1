# Windows compatibility entry point; all setup logic is shared with macOS/Linux.
param(
    [ValidateRange(1024,65535)][int]$Port = 8088,
    [switch]$Install,
    [switch]$SkipBuild,
    [string]$UsersFile,
    [switch]$CreateMissingUsers
)
$ErrorActionPreference = 'Stop'
$setupArguments = @((Join-Path $PSScriptRoot 'setup.mjs'), '--port', [string]$Port)
if ($Install) { $setupArguments += '--install' }
if ($SkipBuild) { $setupArguments += '--skip-build' }
if ($UsersFile) { $setupArguments += @('--users-file', $UsersFile) }
if ($CreateMissingUsers) { $setupArguments += '--create-missing-users' }
& node @setupArguments
if ($LASTEXITCODE -ne 0) { throw 'Platform setup failed; see the error above. Existing data and credentials are retained.' }
