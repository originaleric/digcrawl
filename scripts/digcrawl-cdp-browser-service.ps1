<#
Manage the dedicated Chrome instance used by DigCrawl through CDP on Windows.

Usage:
  .\scripts\digcrawl-cdp-browser-service.ps1 install
  .\scripts\digcrawl-cdp-browser-service.ps1 start|stop|restart|status|logs|uninstall

The Scheduled Task runs at the current user's logon and restarts its managed
Chrome process if it exits. It discovers system Chrome/Chromium and never
uses Playwright's versioned browser cache.
#>

[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('install', 'start', 'stop', 'restart', 'status', 'logs', 'uninstall', 'run-foreground')]
    [string]$Command = 'status'
)

$ErrorActionPreference = 'Stop'

$ServiceNameExplicit = -not [string]::IsNullOrWhiteSpace($env:DIGCRAWL_CDP_BROWSER_SERVICE_NAME)
$PortExplicit = -not [string]::IsNullOrWhiteSpace($env:DIGCRAWL_CDP_PORT)
$UserDataDirExplicit = -not [string]::IsNullOrWhiteSpace($env:DIGCRAWL_CDP_USER_DATA_DIR)
$LogDirExplicit = -not [string]::IsNullOrWhiteSpace($env:DIGCRAWL_CDP_LOG_DIR)
$ChromePathExplicit = -not [string]::IsNullOrWhiteSpace($env:DIGCRAWL_CHROME_PATH)
$ServiceName = if ($ServiceNameExplicit) { $env:DIGCRAWL_CDP_BROWSER_SERVICE_NAME } else { 'DigCrawl CDP Browser' }
$Port = if ($PortExplicit) { [int]$env:DIGCRAWL_CDP_PORT } else { 9323 }
$DataRoot = if ($env:LOCALAPPDATA) { Join-Path $env:LOCALAPPDATA 'DigCrawl' } else { Join-Path $HOME 'AppData\Local\DigCrawl' }
$UserDataDir = if ($UserDataDirExplicit) { $env:DIGCRAWL_CDP_USER_DATA_DIR } else { Join-Path $DataRoot 'cdp-browser' }
$LogDir = if ($LogDirExplicit) { $env:DIGCRAWL_CDP_LOG_DIR } else { Join-Path $DataRoot 'logs\cdp-browser' }
$ServiceStateDir = Join-Path $DataRoot 'cdp-browser-services'
$ActiveServicePath = Join-Path $ServiceStateDir 'active-service.txt'
$LegacyServiceConfigPath = Join-Path $DataRoot 'cdp-browser-service.json'
$ServiceConfigPath = $null
$ScriptPath = $PSCommandPath

function Fail([string]$Message) {
    throw "error: $Message"
}

function Validate-Config {
    if ($Port -lt 1 -or $Port -gt 65535) {
        Fail 'DIGCRAWL_CDP_PORT must be a valid TCP port'
    }
}

function Validate-ServiceName {
    # Task names also become filenames in the private state directory. Keeping
    # the identity simple avoids task-folder ambiguity and path traversal.
    if ([string]::IsNullOrWhiteSpace($ServiceName) -or $ServiceName -notmatch '^[A-Za-z0-9][A-Za-z0-9 ._-]*$') {
        Fail 'DIGCRAWL_CDP_BROWSER_SERVICE_NAME may contain only letters, numbers, spaces, dot, underscore, and hyphen'
    }
}

function Get-ServiceConfigPath([string]$Name = $ServiceName) {
    Join-Path $ServiceStateDir "$Name.json"
}

function Set-ActiveService {
    New-Item -ItemType Directory -Force -Path $ServiceStateDir | Out-Null
    $temporaryPath = "$ActiveServicePath.$PID.tmp"
    try {
        Set-Content -LiteralPath $temporaryPath -Value $ServiceName -Encoding UTF8 -NoNewline
        Move-Item -LiteralPath $temporaryPath -Destination $ActiveServicePath -Force
    } finally {
        Remove-Item -LiteralPath $temporaryPath -Force -ErrorAction SilentlyContinue
    }
}

