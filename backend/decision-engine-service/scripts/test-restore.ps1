param([Parameter(Mandatory = $true)][string]$EvidenceDirectory, [switch]$ReleaseChecks)
$ErrorActionPreference = 'Stop'
$serviceRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$evidencePath = [IO.Path]::GetFullPath($EvidenceDirectory)
if (Test-Path -LiteralPath $evidencePath) { throw 'Evidence directory must be new.' }
if (Test-Path -LiteralPath (Join-Path $serviceRoot '.restore-rehearsal')) { throw 'A restore exchange already exists; inspect it before starting another rehearsal.' }
New-Item -ItemType Directory -Path $evidencePath | Out-Null
$project = 'decision-restore-' + [Guid]::NewGuid().ToString('N').Substring(0,12)
$compose = @('compose','-p',$project,'-f',(Join-Path $serviceRoot 'compose.mysql.test.yaml'),'-f',(Join-Path $serviceRoot 'compose.restore.test.yaml'))
$result = [ordered]@{Project=$project; Started=[DateTime]::UtcNow.ToString('o'); Status='failed'; Cleanup='pending'; ApplicationDatabaseTouched=$false}
function Invoke-DockerLogged([string[]]$Arguments,[string]$LogPath) {
    $previous = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        & docker @Arguments 2>&1 | ForEach-Object { "$_" } | Out-File -Encoding utf8 $LogPath
        $code = $LASTEXITCODE
    } finally { $ErrorActionPreference = $previous }
    return $code
}
try {
    if ((Invoke-DockerLogged ($compose+@('up','-d','--wait','mysql','mysqlrestore')) (Join-Path $evidencePath 'startup.log')) -ne 0) { throw 'Private database startup failed.' }
    # Compile before starting the bounded-wait dump companion.
    if ((Invoke-DockerLogged ($compose+@('run','--name',"$project-runner",'-d','test','sleep','infinity')) (Join-Path $evidencePath 'runner.log')) -ne 0) { throw 'Test runner startup failed.' }
    if ((Invoke-DockerLogged @('exec',"$project-runner",'go','test','-race','-p','2','-run','^$','./internal/store/mysql') (Join-Path $evidencePath 'compile.log')) -ne 0) { throw 'Test compilation failed.' }
    if ($ReleaseChecks) {
        if ((Invoke-DockerLogged @('exec',"$project-runner",'sh','scripts/test-release-readiness.sh') (Join-Path $evidencePath 'release-tests.jsonl')) -ne 0) { throw 'Release migration/permission checks failed.' }
        $releaseEvents = @(Get-Content (Join-Path $evidencePath 'release-tests.jsonl') | Where-Object { $_.StartsWith('{') } | ForEach-Object { $_ | ConvertFrom-Json })
        if (@($releaseEvents | Where-Object { $_.Action -in @('fail','skip') }).Count -ne 0) { throw 'Release checks failed or skipped.' }
        foreach ($required in @('TestReleaseRehearsalFreshUpgradeInterruptedDDLDriftAndForeignOwnership','TestRestoreRelationshipInspectionRejectsOrphansAndTenantMismatch','TestMySQLReleaseRuntimeGrantBoundaryAndWorkerDispatch')) {
            if (@($releaseEvents | Where-Object { $_.Action -eq 'pass' -and $_.Test -eq $required }).Count -ne 1) { throw "Required release check absent: $required" }
        }
        $result.ReleaseChecks='passed'
    }
    if ((Invoke-DockerLogged ($compose+@('up','-d','restore-agent')) (Join-Path $evidencePath 'agent-startup.log')) -ne 0) { throw 'Restore companion startup failed.' }
    if ((Invoke-DockerLogged @('exec','-e','DECISION_RESTORE_REHEARSAL=1',"$project-runner",'go','test','-mod=readonly','-race','-p','2','-count=1','-timeout=300s','-json','./internal/store/mysql','-run','^TestMySQLLogicalRestoreRetainsDecisionsJobsAndCaseReceipts$') (Join-Path $evidencePath 'tests.jsonl')) -ne 0) { throw 'Logical restore acceptance failed.' }
    $events = @(Get-Content (Join-Path $evidencePath 'tests.jsonl') | Where-Object { $_.StartsWith('{') } | ForEach-Object { $_ | ConvertFrom-Json })
    if (@($events | Where-Object { $_.Action -in @('fail','skip') }).Count -ne 0 -or @($events | Where-Object { $_.Action -eq 'pass' -and $_.Test -eq 'TestMySQLLogicalRestoreRetainsDecisionsJobsAndCaseReceipts' }).Count -ne 1) { throw 'Required restore test was skipped, absent or failed.' }
    $result.Status='passed'
    $result.Scope='MySQL 8.4.8 logical dump onto a distinct private server; exact schema inventory and full table checksums; retained receipt reconciliation/replay with restricted case grants. No production PITR/failover or full service activation claim.'
} finally {
    $null = Invoke-DockerLogged ($compose+@('logs','restore-agent')) (Join-Path $evidencePath 'agent.log')
    $null = Invoke-DockerLogged @('rm','-f',"$project-runner") (Join-Path $evidencePath 'runner-cleanup.log')
    $cleanup = Invoke-DockerLogged ($compose+@('down','--volumes')) (Join-Path $evidencePath 'cleanup.log')
    $result.Cleanup = if ($cleanup -eq 0) {'passed'} else {'failed'}
    $result.Finished=[DateTime]::UtcNow.ToString('o')
    $result | ConvertTo-Json -Depth 5 | Set-Content -Encoding utf8 (Join-Path $evidencePath 'result.json')
    if ($cleanup -ne 0) { throw "Cleanup failed for $project" }
}
