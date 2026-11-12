# Compatibility entry point. All platform setup now uses docker/setup.ps1.
param([string]$UsersFile, [switch]$CreateMissingUsers, [ValidateRange(1024,65535)][int]$Port = 8088, [switch]$Install, [switch]$SkipBuild)
& (Join-Path $PSScriptRoot '../setup.ps1') @PSBoundParameters
