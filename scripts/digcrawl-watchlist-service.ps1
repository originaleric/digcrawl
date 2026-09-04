<#
Manage DigCrawl Watchlist as a current-user Windows Scheduled Task.

Usage:
  .\scripts\digcrawl-watchlist-service.ps1 install
  .\scripts\digcrawl-watchlist-service.ps1 start|stop|restart|status|logs|uninstall

`install` succeeds only after the task is registered, started, and observed in
the Running state. It persists the selected binary, config, work directory,
and log directory under LOCALAPPDATA so later lifecycle commands use the same
service definition.
#>

[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('install', 'start', 'stop', 'restart', 'status', 'logs', 'uninstall', 'run-foreground')]
    [string]$Command = 'status'
)

$ErrorActionPreference = 'Stop'
$ScriptPath = $PSCommandPath
$DataRoot = if ($env:LOCALAPPDATA) { Join-Path $env:LOCALAPPDATA 'DigCrawl' } else { Join-Path $HOME 'AppData\Local\DigCrawl' }
$TaskName = if ($env:DIGCRAWL_WATCHLIST_SERVICE_NAME) { $env:DIGCRAWL_WATCHLIST_SERVICE_NAME } else { 'DigCrawl Watchlist' }
$WorkDir = if ($env:DIGCRAWL_WORKDIR) { $env:DIGCRAWL_WORKDIR } else { Split-Path -Parent $PSScriptRoot }
$ConfigPath = if ($env:DIGCRAWL_CONFIG) { $env:DIGCRAWL_CONFIG } else { Join-Path $WorkDir 'config\config.yaml' }
$LogDir = if ($env:DIGCRAWL_LOG_DIR) { $env:DIGCRAWL_LOG_DIR } else { Join-Path $DataRoot 'logs\watchlist' }
$BinPath = $env:DIGCRAWL_BIN
$ServiceConfigPath = Join-Path $DataRoot 'watchlist-service.json'
$Green = "$([char]27)[32m"
$Red = "$([char]27)[31m"
$Reset = "$([char]27)[0m"

function Fail([string]$Message) { throw "error: $Message" }

function Validate-ServiceName {
    if ([string]::IsNullOrWhiteSpace($TaskName) -or $TaskName -notmatch '^[A-Za-z0-9][A-Za-z0-9 ._-]*$') {
        Fail 'DIGCRAWL_WATCHLIST_SERVICE_NAME may contain only letters, numbers, spaces, dot, underscore, and hyphen'
    }
}

function Resolve-Binary {
    if (-not [string]::IsNullOrWhiteSpace($script:BinPath)) { return }
    $projectBinary = Join-Path $WorkDir 'digcrawl.exe'
    if (Test-Path -LiteralPath $projectBinary -PathType Leaf) {
        $script:BinPath = $projectBinary
        return
    }
    $installed = Get-Command digcrawl.exe -ErrorAction SilentlyContinue
    if ($installed) {
        $script:BinPath = $installed.Source
        return
    }
    Fail 'digcrawl.exe not found; set DIGCRAWL_BIN to the release binary path'
}

function Validate-Runtime {
    Validate-ServiceName
    Resolve-Binary
    if (-not (Test-Path -LiteralPath $BinPath -PathType Leaf)) { Fail "digcrawl binary not found: $BinPath" }
    if (-not (Test-Path -LiteralPath $ConfigPath -PathType Leaf)) { Fail "config file not found: $ConfigPath" }
}

function Save-ServiceConfig {
    New-Item -ItemType Directory -Force -Path $DataRoot, $LogDir | Out-Null
    $saved = [ordered]@{
        version = 1
        task_name = $TaskName
        binary_path = [IO.Path]::GetFullPath($BinPath)
        config_path = [IO.Path]::GetFullPath($ConfigPath)
        work_dir = [IO.Path]::GetFullPath($WorkDir)
        log_dir = [IO.Path]::GetFullPath($LogDir)
    }
    $temporaryPath = "$ServiceConfigPath.$PID.tmp"
    try {
        $saved | ConvertTo-Json | Set-Content -LiteralPath $temporaryPath -Encoding UTF8 -NoNewline
        Move-Item -LiteralPath $temporaryPath -Destination $ServiceConfigPath -Force
    } finally {
        Remove-Item -LiteralPath $temporaryPath -Force -ErrorAction SilentlyContinue
    }
}

function Load-ServiceConfig {
    if (-not (Test-Path -LiteralPath $ServiceConfigPath -PathType Leaf)) { return }
    try {
        $saved = Get-Content -LiteralPath $ServiceConfigPath -Raw | ConvertFrom-Json -ErrorAction Stop
        if (-not [string]::IsNullOrWhiteSpace([string]$saved.task_name)) { $script:TaskName = [string]$saved.task_name }
        if (-not [string]::IsNullOrWhiteSpace([string]$saved.binary_path)) { $script:BinPath = [string]$saved.binary_path }
        if (-not [string]::IsNullOrWhiteSpace([string]$saved.config_path)) { $script:ConfigPath = [string]$saved.config_path }
        if (-not [string]::IsNullOrWhiteSpace([string]$saved.work_dir)) { $script:WorkDir = [string]$saved.work_dir }
        if (-not [string]::IsNullOrWhiteSpace([string]$saved.log_dir)) { $script:LogDir = [string]$saved.log_dir }
    } catch {
        Fail "cannot read Watchlist service configuration at $ServiceConfigPath; reinstall the service: $($_.Exception.Message)"
    }
}