function Remove-ActiveServiceIfCurrent {
    if (-not (Test-Path -LiteralPath $ActiveServicePath -PathType Leaf)) { return }
    $activeName = (Get-Content -LiteralPath $ActiveServicePath -Raw).Trim()
    if ($activeName -eq $ServiceName) {
        Remove-Item -LiteralPath $ActiveServicePath -Force -ErrorAction SilentlyContinue
    }
}

function Get-ChromePath {
    $candidates = @()
    if ($env:DIGCRAWL_CHROME_PATH) { $candidates += $env:DIGCRAWL_CHROME_PATH }
    foreach ($registryPath in @(
        'HKCU:\Software\Microsoft\Windows\CurrentVersion\App Paths\chrome.exe',
        'HKLM:\Software\Microsoft\Windows\CurrentVersion\App Paths\chrome.exe',
        'HKCU:\Software\Microsoft\Windows\CurrentVersion\App Paths\msedge.exe',
        'HKLM:\Software\Microsoft\Windows\CurrentVersion\App Paths\msedge.exe'
    )) {
        if (Test-Path $registryPath) {
            $candidates += (Get-ItemProperty -Path $registryPath -ErrorAction SilentlyContinue).'(default)'
        }
    }
    foreach ($base in @($env:ProgramFiles, ${env:ProgramFiles(x86)}, $env:LOCALAPPDATA)) {
        if ($base) {
            $candidates += (Join-Path $base 'Google\Chrome\Application\chrome.exe')
            $candidates += (Join-Path $base 'Chromium\Application\chrome.exe')
            $candidates += (Join-Path $base 'Microsoft\Edge\Application\msedge.exe')
        }
    }
    foreach ($candidate in $candidates | Where-Object { $_ } | Select-Object -Unique) {
        if (Test-Path -LiteralPath $candidate -PathType Leaf) { return $candidate }
    }
    Fail 'no supported system Chrome/Chromium/Edge found; install one or set DIGCRAWL_CHROME_PATH'
}

function Get-CdpUrl { "http://127.0.0.1:$Port/json/version" }

function Test-CdpReady {
    try {
        $response = Invoke-WebRequest -Uri (Get-CdpUrl) -UseBasicParsing -TimeoutSec 2
        $version = $response.Content | ConvertFrom-Json -ErrorAction Stop
        return -not [string]::IsNullOrWhiteSpace([string]$version.webSocketDebuggerUrl)
    } catch {
        return $false
    }
}

function Test-ServiceCdpReady {
    if (-not (Test-CdpReady)) { return $false }
    return @((Get-MutedManagedChromeProcesses)).Count -gt 0
}

function Wait-CdpStopped {
    for ($attempt = 1; $attempt -le 10; $attempt++) {
        if (-not (Test-CdpReady)) { return $true }
        Start-Sleep -Seconds 1
    }
    return $false
}

function Prepare-Install {
    # Reinstall must reclaim the selected service's Chrome child as well as its
    # Task Scheduler wrapper. Stop-Service is safe here because its process
    # predicate requires the full configured port/profile argument pair.
    Stop-Service
    if (-not (Wait-CdpStopped)) {
        Fail "CDP port $Port is already in use; stop the conflicting browser or choose a different DIGCRAWL_CDP_PORT"
    }
}

function Ensure-Directories {
    New-Item -ItemType Directory -Force -Path $UserDataDir, $LogDir | Out-Null
}

