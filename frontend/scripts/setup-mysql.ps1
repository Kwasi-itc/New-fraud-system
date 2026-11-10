$ErrorActionPreference = 'Stop'
$frontend = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
function Checked([string]$Program,[string[]]$Arguments) {
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Program failed; see output above" }
}
Checked 'node' @((Join-Path $PSScriptRoot 'configure-mysql-login.mjs'))
Checked 'node' @((Join-Path $PSScriptRoot 'prepare-mysql.mjs'))
Checked 'node' @((Join-Path $PSScriptRoot 'configure-mysql-storage.mjs'))
$backend = @('compose','-f',(Join-Path $frontend '../backend/decision-engine-service/docker-compose-mysql.yml'))
Checked 'docker' ($backend + @('build','migrate-data-model','migrate-ingestion','migrate-decisions'))
Checked 'docker' ($backend + @('run','--rm','--no-deps','migrate-decisions','/app/install-decisions','--grant-local-browser'))
Checked 'docker' ($backend + @('up','-d','--no-deps','--no-build','--wait','data-model-author','data-model-service','data-model-worker','ingestion-service','ingestion-worker','decision-engine-service'))
Checked 'docker' @('compose','--profile','install','--env-file',(Join-Path $frontend '../.local/mysql-dev/compose.env'),'-f',(Join-Path $frontend '../backend/auth-service/docker-compose-mysql.yml'),'up','-d','--no-deps','--force-recreate','--wait','auth-service','auth-worker')
$compose = @('compose','-f',(Join-Path $frontend 'docker-compose-mysql.yml'))
Checked 'docker' @('compose','-f',(Join-Path $frontend '../backend/data-model-service/docker-compose-mysql.yml'),'run','--rm','--no-deps','data-model-author','/app/mysql-local-author','--sync-local-consumers')
Checked 'docker' ($compose + @('build','frontend'))
Checked 'docker' ($compose + @('up','-d','--no-deps','--wait','frontend'))
Write-Output 'Authenticated frontend: http://localhost:3002. Sign in with a provisioned Keycloak account.'
