[CmdletBinding()]
param(
    [string]$BinDir = "",
    [int]$EdgePort = 18080,
    [int]$OrchestratorPort = 18081,
    [int]$ProviderPort = 18082
)

$ErrorActionPreference = "Stop"
$BinDir = if ($BinDir) { $BinDir } else { Join-Path $PSScriptRoot "..\..\bin" }
$processes = @()
$logs = Join-Path $PSScriptRoot "..\..\tmp"
New-Item -ItemType Directory -Force -Path $logs | Out-Null

function Start-GreenRouteProcess {
    param([string]$Executable, [string]$StdoutName, [string]$StderrName)
    $process = Start-Process -FilePath $Executable -PassThru -WindowStyle Hidden `
        -RedirectStandardOutput (Join-Path $logs $StdoutName) `
        -RedirectStandardError (Join-Path $logs $StderrName)
    $script:processes += $process
}

try {
    $env:APP_ENV = "development"
    $env:INTERNAL_API_TOKEN = "local-integration-internal-token-1234567890"
    $env:PROVIDER_MODE = "stub"
    $env:PROVIDER_STUB_SCENARIO = "normal"
    $env:HTTP_ADDR = ":$ProviderPort"
    Start-GreenRouteProcess (Join-Path $BinDir "provider-yandex.exe") "provider.out.log" "provider.err.log"

    $env:ORCHESTRATOR_ADDR = ":$OrchestratorPort"
    $env:PROVIDER_URL = "http://127.0.0.1:$ProviderPort"
    $env:PROVIDER_DATA_STORAGE_ALLOWED = "false"
    $env:ENABLE_ENHANCED_SEARCH = "true"
    Start-GreenRouteProcess (Join-Path $BinDir "routing-orchestrator.exe") "orchestrator.out.log" "orchestrator.err.log"

    $env:EDGE_ADDR = ":$EdgePort"
    $env:ORCHESTRATOR_URL = "http://127.0.0.1:$OrchestratorPort"
    $env:ENABLE_ANONYMOUS_USAGE = "true"
    $env:REDIS_URL = ""
    $env:DATABASE_URL = ""
    Start-GreenRouteProcess (Join-Path $BinDir "edge-api.exe") "edge.out.log" "edge.err.log"

    $ready = $false
    for ($attempt = 0; $attempt -lt 40; $attempt++) {
        try {
            $health = Invoke-RestMethod -Uri "http://127.0.0.1:$EdgePort/health/ready" -TimeoutSec 2
            if ($health.status -eq "ready") {
                $ready = $true
                break
            }
        } catch {}
        Start-Sleep -Milliseconds 250
    }
    if (-not $ready) { throw "edge-api did not become ready" }

    $payload = @{
        origin = @{ latitude = 55.751244; longitude = 37.618423 }
        destination = @{ latitude = 55.8319; longitude = 37.4116 }
        waypoints = @()
        routingMode = "GREENEST"
        maxExtraDistanceMeters = 30000
        maxExtraDistancePercent = 100
        maxExtraTimeSeconds = 1800
        avoidTolls = $false
        avoidUnpaved = $false
        strictness = 0.8
        maxProviderRequests = 8
        searchDeadlineMs = 8000
    } | ConvertTo-Json -Depth 8 -Compress
    $headers = @{ "Idempotency-Key" = "integration-0001" }
    $started = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$EdgePort/api/v1/route-searches" `
        -Method Post -ContentType "application/json" -Body $payload -Headers $headers -TimeoutSec 10
    $accepted = $started.Content | ConvertFrom-Json
    $result = $accepted
    for ($attempt = 0; $attempt -lt 80; $attempt++) {
        $result = Invoke-RestMethod -Uri "http://127.0.0.1:$EdgePort/api/v1/route-searches/$($accepted.searchId)" -TimeoutSec 3
        if ($result.status -in @("COMPLETED", "DEGRADED", "FAILED")) { break }
        Start-Sleep -Milliseconds 100
    }
    if ($result.status -notin @("COMPLETED", "DEGRADED")) { throw "search did not complete successfully: $($result.status)" }
    if (-not $result.selectedRoute.confidence.level) { throw "selected route has no confidence" }
    if ($result.selectedRoute.reasonCodes.Count -eq 0) { throw "selected route has no reason codes" }
    if (-not ($result.PSObject.Properties.Name -contains "bestEffortRoutes")) { throw "bestEffortRoutes contract field is missing" }
    if ($result.bestEffortRoutes.Count -ne 0) { throw "non-strict search returned strict best-effort proof routes" }
    if ($result.providerUsage.requestsUsed -gt $result.providerUsage.requestBudget) { throw "provider budget was exceeded" }

    $replay = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$EdgePort/api/v1/route-searches" `
        -Method Post -ContentType "application/json" -Body $payload -Headers $headers -TimeoutSec 10
    if ($replay.Headers["Idempotency-Replayed"] -ne "true") { throw "idempotency replay header missing" }
    $suggest = Invoke-RestMethod -Uri "http://127.0.0.1:$EdgePort/api/v1/geosuggest?q=Moscow&lang=en_US&limit=3" -TimeoutSec 5
    if ($suggest.suggestions.Count -lt 1) { throw "stub geosuggest returned no results" }
    $events = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$EdgePort/api/v1/route-searches/$($accepted.searchId)/events" `
        -Headers @{ Accept = "text/event-stream" } -TimeoutSec 10
    if ($events.Content -notmatch "SEARCH_COMPLETED|SEARCH_DEGRADED") { throw "terminal SSE event missing" }

    Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$EdgePort/api/v1/route-searches/$($accepted.searchId)" -Method Delete -TimeoutSec 5 | Out-Null
    [pscustomobject]@{
        ready = $ready
        postStatus = $started.StatusCode
        searchId = $accepted.searchId
        finalStatus = $result.status
        selected = $result.selectedRoute.candidateId
        confidence = $result.selectedRoute.confidence.level
        providerRequests = $result.providerUsage.requestsUsed
        requestBudget = $result.providerUsage.requestBudget
        replayed = $replay.Headers["Idempotency-Replayed"]
        suggestions = $suggest.suggestions.Count
        sseEvents = ([regex]::Matches($events.Content, "event: ")).Count
    } | ConvertTo-Json -Compress
} finally {
    foreach ($process in $processes) {
        if ($process -and -not $process.HasExited) {
            Stop-Process -Id $process.Id -Force
        }
    }
}