function Import-ServiceConfig([string]$Path = $ServiceConfigPath, [bool]$PreserveExplicitRuntime = $false) {
    if (-not $Path -or -not (Test-Path -LiteralPath $Path -PathType Leaf)) { return $false }
    try {
        $saved = Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json -ErrorAction Stop
        if ($ServiceNameExplicit -and -not [string]::IsNullOrWhiteSpace([string]$saved.service_name) -and [string]$saved.service_name -ne $ServiceName) {
            Fail "service configuration at $Path names a different service"
        }
        if (-not $ServiceNameExplicit -and -not [string]::IsNullOrWhiteSpace([string]$saved.service_name)) { $script:ServiceName = [string]$saved.service_name }
        if ((-not $PreserveExplicitRuntime -or -not $PortExplicit) -and $null -ne $saved.port) { $script:Port = [int]$saved.port }
        if ((-not $PreserveExplicitRuntime -or -not $UserDataDirExplicit) -and -not [string]::IsNullOrWhiteSpace([string]$saved.user_data_dir)) { $script:UserDataDir = [string]$saved.user_data_dir }
        if ((-not $PreserveExplicitRuntime -or -not $LogDirExplicit) -and -not [string]::IsNullOrWhiteSpace([string]$saved.log_dir)) { $script:LogDir = [string]$saved.log_dir }
        if ((-not $PreserveExplicitRuntime -or -not $ChromePathExplicit) -and -not [string]::IsNullOrWhiteSpace([string]$saved.chrome_path)) { $env:DIGCRAWL_CHROME_PATH = [string]$saved.chrome_path }
        Validate-ServiceName
        Validate-Config
        return $true
    } catch {
        Fail "cannot read service configuration at $Path; reinstall the CDP browser service: $($_.Exception.Message)"
    }
}

function Resolve-ManagedService([bool]$PreserveExplicitRuntime = $false) {
    Validate-ServiceName
    if ($ServiceNameExplicit) {
        $script:ServiceConfigPath = Get-ServiceConfigPath
        Import-ServiceConfig $ServiceConfigPath $PreserveExplicitRuntime | Out-Null
        return
    }
    if (Test-Path -LiteralPath $ActiveServicePath -PathType Leaf) {
        $activeName = (Get-Content -LiteralPath $ActiveServicePath -Raw).Trim()
        $script:ServiceName = $activeName
        Validate-ServiceName
        $script:ServiceConfigPath = Get-ServiceConfigPath
        if (-not (Import-ServiceConfig $ServiceConfigPath $PreserveExplicitRuntime)) { Fail "active service metadata is missing at $ServiceConfigPath; reinstall the CDP browser service" }
        if ($ServiceName -ne $activeName) { Fail "active service metadata at $ServiceConfigPath names a different service; reinstall the CDP browser service" }
        return
    }
    # Backward compatibility for services installed before per-service
    # metadata existed. A later install adopts it into the new state layout.
    if (Test-Path -LiteralPath $LegacyServiceConfigPath -PathType Leaf) {
        $script:ServiceConfigPath = $LegacyServiceConfigPath
        Import-ServiceConfig $ServiceConfigPath $PreserveExplicitRuntime | Out-Null
        return
    }
    $script:ServiceConfigPath = Get-ServiceConfigPath
    Import-ServiceConfig $ServiceConfigPath $PreserveExplicitRuntime | Out-Null
}

function Save-ServiceConfig([string]$ChromePath) {
    $serviceConfig = [ordered]@{
        version = 2
        service_name = $ServiceName
        port = $Port
        user_data_dir = $UserDataDir
        log_dir = $LogDir
        chrome_path = $ChromePath
    }
    $temporaryPath = "$ServiceConfigPath.$PID.tmp"
    try {
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $ServiceConfigPath) | Out-Null
        $serviceConfig | ConvertTo-Json | Set-Content -LiteralPath $temporaryPath -Encoding UTF8 -NoNewline
        Move-Item -LiteralPath $temporaryPath -Destination $ServiceConfigPath -Force
    } finally {
        Remove-Item -LiteralPath $temporaryPath -Force -ErrorAction SilentlyContinue
    }
}

