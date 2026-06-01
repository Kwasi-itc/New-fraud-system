param(
    [Parameter(Mandatory = $true)]
    [string]$BinaryName,

    [Parameter(Mandatory = $true)]
    [string]$Package,

    [string[]]$AppArgs = @()
)

$ErrorActionPreference = "Stop"

New-Item -ItemType Directory -Force ".bin" | Out-Null

$binaryPath = Join-Path ".bin" $BinaryName
$binaryFullPath = Join-Path (Get-Location) $binaryPath

& go build -o $binaryPath $Package
if ($LASTEXITCODE -ne 0) {
    throw "go build failed with exit code $LASTEXITCODE"
}

& $binaryFullPath @AppArgs
if ($LASTEXITCODE -ne 0) {
    throw "$BinaryName exited with code $LASTEXITCODE"
}
