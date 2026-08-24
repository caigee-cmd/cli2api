[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$RootDir = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$EnvFile = Join-Path $RootDir "deploy\.env"
$EnvExample = Join-Path $RootDir "deploy\.env.example"
$ComposeFile = Join-Path $RootDir "deploy\docker-compose.yml"

function Invoke-Compose {
    & docker compose --env-file $EnvFile -f $ComposeFile @args | Out-Host
    return ($LASTEXITCODE -eq 0)
}

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw "Docker is required. Install and start Docker Desktop first."
}
if (-not (Test-Path $EnvFile)) {
    Copy-Item $EnvExample $EnvFile
    Write-Host "Created $EnvFile; SQLite will generate the API key on first startup."
}
& docker info *> $null
if ($LASTEXITCODE -ne 0) {
    throw "Docker Desktop is not running."
}

$StartedAt = [DateTime]::UtcNow.ToString("yyyy-MM-ddTHH:mm:ssZ")
if (-not (Invoke-Compose pull)) {
    Write-Host "Published image unavailable; building the image locally."
    if (-not (Invoke-Compose up -d --build)) {
        throw "Failed to build and start CLI2API."
    }
} elseif (-not (Invoke-Compose up -d)) {
    throw "Failed to start CLI2API."
}

$Healthy = $false
for ($Attempt = 0; $Attempt -lt 60; $Attempt++) {
    try {
        $Health = Invoke-RestMethod -Uri "http://127.0.0.1:3010/health" -TimeoutSec 2
        if ($Health.ok) {
            $Healthy = $true
            break
        }
    } catch {
    }
    Start-Sleep -Seconds 1
}

if (-not $Healthy) {
    [void](Invoke-Compose ps)
    [void](Invoke-Compose logs --no-color --tail=100 qoder-api-proxy)
    throw "CLI2API did not become healthy."
}

Write-Host "CLI2API is running at http://127.0.0.1:3010"
$Logs = & docker compose --env-file $EnvFile -f $ComposeFile logs --no-color --since $StartedAt qoder-api-proxy 2>$null
$NewKey = $Logs | Select-String -SimpleMatch "initialized API key"
if ($NewKey) {
    $NewKey.Line | Write-Host
} else {
    Write-Host "The API key is already stored in the SQLite database."
}