function Get-ManagedChromeProcesses {
    # CommandLine can contain quoted arguments when the profile path has
    # spaces. Match complete argument boundaries, not substrings: port 93230,
    # a profile suffix, and Chromium helpers (--type=...) are not this service.
    $portPattern = '(?<!\S)"?--remote-debugging-port="?' + [regex]::Escape([string]$Port) + '"?(?=\s|$)'
    # Win32_Process can preserve quotes around the whole argument or only the
    # value after '='. Accept both serializations while retaining complete
    # token boundaries, so neither port/profile prefixes nor helpers match.
    $profilePattern = '(?<!\S)"?--user-data-dir="?' + [regex]::Escape($UserDataDir) + '"?(?=\s|$)'
    $helperPattern = '(?<!\S)"?--type='
    Get-CimInstance Win32_Process -Filter "Name='chrome.exe' OR Name='msedge.exe'" | Where-Object {
        $_.CommandLine -and $_.CommandLine -match $portPattern -and $_.CommandLine -match $profilePattern -and $_.CommandLine -notmatch $helperPattern
    }
}

function Get-MutedManagedChromeProcesses {
    # A reachable browser with the right port/profile is not sufficient: the
    # dedicated service owns the process and must preserve its no-audio
    # contract across upgrades and restarts.
    $mutePattern = '(?<!\S)"?--mute-audio"?(?=\s|$)'
    Get-ManagedChromeProcesses | Where-Object { $_.CommandLine -match $mutePattern }
}

function Get-ChromeLaunchArguments {
    @(
        '--headless=new',
        '--remote-debugging-address=127.0.0.1',
        "--remote-debugging-port=$Port",
        "--user-data-dir=$UserDataDir",
        '--no-first-run',
        '--no-default-browser-check',
        '--disable-gpu',
        '--mute-audio',
        'about:blank'
    )
}

function Run-Foreground {
    Validate-Config
    if (Test-CdpReady) {
        if (-not (Test-ServiceCdpReady)) {
	    if (@(Get-ManagedChromeProcesses).Count -gt 0) {
	        Fail 'managed CDP browser is missing --mute-audio; run restart'
	    }
            Fail "CDP port $Port is occupied by a browser that does not use this service profile"
        }
        Write-Output "CDP browser with this service profile is already available at $(Get-CdpUrl); monitoring it"
        while (Test-ServiceCdpReady) { Start-Sleep -Seconds 5 }
    }
    Ensure-Directories
    $chrome = Get-ChromePath
	$arguments = Get-ChromeLaunchArguments
    $stdout = Join-Path $LogDir 'chrome.stdout.log'
    $stderr = Join-Path $LogDir 'chrome.stderr.log'
    # Native PowerShell invocation preserves every array item as one process
    # argument, including --user-data-dir paths containing spaces.
    & $chrome @arguments 1>> $stdout 2>> $stderr
    exit $LASTEXITCODE
}