function Get-TaskState {
    $task = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    if (-not $task) { return 'not_loaded' }
    return [string]$task.State
}

function Test-ServiceRunning { return (Get-TaskState) -eq 'Running' }

function Write-ServiceStatus {
    $state = Get-TaskState
    if ($state -eq 'Running') {
        Write-Output "status=${Green}running${Reset}"
        return
    }
    Write-Output "status=${Red}$($state.ToLowerInvariant())${Reset}"
}

function Wait-ServiceRunning {
    for ($attempt = 1; $attempt -le 15; $attempt++) {
        if (Test-ServiceRunning) { return $true }
        Start-Sleep -Seconds 1
    }
    return $false
}

function Stop-ExistingServiceTask {
    if (-not (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue)) { return }
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    for ($attempt = 1; $attempt -le 15; $attempt++) {
        if (-not (Test-ServiceRunning)) { return }
        Start-Sleep -Seconds 1
    }
    Fail "existing $TaskName task did not stop before reinstall"
}

function Register-ServiceTask {
    $powershell = (Get-Command powershell.exe -ErrorAction Stop).Source
    $arguments = "-NoProfile -ExecutionPolicy Bypass -File `"$ScriptPath`" run-foreground"
    $action = New-ScheduledTaskAction -Execute $powershell -Argument $arguments
    $taskUser = if ($env:USERDOMAIN) { "$env:USERDOMAIN\$env:USERNAME" } else { $env:USERNAME }
    $trigger = New-ScheduledTaskTrigger -AtLogOn -User $taskUser
    $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit (New-TimeSpan -Seconds 0)
    Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger -Settings $settings -Description 'DigCrawl Watchlist Scheduler' -Force | Out-Null
}

function Install-Service {
    Validate-Runtime
    Stop-ExistingServiceTask
    Save-ServiceConfig
    Register-ServiceTask
    Start-ScheduledTask -TaskName $TaskName
    if (-not (Wait-ServiceRunning)) {
        Write-ServiceStatus | Out-Host
        Fail "install did not leave $TaskName running; inspect logs with: .\scripts\digcrawl-watchlist-service.ps1 logs"
    }
    Write-ServiceStatus | Out-Host
    Write-Output "installed and running Scheduled Task: $TaskName"
}

function Start-Service {
    if (-not (Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue)) { Fail 'service is not installed; run install first' }
    Start-ScheduledTask -TaskName $TaskName
    if (-not (Wait-ServiceRunning)) {
        Write-ServiceStatus | Out-Host
        Fail "start did not leave $TaskName running; inspect logs"
    }
    Write-ServiceStatus
}

function Stop-Service {
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
}

function Restart-ServiceTask {
    Stop-Service
    Start-Service
}

function Show-Logs {
    New-Item -ItemType Directory -Force -Path $LogDir | Out-Null
    $stdout = Join-Path $LogDir 'watchlist.stdout.log'
    $stderr = Join-Path $LogDir 'watchlist.stderr.log'
    New-Item -ItemType File -Force -Path $stdout, $stderr | Out-Null
    Get-Content -Path $stdout, $stderr -Tail 100
}

function Run-Foreground {
    Load-ServiceConfig
    Validate-Runtime
    New-Item -ItemType Directory -Force -Path $LogDir | Out-Null
    $stdout = Join-Path $LogDir 'watchlist.stdout.log'
    $stderr = Join-Path $LogDir 'watchlist.stderr.log'
    Push-Location $WorkDir
    try {
        & $BinPath watchlist serve --config $ConfigPath 1>> $stdout 2>> $stderr
        exit $LASTEXITCODE
    } finally {
        Pop-Location
    }
}

if ($env:DIGCRAWL_WATCHLIST_SERVICE_TEST -eq '1') { return }

if ($Command -ne 'install' -and $Command -ne 'run-foreground') { Load-ServiceConfig }

switch ($Command) {
    'install' { Install-Service }
    'start' { Start-Service }
    'stop' { Stop-Service }
    'restart' { Restart-ServiceTask }
    'status' {
        Write-ServiceStatus
        if (-not (Test-ServiceRunning)) { exit 1 }
    }
    'logs' { Show-Logs }
    'uninstall' { Stop-Service; Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false -ErrorAction SilentlyContinue; Remove-Item -LiteralPath $ServiceConfigPath -Force -ErrorAction SilentlyContinue }
    'run-foreground' { Run-Foreground }
}
