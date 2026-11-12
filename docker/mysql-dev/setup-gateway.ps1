# Compatibility entry point: the gateway now belongs to the full platform.
param([ValidateRange(1024,65535)][int]$Port = 8088)
& (Join-Path $PSScriptRoot '../setup.ps1') @PSBoundParameters
