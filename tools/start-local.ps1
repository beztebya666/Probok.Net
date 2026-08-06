[CmdletBinding()]
param(
    [string]$EnvironmentFile = ".env",
    [string]$BinaryDirectory = "artifacts\bin"
)

$ErrorActionPreference = "Stop"
$workspace = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$environmentPath = Join-Path $workspace $EnvironmentFile
$binaryPath = Join-Path $workspace $BinaryDirectory
$logPath = Join-Path $workspace "artifacts\logs"

if (-not (Test-Path -LiteralPath $environmentPath -PathType Leaf)) {
    throw "Environment file not found: $environmentPath"
}
if (-not (Test-Path -LiteralPath $binaryPath -PathType Container)) {
    throw "Binary directory not found: $binaryPath. Build the services first."
}
New-Item -ItemType Directory -Force -Path $logPath | Out-Null

foreach ($line in Get-Content -LiteralPath $environmentPath) {
    if ($line -match '^([A-Za-z_][A-Za-z0-9_]*)=(.*)$') {
        $value = $matches[2].Trim()
        if ($value.Length -ge 2 -and (($value.StartsWith('"') -and $value.EndsWith('"')) -or ($value.StartsWith("'") -and $value.EndsWith("'")))) {
            $value = $value.Substring(1, $value.Length - 2)
        }
        [Environment]::SetEnvironmentVariable($matches[1], $value, "Process")
    }
}

if ($env:PROVIDER_MODE -eq "2gis" -and [string]::IsNullOrWhiteSpace($env:DGIS_API_KEY)) {
    throw "DGIS_API_KEY is required for PROVIDER_MODE=2gis"
}
if ([string]::IsNullOrWhiteSpace($env:INTERNAL_API_TOKEN)) {
    throw "INTERNAL_API_TOKEN is required in the local environment file"
}

$env:APP_ENV = "local"
$env:PROVIDER_DATA_STORAGE_ALLOWED = "false"
$env:PROVIDER_DATA_MODIFICATION_ALLOWED = "false"
$env:REQUIRE_REDIS = "false"
$env:REQUIRE_DATABASE = "false"
$env:ENABLE_ANONYMOUS_USAGE = "true"

# Phones and tablets on the same network open the UI by this machine's LAN
# address, so those origins must be allowed too. Only private addresses are
# collected; the loopback origins stay first for the local browser.
$localOrigins = [System.Collections.Generic.List[string]]::new()
$localOrigins.Add("http://localhost:3000")
$localOrigins.Add("http://127.0.0.1:3000")
try {
    Get-NetIPAddress -AddressFamily IPv4 -ErrorAction Stop |
        Where-Object { $_.IPAddress -match '^(10\.|192\.168\.|172\.(1[6-9]|2[0-9]|3[01])\.)' } |
        ForEach-Object {
            $localOrigins.Add("http://$($_.IPAddress):3000")
            # nip.io gives the same address a hostname, which is what map
            # providers accept in their referer restrictions.
            $localOrigins.Add("http://$($_.IPAddress.Replace('.', '-')).nip.io:3000")
        }
} catch {
    Write-Verbose "Could not enumerate local IPv4 addresses: $_"
}
$env:CORS_ALLOWED_ORIGINS = ($localOrigins | Select-Object -Unique) -join ","
$env:REDIS_URL = ""
$env:DATABASE_URL = ""

$started = @()

function Get-ListenerProcess {
    param([int]$Port)
    $listener = Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($listener) { return Get-Process -Id $listener.OwningProcess -ErrorAction Stop }
    return $null
}

function Start-GreenRouteService {
    param([string]$Name, [string]$Executable, [int]$Port)
    $existing = Get-ListenerProcess -Port $Port
    if ($existing) {
        throw "Port $Port for $Name is already owned by PID $($existing.Id). Refusing to reuse a process whose environment cannot be verified; run tools/stop-local.ps1 first."
    }
    $process = Start-Process -FilePath $Executable -WorkingDirectory $workspace -PassThru -WindowStyle Hidden `
        -RedirectStandardOutput (Join-Path $logPath "$Name.out.log") `
        -RedirectStandardError (Join-Path $logPath "$Name.err.log")
    $script:started += $process
    return [pscustomobject]@{ service = $Name; port = $Port; pid = $process.Id; started = $true }
}

function Wait-Ready {
    param([string]$Name, [int]$Port)
    for ($attempt = 0; $attempt -lt 60; $attempt++) {
        try {
            $response = Invoke-RestMethod -Uri "http://127.0.0.1:$Port/health/ready" -TimeoutSec 2
            if ($response.status -eq "ready") { return }
        } catch {}
        Start-Sleep -Milliseconds 250
    }
    throw "$Name did not become ready on port $Port"
}

try {
    $env:PROVIDER_ADDR = "127.0.0.1:8082"
    $provider = Start-GreenRouteService -Name "provider" -Executable (Join-Path $binaryPath "provider-yandex.exe") -Port 8082
    Wait-Ready -Name "provider" -Port 8082

    $env:ORCHESTRATOR_ADDR = "127.0.0.1:8081"
    $env:PROVIDER_URL = "http://127.0.0.1:8082"
    $orchestrator = Start-GreenRouteService -Name "orchestrator" -Executable (Join-Path $binaryPath "routing-orchestrator.exe") -Port 8081
    Wait-Ready -Name "orchestrator" -Port 8081

    # The edge API is the only service a browser talks to, so it is the only one
    # bound to every interface. The orchestrator and the provider adapter stay on
    # loopback: they hold the internal token and the provider keys.
    $env:EDGE_ADDR = "0.0.0.0:8080"
    $env:ORCHESTRATOR_URL = "http://127.0.0.1:8081"
    $edge = Start-GreenRouteService -Name "edge" -Executable (Join-Path $binaryPath "edge-api.exe") -Port 8080
    Wait-Ready -Name "edge" -Port 8080

    @($provider, $orchestrator, $edge) | ConvertTo-Json -Compress
} catch {
    foreach ($process in $started) {
        if ($process -and -not $process.HasExited) { Stop-Process -Id $process.Id -Force }
    }
    throw
}
