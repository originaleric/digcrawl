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

Write-Output 'Windows CDP browser service regression checks passed'
