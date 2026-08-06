[CmdletBinding()]
param([int]$Port = 3000, [string]$Hostname = "0.0.0.0")

$ErrorActionPreference = "Stop"
$workspace = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$web = Join-Path $workspace "apps\web"
$environmentPath = Join-Path $workspace ".env"
$logPath = Join-Path $workspace "artifacts\logs"

if (Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue) {
    throw "Port $Port is already in use"
}

# The web process must never inherit provider credentials. Read the local file
# only to export the browser key and privacy-safe capability signals.
$serverSecretNames = @(
    "INTERNAL_API_TOKEN",
    "YANDEX_ROUTER_API_KEY",
    "YANDEX_GEOCODER_API_KEY",
    "YANDEX_GEOSUGGEST_API_KEY",
    "TWO_GIS_API_KEY",
    "DGIS_API_KEY"
)
foreach ($name in $serverSecretNames) {
    [Environment]::SetEnvironmentVariable($name, $null, "Process")
}

$safeValues = @{}
foreach ($line in Get-Content -LiteralPath $environmentPath) {
    if ($line -match '^([A-Za-z_][A-Za-z0-9_]*)=(.*)$') {
        $name = $matches[1]
        $value = $matches[2].Trim()
        if ($value.Length -ge 2 -and (($value.StartsWith('"') -and $value.EndsWith('"')) -or ($value.StartsWith("'") -and $value.EndsWith("'")))) {
            $value = $value.Substring(1, $value.Length - 2)
        }
        $safeValues[$name] = $value
    }
}

if ($safeValues.ContainsKey("NEXT_PUBLIC_YANDEX_MAPS_API_KEY")) {
    $env:NEXT_PUBLIC_YANDEX_MAPS_API_KEY = $safeValues["NEXT_PUBLIC_YANDEX_MAPS_API_KEY"]
}
$twoGisMapGLKey = if ($safeValues["NEXT_PUBLIC_2GIS_MAPGL_API_KEY"]) {
    $safeValues["NEXT_PUBLIC_2GIS_MAPGL_API_KEY"]
} elseif ($safeValues["TWO_GIS_API_KEY"]) {
    $safeValues["TWO_GIS_API_KEY"]
} else {
    # Localhost preview compatibility only. Production requires a distinct,
    # origin-restricted MapGL credential (enforced by the Helm chart).
    $safeValues["DGIS_API_KEY"]
}
if (-not [string]::IsNullOrWhiteSpace($twoGisMapGLKey)) {
    $env:NEXT_PUBLIC_2GIS_MAPGL_API_KEY = $twoGisMapGLKey
} else {
    Remove-Item Env:NEXT_PUBLIC_2GIS_MAPGL_API_KEY -ErrorAction SilentlyContinue
}
if ($safeValues.ContainsKey("NEXT_PUBLIC_2GIS_MAPGL_DARK_STYLE_ID")) {
    $env:NEXT_PUBLIC_2GIS_MAPGL_DARK_STYLE_ID = $safeValues["NEXT_PUBLIC_2GIS_MAPGL_DARK_STYLE_ID"]
} else {
    Remove-Item Env:NEXT_PUBLIC_2GIS_MAPGL_DARK_STYLE_ID -ErrorAction SilentlyContinue
}
$env:GREENROUTE_PROVIDER_MODE = if ($safeValues["PROVIDER_MODE"]) { $safeValues["PROVIDER_MODE"] } else { "stub" }
$env:GREENROUTE_ADDRESS_PROVIDER_MODE = if ($safeValues["ADDRESS_PROVIDER_MODE"]) { $safeValues["ADDRESS_PROVIDER_MODE"] } else { "auto" }
$env:GREENROUTE_YANDEX_GEOCODER_CONFIGURED = [string](-not [string]::IsNullOrWhiteSpace($safeValues["YANDEX_GEOCODER_API_KEY"]))
$env:GREENROUTE_YANDEX_GEOSUGGEST_CONFIGURED = [string](-not [string]::IsNullOrWhiteSpace($safeValues["YANDEX_GEOSUGGEST_API_KEY"]))
$env:GREENROUTE_DGIS_CONFIGURED = [string](-not [string]::IsNullOrWhiteSpace($safeValues["DGIS_API_KEY"]))
$env:GREENROUTE_YANDEX_TRAFFIC_AVAILABLE = [string]($safeValues["GREENROUTE_YANDEX_TRAFFIC_AVAILABLE"] -eq "true")
$env:GREENROUTE_ADMIN_ENABLED = [string]($safeValues["GREENROUTE_ADMIN_ENABLED"] -eq "true")
$env:GREENROUTE_ADMIN_IN_MENU = [string]($safeValues["GREENROUTE_ADMIN_IN_MENU"] -eq "true")
$env:NEXT_PUBLIC_DEMO_MODE = "false"
$env:NEXT_PUBLIC_EDGE_API_BASE_URL = "http://localhost:8080"
$env:EDGE_API_PUBLIC_URL = "http://localhost:8080"
New-Item -ItemType Directory -Force -Path $logPath | Out-Null

$process = Start-Process -FilePath "npm.cmd" -ArgumentList @("run", "dev", "--", "--hostname", $Hostname, "--port", "$Port") `
    -WorkingDirectory $web -PassThru -WindowStyle Hidden `
    -RedirectStandardOutput (Join-Path $logPath "web.out.log") `
    -RedirectStandardError (Join-Path $logPath "web.err.log")

for ($attempt = 0; $attempt -lt 120; $attempt++) {
    try {
        $response = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$Port/api/health" -TimeoutSec 2
        if ($response.StatusCode -eq 200) {
            [pscustomobject]@{ service = "web"; port = $Port; launcherPid = $process.Id } | ConvertTo-Json -Compress
            exit 0
        }
    } catch {}
    Start-Sleep -Milliseconds 250
}

if (-not $process.HasExited) { Stop-Process -Id $process.Id }
throw "Web application did not become ready on port $Port"
