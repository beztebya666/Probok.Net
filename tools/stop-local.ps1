[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$workspace = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$allowedRoots = @(
    (Join-Path $workspace "artifacts\bin"),
    (Join-Path $workspace "bin")
)
$stopped = @()

foreach ($port in @(8080, 8081, 8082)) {
    $listener = Get-NetTCPConnection -State Listen -LocalPort $port -ErrorAction SilentlyContinue | Select-Object -First 1
    if (-not $listener) { continue }
    $process = Get-Process -Id $listener.OwningProcess -ErrorAction Stop
    $executable = $process.Path
    $approved = $false
    foreach ($root in $allowedRoots) {
        if ($executable -and $executable.StartsWith($root, [StringComparison]::OrdinalIgnoreCase)) {
            $approved = $true
            break
        }
    }
    if (-not $approved) {
        throw "Refusing to stop PID $($process.Id) on port $port because it is outside the GreenRoute binary directories"
    }
    Stop-Process -Id $process.Id
    $stopped += [pscustomobject]@{ port = $port; pid = $process.Id; process = $process.ProcessName }
}

$stopped | ConvertTo-Json -Compress