function Install-Service {
    Validate-Config
    Validate-ServiceName
    Prepare-Install
    Ensure-Directories
    $chrome = Get-ChromePath
    # Task Scheduler does not inherit the installer's transient environment.
    # Persist the complete validated launch configuration beside the per-user
    # profile so every later task start uses the same source of truth.
    Save-ServiceConfig $chrome
    # The scheduled Task executes a fresh PowerShell process without this
    # installer's environment. Publish the active identity before starting it
    # so that process resolves exactly the configuration saved above.
    Set-ActiveService
    $powershell = (Get-Command powershell.exe -ErrorAction Stop).Source
    $arguments = "-NoProfile -ExecutionPolicy Bypass -File `"$ScriptPath`" run-foreground"
    $action = New-ScheduledTaskAction -Execute $powershell -Argument $arguments
    $taskUser = if ($env:USERDOMAIN) { "$env:USERDOMAIN\$env:USERNAME" } else { $env:USERNAME }
    $trigger = New-ScheduledTaskTrigger -AtLogOn -User $taskUser
    $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit (New-TimeSpan -Seconds 0)
    Register-ScheduledTask -TaskName $ServiceName -Action $action -Trigger $trigger -Settings $settings -Description 'Dedicated local Chrome CDP browser for DigCrawl' -Force | Out-Null
    Start-ScheduledTask -TaskName $ServiceName
    for ($attempt = 1; $attempt -le 15; $attempt++) {
        if (Test-ServiceCdpReady) { break }
        Start-Sleep -Seconds 1
    }
    if (-not (Test-ServiceCdpReady)) { Fail 'CDP endpoint did not become ready within 15 seconds with the configured service profile' }
    Write-Output "installed CDP browser Scheduled Task: $ServiceName"
}

function Start-Service {
    if (-not (Get-ScheduledTask -TaskName $ServiceName -ErrorAction SilentlyContinue)) { Fail 'service is not installed; run install first' }
    Start-ScheduledTask -TaskName $ServiceName
    for ($attempt = 1; $attempt -le 15; $attempt++) {
        if (Test-ServiceCdpReady) { return }
        Start-Sleep -Seconds 1
    }
    Fail 'CDP endpoint did not become ready within 15 seconds'
}

function Stop-Service {
    Stop-ScheduledTask -TaskName $ServiceName -ErrorAction SilentlyContinue
    Get-ManagedChromeProcesses | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
}

function Restart-ManagedService {
    # Keep restart on the same lifecycle boundary as install: task stop and
    # Chrome termination are asynchronous, so a replacement must not race the
    # old process for its CDP port or persistent profile.
    Stop-Service
    if (-not (Wait-CdpStopped)) {
        Fail "CDP endpoint did not stop within 10 seconds"
    }
    Start-Service
}

function Show-Status {
    if (Test-ServiceCdpReady) {
        Write-Output "OK   CDP endpoint: $(Get-CdpUrl)"
        return
    }
    if (Test-CdpReady) {
	if (@(Get-ManagedChromeProcesses).Count -gt 0) {
	    Write-Output 'FAIL CDP browser is missing --mute-audio; run restart'
	    exit 1
	}
        Write-Output "FAIL CDP endpoint is occupied by a browser outside this service profile: $(Get-CdpUrl)"
        exit 1
    }
    Write-Output "FAIL CDP endpoint unavailable: $(Get-CdpUrl)"
    exit 1
}

function Show-Logs {
    Ensure-Directories
    $stdout = Join-Path $LogDir 'chrome.stdout.log'
    $stderr = Join-Path $LogDir 'chrome.stderr.log'
    New-Item -ItemType File -Force -Path $stdout, $stderr | Out-Null
    Get-Content -Path $stdout, $stderr -Tail 100
}

# The Windows regression script dot-sources these functions and supplies fake
# CIM/Task Scheduler commands. Production execution never sets this variable.
if ($env:DIGCRAWL_CDP_BROWSER_SERVICE_TEST -eq '1') { return }

Resolve-ManagedService ($Command -eq 'install')

if ($Command -eq 'install') {
    # Existing v1 installations used one global file. Reinstalling adopts the
    # selected service into the per-service state directory without deleting
    # another legacy/default task that might still be intentionally present.
    $script:ServiceConfigPath = Get-ServiceConfigPath
}

switch ($Command) {
    'install' { Install-Service; Show-Status }
    'start' { Start-Service; Show-Status }
    'stop' { Stop-Service }
    'restart' { Restart-ManagedService }
    'status' { Show-Status }
    'logs' { Show-Logs }
    'uninstall' { Stop-Service; Unregister-ScheduledTask -TaskName $ServiceName -Confirm:$false -ErrorAction SilentlyContinue; Remove-Item -LiteralPath $ServiceConfigPath -Force -ErrorAction SilentlyContinue; Remove-ActiveServiceIfCurrent }
    'run-foreground' { Run-Foreground }
}
