$ErrorActionPreference = 'Stop'
$service = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$repo = [IO.Path]::GetFullPath((Join-Path $service '../..'))
$local = Join-Path $repo '.local/mysql-dev'
function Checked([string]$Program,[string[]]$Arguments) {
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Program failed; see output above" }
}
$compose = @('compose','--profile','install','--env-file',(Join-Path $local 'compose.env'),'-f',(Join-Path $service 'docker-compose-mysql.yml'))
Checked 'docker' ($compose + @('up','-d','--wait','mysql','mysql-reference'))
Checked 'docker' ($compose + @('stop','screening-service','screening-worker'))
Checked 'node' @((Join-Path $service 'scripts/prepare-mysql.mjs'),'--reconcile-network')
Checked 'docker' ($compose + @('build','migrate-screening'))
Checked 'docker' ($compose + @('run','--rm','--no-deps','migrate-screening'))
Checked 'docker' ($compose + @('up','-d','--no-deps','--wait','screening-service','screening-worker'))
Write-Output 'Screening API: http://localhost:8085/docs. Matching uses a labelled synthetic local development catalog.'
Write-Output 'Human routes need an auth access JWT. Owner routes need the separate private operator and service credentials.'
