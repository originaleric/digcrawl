<#
Regression coverage for the Windows Watchlist Scheduled Task manager. It
dot-sources the production script in test mode and replaces Task Scheduler
commands, so it runs without a registered task or DigCrawl binary.
#>

[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$greenEscape = "$([char]27)[32m"
$redEscape = "$([char]27)[31m"
$resetEscape = "$([char]27)[0m"

function Assert-True([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw "assertion failed: $Message" }
}

$scriptPath = Join-Path $PSScriptRoot 'digcrawl-watchlist-service.ps1'
$env:DIGCRAWL_WATCHLIST_SERVICE_TEST = '1'
try {
    . $scriptPath
} finally {
    Remove-Item Env:DIGCRAWL_WATCHLIST_SERVICE_TEST -ErrorAction SilentlyContinue
}

$testRoot = Join-Path ([IO.Path]::GetTempPath()) "digcrawl-watchlist-service-test-$PID"
try {
    New-Item -ItemType Directory -Force -Path $testRoot | Out-Null
    $script:DataRoot = Join-Path $testRoot 'state'
    $script:ServiceConfigPath = Join-Path $script:DataRoot 'watchlist-service.json'
    $script:TaskName = 'DigCrawl Watchlist Test'
    $script:WorkDir = $testRoot
    $script:ConfigPath = Join-Path $testRoot 'config.yaml'
    $script:BinPath = Join-Path $testRoot 'digcrawl.exe'
    $script:LogDir = Join-Path $testRoot 'logs'
    New-Item -ItemType File -Force -Path $script:ConfigPath, $script:BinPath | Out-Null

    $script:Lifecycle = @()
    $script:TaskExists = $false
    $script:TaskState = 'Ready'

    function Get-Command {
        [CmdletBinding()]
        param([string]$Name)
        if ($Name -eq 'powershell.exe') { return [pscustomobject]@{ Source = 'powershell.exe' } }
        return $null
    }
    function New-ScheduledTaskAction { param([string]$Execute, [string]$Argument); return [pscustomobject]@{} }
    function New-ScheduledTaskTrigger { param([switch]$AtLogOn, [string]$User); return [pscustomobject]@{} }
    function New-ScheduledTaskSettingsSet {
        param([switch]$AllowStartIfOnBatteries, [switch]$DontStopIfGoingOnBatteries, [int]$RestartCount, [TimeSpan]$RestartInterval, [TimeSpan]$ExecutionTimeLimit)
        return [pscustomobject]@{}
    }
    function Register-ScheduledTask {
        param([string]$TaskName, $Action, $Trigger, $Settings, [string]$Description, [switch]$Force)
        $script:Lifecycle += 'register'; $script:TaskExists = $true; return [pscustomobject]@{}
    }
    function Start-ScheduledTask { param([string]$TaskName); $script:Lifecycle += 'start'; $script:TaskState = 'Running' }
    function Stop-ScheduledTask { param([string]$TaskName); $script:Lifecycle += 'stop'; $script:TaskState = 'Ready' }
    function Get-ScheduledTask {
        param([string]$TaskName)
        if (-not $script:TaskExists) { return $null }
        return [pscustomobject]@{ State = $script:TaskState }
    }
    function Start-Sleep { param([int]$Seconds) }

    Install-Service
    Assert-True (($script:Lifecycle -join ',') -eq 'register,start') 'install must register then start the task'
    Assert-True (Test-ServiceRunning) 'install must leave the task running'
    $runningStatus = Write-ServiceStatus | Out-String
    Assert-True ($runningStatus.Contains("status=${greenEscape}running${resetEscape}")) 'running status must be green'
    Assert-True (-not $runningStatus.Contains('True')) 'status must not append a Boolean result'

    $script:Lifecycle = @()
    Install-Service
    Assert-True (($script:Lifecycle -join ',') -eq 'stop,register,start') 'reinstall must stop the old task before replacing it'
    Assert-True (Test-ServiceRunning) 'reinstall must leave the replacement task running'

    $script:TaskState = 'Ready'
    $failedStatus = Write-ServiceStatus | Out-String
    Assert-True ($failedStatus.Contains("status=${redEscape}ready${resetEscape}")) 'non-running status must be red'
} finally {
    Remove-Item -LiteralPath $testRoot -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Output 'Windows Watchlist service regression checks passed'
