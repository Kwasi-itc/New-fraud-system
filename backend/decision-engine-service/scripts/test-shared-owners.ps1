param(
    [Parameter(Mandatory = $true)][string]$EvidenceDirectory
)
$ErrorActionPreference = 'Stop'
$serviceRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$evidencePath = [IO.Path]::GetFullPath($EvidenceDirectory)
if (Test-Path -LiteralPath $evidencePath) { throw 'Evidence directory must be new so an earlier result cannot be overwritten.' }
New-Item -ItemType Directory -Path $evidencePath | Out-Null
$project = 'decision-accept-' + [Guid]::NewGuid().ToString('N').Substring(0, 12)
$compose = @('compose', '-p', $project, '-f', (Join-Path $serviceRoot 'compose.mysql.test.yaml'), '-f', (Join-Path $serviceRoot 'compose.differential.test.yaml'))
$result = [ordered]@{ Project=$project; Started=[DateTime]::UtcNow.ToString('o'); Status='failed'; Cleanup='pending'; ApplicationDatabaseTouched=$false }
function Invoke-DockerLogged([string[]]$Arguments, [string]$LogPath) {
    # Windows PowerShell turns native stderr into ErrorRecord objects. Docker
    # writes normal progress there; use its exit status as the failure contract.
    $previous = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        & docker @Arguments 2>&1 | ForEach-Object { "$_" } | Out-File -Encoding utf8 $LogPath
        $code = $LASTEXITCODE
    } finally { $ErrorActionPreference = $previous }
    return $code
}
try {
    if ((Invoke-DockerLogged ($compose + @('up','-d','--wait','mysql','postgres')) (Join-Path $evidencePath 'startup.log')) -ne 0) { throw 'Disposable database startup failed.' }
    if ((Invoke-DockerLogged ($compose + @('exec','-T','-e','MYSQL_PWD=isolated-test-root-only','mysql','mysql','-uroot','-e','CREATE DATABASE IF NOT EXISTS data_model_publication_test')) (Join-Path $evidencePath 'owner-base.log')) -ne 0) { throw 'Disposable owner fixture initialization failed.' }
    if ((Invoke-DockerLogged ($compose + @('exec','-T','-e','MYSQL_PWD=isolated-test-root-only','mysql','mysql','-uroot','-e','SELECT VERSION()')) (Join-Path $evidencePath 'mysql-version.log')) -ne 0) { throw 'MySQL version inspection failed.' }
    if ((Invoke-DockerLogged ($compose + @('exec','-T','postgres','psql','-U','test_owner','-d','decision_test_admin','-c','SELECT version()')) (Join-Path $evidencePath 'postgres-version.log')) -ne 0) { throw 'PostgreSQL version inspection failed.' }
    # The compose overlay supplies both required-integration flags. No caller
    # database URL is forwarded; the fixture only creates generated databases.
    $command = @'
set -eu
test "$DECISION_MYSQL_REQUIRE_INTEGRATION" = 1
test "$DECISION_ENGINE_REQUIRE_INTEGRATION" = 1
test "$DECISION_MYSQL_TEST_ADDRESS" = mysql:3306
go version
go test -mod=readonly -race -p 2 -count=1 -timeout=600s -json ./internal/mysqlruntime ./internal/store/mysql -run 'Test(SharedOwnerModelComposition|BrowserRealOwnerEditParity|ActualDecisionServiceRevocationBeforePersistence|ScreeningOwnerMutationsAndProjectionDrain)$'
cd ../data-model-service
export DATA_MODEL_MYSQL_TEST_ADDRESS=mysql:3306
export DATA_MODEL_MYSQL_TEST_ROOT_PASSWORD=isolated-test-root-only
export DATA_MODEL_REQUIRE_MYSQL_TEST=1
go test -mod=readonly -race -p 2 -count=1 -timeout=300s -json ./internal/store/mysql/metadata -run 'TestPhysicalCreationAllocated/service_allocated_native_index_worker_decision$'
'@
    $command | Set-Content -Encoding utf8 (Join-Path $evidencePath 'command.sh')
    if ((Invoke-DockerLogged ($compose + @('run','--rm','--no-deps','test','sh','-ec',$command)) (Join-Path $evidencePath 'tests.jsonl')) -ne 0) { throw 'Shared owner acceptance tests failed.' }
    $events = @(Get-Content (Join-Path $evidencePath 'tests.jsonl') | Where-Object { $_.StartsWith('{') } | ForEach-Object { $_ | ConvertFrom-Json })
    $failed = @($events | Where-Object { $_.Action -in @('fail','skip') })
    $passed = @($events | Where-Object { $_.Action -eq 'pass' -and $_.Test -and $_.Test -notmatch '/' })
    $required=@('TestSharedOwnerModelComposition','TestBrowserRealOwnerEditParity','TestActualDecisionServiceRevocationBeforePersistence','TestScreeningOwnerMutationsAndProjectionDrain','TestPhysicalCreationAllocated')
    if ($failed.Count -ne 0 -or $passed.Count -ne $required.Count) { throw 'Required tests were absent, failed or skipped.' }
    foreach ($name in $required) { if (@($passed | Where-Object { $_.Test -eq $name }).Count -ne 1) { throw "Required test absent: $name" } }
    if (@($events | Where-Object { $_.Action -eq 'pass' -and $_.Test -eq 'TestPhysicalCreationAllocated/service_allocated_native_index_worker_decision' }).Count -ne 1) { throw 'Physical owner journey did not execute.' }
    $result.Status='passed'
    $result.PassedTopLevelTests=$passed.Count
    $result.Scope='Real owner migrations; physical publication/recovery/grants; model-change and aggregate evaluation; browser edits/copy/races; revocation; screening mutation/projection drain. Full external effect-provider/release acceptance remains separate.'
} finally {
    $cleanupCode=Invoke-DockerLogged ($compose + @('down','--volumes','--remove-orphans')) (Join-Path $evidencePath 'cleanup.log')
    $result.Cleanup=if ($cleanupCode -eq 0) {'passed'} else {'failed'}
    $result.Finished=[DateTime]::UtcNow.ToString('o')
    $result | ConvertTo-Json -Depth 5 | Set-Content -Encoding utf8 (Join-Path $evidencePath 'result.json')
    if ($cleanupCode -ne 0) { throw "Cleanup failed for disposable project $project" }
}
