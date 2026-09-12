<#
Executable Windows regression coverage for the CDP browser Task Scheduler
manager. It runs without Chrome or a registered Task by dot-sourcing the
production script in test mode and replacing its OS integrations.
#>

[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

function Assert-True([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw "assertion failed: $Message" }
}

$scriptPath = Join-Path $PSScriptRoot 'digcrawl-cdp-browser-service.ps1'
[scriptblock]::Create((Get-Content -LiteralPath $scriptPath -Raw)) | Out-Null
$env:DIGCRAWL_CDP_BROWSER_SERVICE_TEST = '1'
try {
    . $scriptPath
} finally {
    Remove-Item Env:DIGCRAWL_CDP_BROWSER_SERVICE_TEST -ErrorAction SilentlyContinue
}

$Port = 9323
$UserDataDir = 'C:\Users\Dig Crawl\AppData\Local\DigCrawl\cdp-browser'

function Get-CimInstance {
    param([string]$ClassName, [string]$Filter)
    return $script:FakeProcesses
}

function Set-FakeProcesses([string[]]$CommandLines) {
    $script:FakeProcesses = @($CommandLines | ForEach-Object {
        [pscustomobject]@{ Name = 'chrome.exe'; ProcessId = 42; CommandLine = $_ }
    })
}

foreach ($main in @(
    'chrome.exe --remote-debugging-port=9323 "--user-data-dir=C:\Users\Dig Crawl\AppData\Local\DigCrawl\cdp-browser" --no-first-run',
    'chrome.exe --remote-debugging-port=9323 --user-data-dir="C:\Users\Dig Crawl\AppData\Local\DigCrawl\cdp-browser" --no-first-run'
)) {
    Set-FakeProcesses @($main)
    Assert-True (@(Get-ManagedChromeProcesses).Count -eq 1) "exact main process should match: $main"
}

function Test-CdpReady { return $true }

$unmutedMain = 'chrome.exe --remote-debugging-port=9323 --user-data-dir="C:\Users\Dig Crawl\AppData\Local\DigCrawl\cdp-browser" --no-first-run'
Set-FakeProcesses @($unmutedMain)
Assert-True (@(Get-MutedManagedChromeProcesses).Count -eq 0) 'a managed browser without --mute-audio must not satisfy the service contract'
Assert-True (-not (Test-ServiceCdpReady)) 'an unmuted managed browser must fail service readiness'

$mutedMain = "$unmutedMain --mute-audio"
Set-FakeProcesses @($mutedMain)
Assert-True (@(Get-MutedManagedChromeProcesses).Count -eq 1) 'a muted managed browser should satisfy the service contract'
Assert-True (Test-ServiceCdpReady) 'a muted managed browser should pass service readiness'
Assert-True ((Get-ChromeLaunchArguments) -contains '--mute-audio') 'managed Chrome launch arguments must include --mute-audio'

foreach ($foreign in @(
    'chrome.exe --remote-debugging-port=93230 --user-data-dir="C:\Users\Dig Crawl\AppData\Local\DigCrawl\cdp-browser" --no-first-run',
    'chrome.exe --remote-debugging-port=9323 --user-data-dir="C:\Users\Dig Crawl\AppData\Local\DigCrawl\cdp-browser-other" --no-first-run',
    'chrome.exe --remote-debugging-port=9323 --user-data-dir="C:\Users\Dig Crawl\AppData\Local\DigCrawl\cdp-browser" --no-first-run --type=renderer'
)) {
    Set-FakeProcesses @($foreign)
    Assert-True (@(Get-ManagedChromeProcesses).Count -eq 0) "foreign process must not match: $foreign"
}

$script:StopCalled = $false
function Stop-Service { $script:StopCalled = $true }
function Wait-CdpStopped { return $script:StopCalled }
Prepare-Install
Assert-True $script:StopCalled 'reinstall must call Stop-Service, not only Stop-ScheduledTask'

$script:Lifecycle = @()
function Stop-Service { $script:Lifecycle += 'stop' }
function Wait-CdpStopped { $script:Lifecycle += 'released'; return $true }
function Start-Service { $script:Lifecycle += 'start' }
Restart-ManagedService
Assert-True (($script:Lifecycle -join ',') -eq 'stop,released,start') 'restart must wait for the prior CDP browser to release before starting a replacement'

# Installation is only provisioning. It must register a task that can be
# started explicitly, without an AtLogOn trigger, restart policy, or implicit
# Start-ScheduledTask call.
$manualTaskRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("digcrawl-cdp-manual-task-$([guid]::NewGuid().ToString('N'))")
try {
    $script:ServiceName = 'DigCrawl CDP Browser Test'
    $script:Port = 19323
    $script:UserDataDir = Join-Path $manualTaskRoot 'profile'
    $script:LogDir = Join-Path $manualTaskRoot 'logs'
    $script:ServiceStateDir = Join-Path $manualTaskRoot 'state'
    $script:ActiveServicePath = Join-Path $script:ServiceStateDir 'active-service.txt'
    $script:ServiceConfigPath = Join-Path $script:ServiceStateDir 'DigCrawl CDP Browser Test.json'
    $script:TaskSettings = $null
    $script:RegisteredTaskParameters = $null
    $script:StartCalled = $false

    function Prepare-Install {}
    function Get-ChromePath { return $scriptPath }
    function New-ScheduledTaskAction { param($Execute, $Argument) return [pscustomobject]@{ Execute = $Execute; Argument = $Argument } }
    function New-ScheduledTaskSettingsSet {
        param([switch]$AllowStartIfOnBatteries, [switch]$DontStopIfGoingOnBatteries, [int]$RestartCount, $ExecutionTimeLimit)
        $script:TaskSettings = @{ RestartCount = $RestartCount; ExecutionTimeLimit = $ExecutionTimeLimit }
        return [pscustomobject]@{}
    }
    function Register-ScheduledTask {
        param($TaskName, $Action, $Settings, $Description, [switch]$Force)
        $script:RegisteredTaskParameters = $PSBoundParameters
        return [pscustomobject]@{}
    }
    function Start-ScheduledTask { $script:StartCalled = $true }

    Install-Service
    Assert-True ($null -ne $script:RegisteredTaskParameters) 'install must register the manually startable task'
    Assert-True (-not $script:RegisteredTaskParameters.ContainsKey('Trigger')) 'install must not create a logon trigger'
    Assert-True ($script:TaskSettings.RestartCount -eq 0) 'install must disable Task Scheduler restart attempts'
    Assert-True (-not $script:StartCalled) 'install must not start Chrome implicitly'
} finally {
    Remove-Item -LiteralPath $manualTaskRoot -Recurse -Force -ErrorAction SilentlyContinue
}

# Reconfiguration is a two-phase lifecycle: first retire the saved browser
# definition, including a Chrome process whose CDP port has already closed;
# only then apply the caller's new runtime values. Reload the production
# functions because the assertions above intentionally replace integrations.
$env:DIGCRAWL_CDP_BROWSER_SERVICE_TEST = '1'
try {
    . $scriptPath
} finally {
    Remove-Item Env:DIGCRAWL_CDP_BROWSER_SERVICE_TEST -ErrorAction SilentlyContinue
}

$reconfigurationRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("digcrawl-cdp-reconfigure-$([guid]::NewGuid().ToString('N'))")
try {
    $oldPort = 19433
    $newPort = 19434
    $oldUserDataDir = Join-Path $reconfigurationRoot 'profile'
    $oldLogDir = Join-Path $reconfigurationRoot 'logs'
    $script:ServiceNameExplicit = $false
    $script:PortExplicit = $true
    $script:UserDataDirExplicit = $false
    $script:LogDirExplicit = $false
    $script:ChromePathExplicit = $false
    $script:ExplicitPort = $newPort
    $script:ServiceName = 'DigCrawl CDP Browser'
    $script:Port = $newPort
    $script:UserDataDir = $oldUserDataDir
    $script:LogDir = $oldLogDir
    $script:ServiceStateDir = Join-Path $reconfigurationRoot 'state'
    $script:ActiveServicePath = Join-Path $script:ServiceStateDir 'active-service.txt'
    $script:LegacyServiceConfigPath = Join-Path $reconfigurationRoot 'legacy.json'
    New-Item -ItemType Directory -Force -Path $script:ServiceStateDir | Out-Null
    Set-Content -LiteralPath $script:ActiveServicePath -Value $script:ServiceName -Encoding UTF8 -NoNewline
    $script:ServiceConfigPath = Get-ServiceConfigPath
    [ordered]@{
        version = 2
        service_name = $script:ServiceName
        port = $oldPort
        user_data_dir = $oldUserDataDir
        log_dir = $oldLogDir
        chrome_path = $scriptPath
    } | ConvertTo-Json | Set-Content -LiteralPath $script:ServiceConfigPath -Encoding UTF8 -NoNewline

    Resolve-ManagedService
    Assert-True ($script:Port -eq $oldPort) 'install must resolve the saved old port before stopping the service'
    Validate-ExplicitRuntimeOverrides
    Assert-True ($script:Port -eq $oldPort) 'validating an explicit replacement port must not overwrite the old stop configuration'

    $oldMain = "chrome.exe --remote-debugging-port=$oldPort --user-data-dir=`"$oldUserDataDir`" --no-first-run --mute-audio"
    Set-FakeProcesses @($oldMain)
    $script:FakeCdpReady = $false
    $script:StoppedPorts = @()
    $script:ReleaseWaitCount = 0
    $script:SavedPort = $null
    function Test-CdpReady { return $script:FakeCdpReady }
    function Stop-Service { $script:StoppedPorts += $script:Port }
    function Start-Sleep {
        param([int]$Seconds)
        $script:ReleaseWaitCount++
        Set-FakeProcesses @()
    }
    function Ensure-Directories {}
    function Get-ChromePath { return $scriptPath }
    function Save-ServiceConfig([string]$ChromePath) { $script:SavedPort = $script:Port }
    function Set-ActiveService {}
    function New-ScheduledTaskAction { param($Execute, $Argument) return [pscustomobject]@{} }
    function New-ScheduledTaskSettingsSet {
        param([switch]$AllowStartIfOnBatteries, [switch]$DontStopIfGoingOnBatteries, [int]$RestartCount, $ExecutionTimeLimit)
        return [pscustomobject]@{}
    }
    function Register-ScheduledTask { param($TaskName, $Action, $Settings, $Description, [switch]$Force) return [pscustomobject]@{} }

    Install-Service
    Assert-True (($script:StoppedPorts -join ',') -eq "$oldPort") 'reconfiguration must stop Chrome using the saved old port'
    Assert-True ($script:ReleaseWaitCount -gt 0) 'a closed CDP endpoint is insufficient while the old Chrome main process remains'
    Assert-True ($script:SavedPort -eq $newPort) 'the replacement task must persist the explicit new port after old Chrome exits'
} finally {
    Remove-Item -LiteralPath $reconfigurationRoot -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Output 'Windows CDP browser service regression checks passed'
