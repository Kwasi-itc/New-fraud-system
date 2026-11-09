param([ValidateRange(1024,65535)][int]$Port = 3307)
$ErrorActionPreference = 'Stop'
$repo = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$directory = Join-Path $repo '.local/mysql-dev'
$null = New-Item -ItemType Directory -Path $directory -Force
$null = New-Item -ItemType Directory -Path (Join-Path $directory 'plans') -Force
foreach ($secretName in @('root-password.txt', 'data-model-password.txt')) {
$secret = Join-Path $directory $secretName
if (-not (Test-Path -LiteralPath $secret)) {
    $bytes = New-Object byte[] 32
    $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
    $password = [BitConverter]::ToString($bytes).Replace('-','').ToLowerInvariant()
    # Never overwrite credentials: initialized MySQL volumes retain the old value.
    $stream = [IO.File]::Open($secret,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::None)
    try {
        $raw = [Text.Encoding]::ASCII.GetBytes($password)
        $stream.Write($raw,0,$raw.Length)
    } finally { $stream.Dispose() }
}
$existing = [IO.File]::ReadAllText($secret)
if ($existing -notmatch '^[a-f0-9]{64}$') { throw 'Existing local password file is invalid; inspect it without resetting the database.' }
}
$envPath = Join-Path $directory 'compose.env'
if (-not (Test-Path -LiteralPath $envPath)) {
    [IO.File]::WriteAllText($envPath,"MYSQL_DEV_PORT=$Port`n",(New-Object Text.UTF8Encoding($false)))
}
Write-Output 'Local MySQL configuration is ready. Existing password, port settings and data were preserved.'
Write-Output 'Start: docker compose --env-file .local/mysql-dev/compose.env -f compose.mysql-dev.yaml up -d --wait'
Write-Output 'This command starts MySQL only. For local data-model/ingestion APIs, see docker/mysql-dev/README.md.'
