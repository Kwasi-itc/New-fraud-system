$ErrorActionPreference = 'Stop'
$service = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$repo = [IO.Path]::GetFullPath((Join-Path $service '../..'))
$local = Join-Path $repo '.local/mysql-dev'
function Checked([string]$Program,[string[]]$Arguments) {
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Program failed; see output above" }
}
Checked 'node' @((Join-Path $service 'scripts/prepare-mysql.mjs'))
$compose = @('compose','--profile','install','--env-file',(Join-Path $local 'compose.env'),'-f',(Join-Path $service 'docker-compose-mysql.yml'))
Checked 'docker' ($compose + @('up','-d','--wait','mysql','mysql-reference'))
Checked 'docker' ($compose + @('stop','case-manager-service','case-manager-worker'))
Checked 'docker' ($compose + @('build','migrate-case'))
Checked 'docker' ($compose + @('run','--rm','--no-deps','migrate-case'))
Checked 'docker' ($compose + @('up','-d','--no-deps','--wait','case-manager-service','case-manager-worker'))
Write-Output 'Case API: http://localhost:8086/docs. Use an auth access token with case roles and the matching tenant.'
Write-Output 'Service callbacks require the generated case service credential and an active registered tenant.'
