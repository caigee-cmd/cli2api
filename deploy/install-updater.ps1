[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$RootDir = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$EnvFile = Join-Path $RootDir "deploy\.env"
$EnvExample = Join-Path $RootDir "deploy\.env.example"
$ComposeFile = Join-Path $RootDir "deploy\docker-compose.yml"
$InstallDir = Join-Path $env:LOCALAPPDATA "cli2api-updater"
$Binary = Join-Path $InstallDir "cli2api-updater.exe"
$CandidateBinary = Join-Path $InstallDir "cli2api-updater.new.exe"
$TokenFile = Join-Path $InstallDir "token"
$StatusFile = Join-Path $InstallDir "status.json"
$Runner = Join-Path $InstallDir "run-updater.ps1"
$StdoutLog = Join-Path $InstallDir "stdout.log"
$StderrLog = Join-Path $InstallDir "stderr.log"
$SocketPlaceholder = Join-Path $InstallDir "socket-placeholder"
$TaskName = "CLI2API Updater"
$ContainerName = "qoder-api-proxy"
$ServiceName = "qoder-api-proxy"
$ImageRepository = "ghcr.io/caigee-cmd/cli2api"
$ListenAddress = "127.0.0.1:3011"
$GitHubRepository = "caigee-cmd/cli2api"

function Require-Command {
    param([string]$Name)
    $Command = Get-Command $Name -ErrorAction SilentlyContinue
    if (-not $Command) {
        throw "Missing command: $Name"
    }
    return $Command
}

function Read-EnvValue {
    param([string]$Path, [string]$Key)
    if (-not (Test-Path $Path)) {
        return ""
    }
    $Match = Get-Content $Path | Where-Object { $_ -match "^$([Regex]::Escape($Key))=" } | Select-Object -First 1
    if (-not $Match) {
        return ""
    }
    return ($Match -replace "^$([Regex]::Escape($Key))=", "").Trim()
}

function Set-EnvValue {
    param([string]$Path, [string]$Key, [string]$Value)

    $Lines = @()
    if (Test-Path $Path) {
        $Lines = @(Get-Content $Path)
    }
    $Prefix = "$Key="
    $Found = $false
    for ($Index = 0; $Index -lt $Lines.Count; $Index++) {
        if ($Lines[$Index].StartsWith($Prefix, [StringComparison]::Ordinal)) {
            if (-not $Found) {
                $Lines[$Index] = "$Prefix$Value"
                $Found = $true
            } else {
                $Lines[$Index] = $null
            }
        }
    }
    $Lines = @($Lines | Where-Object { $null -ne $_ })
    if (-not $Found) {
        $Lines += "$Prefix$Value"
    }
    $Encoding = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($Path, (($Lines -join [Environment]::NewLine) + [Environment]::NewLine), $Encoding)
}

function New-RandomToken {
    $Bytes = New-Object byte[] 32
    $Rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $Rng.GetBytes($Bytes)
    } finally {
        $Rng.Dispose()
    }
    return -join ($Bytes | ForEach-Object { $_.ToString("x2") })
}

function Protect-Path {
    param([string]$Path)
    $Identity = [System.Security.Principal.WindowsIdentity]::GetCurrent()
    $UserSid = $Identity.User.Value
    $Item = Get-Item $Path
    if ($Item.PSIsContainer) {
        $UserGrant = "*${UserSid}:(OI)(CI)F"
        $SystemGrant = "*S-1-5-18:(OI)(CI)F"
    } else {
        $UserGrant = "*${UserSid}:F"
        $SystemGrant = "*S-1-5-18:F"
    }
    & icacls.exe $Path "/inheritance:r" "/grant:r" $UserGrant "/grant:r" $SystemGrant | Out-Null
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to restrict permissions on $Path"
    }
}


function Get-RunningReleaseVersion {
    try {
        $Health = Invoke-RestMethod -Uri "http://127.0.0.1:3010/health" -TimeoutSec 3
    } catch {
        return $null
    }
    $Version = ([string]$Health.version).Trim()
    if ($Version -match '^v[0-9]+\.[0-9]+\.[0-9]+$') {
        return $Version
    }
    if ($Version -match '^[0-9]+\.[0-9]+\.[0-9]+$') {
        return "v$Version"
    }
    return $null
}

function Get-UpdaterAssetName {
    $NativeArchitecture = $env:PROCESSOR_ARCHITECTURE
    if (-not [string]::IsNullOrWhiteSpace($env:PROCESSOR_ARCHITEW6432)) {
        $NativeArchitecture = $env:PROCESSOR_ARCHITEW6432
    }
    switch ($NativeArchitecture.ToUpperInvariant()) {
        "AMD64" { return "cli2api-updater_windows_amd64.exe" }
        "ARM64" { return "cli2api-updater_windows_arm64.exe" }
        default { throw "Unsupported Windows architecture: $NativeArchitecture" }
    }
}

function Install-ReleasedUpdater {
    param([string]$Destination)

    $Version = Get-RunningReleaseVersion
    if ([string]::IsNullOrWhiteSpace($Version)) {
        return $false
    }
    $AssetName = Get-UpdaterAssetName
    $TempDir = Join-Path ([System.IO.Path]::GetTempPath()) ("cli2api-updater-" + [Guid]::NewGuid().ToString("N"))
    New-Item -ItemType Directory -Force -Path $TempDir | Out-Null
    $AssetPath = Join-Path $TempDir $AssetName
    $ChecksumPath = Join-Path $TempDir "cli2api-updater_checksums.txt"

    try {
        foreach ($SourceLabel in @($Version, "latest")) {
            if ($SourceLabel -eq "latest") {
                $BaseUrl = "https://github.com/$GitHubRepository/releases/latest/download"
            } else {
                $BaseUrl = "https://github.com/$GitHubRepository/releases/download/$SourceLabel"
            }
            try {
                Invoke-WebRequest -UseBasicParsing -Uri "$BaseUrl/$AssetName" -OutFile $AssetPath
            } catch {
                continue
            }
            try {
                Invoke-WebRequest -UseBasicParsing -Uri "$BaseUrl/cli2api-updater_checksums.txt" -OutFile $ChecksumPath
            } catch {
                throw "Updater checksum file is unavailable from $SourceLabel."
            }

            $EscapedAsset = [Regex]::Escape($AssetName)
            $ChecksumLine = Get-Content $ChecksumPath | Where-Object { $_ -match "\s+$EscapedAsset$" } | Select-Object -First 1
            if (-not $ChecksumLine) {
                throw "Updater checksum is missing for $AssetName."
            }
            $ExpectedHash = ($ChecksumLine -split '\s+')[0].ToLowerInvariant()
            $ActualHash = (Get-FileHash -Algorithm SHA256 $AssetPath).Hash.ToLowerInvariant()
            if ($ActualHash -ne $ExpectedHash) {
                throw "Updater checksum verification failed for $AssetName."
            }

            Unblock-File -Path $AssetPath -ErrorAction SilentlyContinue
            Copy-Item -Force $AssetPath $Destination
            Write-Host "Installed updater asset $AssetName from $SourceLabel."
            return $true
        }
        Write-Host "Released updater asset is unavailable; trying a local build."
        return $false
    } finally {
        Remove-Item -Recurse -Force $TempDir -ErrorAction SilentlyContinue
    }
}

$DockerCommand = Require-Command "docker"
[void](Require-Command "Register-ScheduledTask")
[void](Require-Command "New-ScheduledTaskAction")
[void](Require-Command "New-ScheduledTaskTrigger")
[void](Require-Command "New-ScheduledTaskPrincipal")
[void](Require-Command "New-ScheduledTaskSettingsSet")
[void](Require-Command "icacls.exe")

& docker info *> $null
if ($LASTEXITCODE -ne 0) {
    throw "Start Docker Desktop before installing the managed updater."
}

if (-not (Test-Path $EnvFile)) {
    Copy-Item $EnvExample $EnvFile
}
New-Item -ItemType Directory -Force -Path $InstallDir, $SocketPlaceholder | Out-Null

$Token = Read-EnvValue $EnvFile "UPDATE_AGENT_TOKEN"
if ([string]::IsNullOrWhiteSpace($Token)) {
    $Token = New-RandomToken
}
$DockerSocketPath = $SocketPlaceholder.Replace("\", "/")
Set-EnvValue $EnvFile "CLI2API_UPDATER_SOCKET_DIR" $DockerSocketPath
Set-EnvValue $EnvFile "UPDATE_AGENT_URL" "http://host.docker.internal:3011"
Set-EnvValue $EnvFile "UPDATE_AGENT_TOKEN" $Token

if (-not (Install-ReleasedUpdater $CandidateBinary)) {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        throw "No updater asset exists for the running version. Install Go 1.25.6+ to build it locally."
    }
    Write-Host "Building cli2api-updater.exe from the checked-out source."
    Push-Location $RootDir
    try {
        & go build -trimpath -o $CandidateBinary .\cmd\updater
        if ($LASTEXITCODE -ne 0) {
            throw "Failed to build cli2api-updater.exe"
        }
    } finally {
        Pop-Location
    }
}

$ExistingTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($ExistingTask) {
    Stop-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    $TaskState = "Running"
    for ($Attempt = 0; $Attempt -lt 20; $Attempt++) {
        $CurrentTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
        $TaskState = if ($CurrentTask) { [string]$CurrentTask.State } else { "Stopped" }
        if ($TaskState -ne "Running") {
            break
        }
        Start-Sleep -Milliseconds 250
    }
    if ($TaskState -eq "Running") {
        throw "Could not stop the existing updater task."
    }
}
try {
    Move-Item -Force $CandidateBinary $Binary
} catch {
    if ($ExistingTask) {
        Start-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
    }
    throw
}

$Encoding = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText($TokenFile, "$Token`r`n", $Encoding)
Protect-Path $InstallDir
Protect-Path $EnvFile

$DockerDir = Split-Path -Parent $DockerCommand.Path
$RunnerContent = @"
Set-StrictMode -Version Latest
`$ErrorActionPreference = "Stop"
`$env:HOME = `$env:USERPROFILE
`$env:PATH = "$DockerDir;`$env:SystemRoot\System32;`$env:SystemRoot;`$env:PATH"
`$UpdaterArguments = @(
    "--listen", "$ListenAddress",
    "--auth-token-file", "$TokenFile",
    "--status-file", "$StatusFile",
    "--compose-file", "$ComposeFile",
    "--env-file", "$EnvFile",
    "--service", "$ServiceName",
    "--container", "$ContainerName",
    "--image-repository", "$ImageRepository",
    "--health-url", "http://127.0.0.1:3010/health"
)
& "$Binary" `$UpdaterArguments 1>> "$StdoutLog" 2>> "$StderrLog"
exit `$LASTEXITCODE
"@
[System.IO.File]::WriteAllText($Runner, $RunnerContent, $Encoding)

$Identity = [System.Security.Principal.WindowsIdentity]::GetCurrent()
$UserName = $Identity.Name
$ActionArguments = "-NoProfile -NonInteractive -ExecutionPolicy Bypass -File `"$Runner`""
$Action = New-ScheduledTaskAction -Execute "powershell.exe" -Argument $ActionArguments -WorkingDirectory $RootDir
$Trigger = New-ScheduledTaskTrigger -AtLogOn -User $UserName
$Principal = New-ScheduledTaskPrincipal -UserId $UserName -LogonType Interactive -RunLevel Limited
$Settings = New-ScheduledTaskSettingsSet -StartWhenAvailable -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero)

if ($ExistingTask) {
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
}
Register-ScheduledTask -TaskName $TaskName -Action $Action -Trigger $Trigger -Principal $Principal -Settings $Settings | Out-Null
Start-ScheduledTask -TaskName $TaskName

$Headers = @{ Authorization = "Bearer $Token" }
$Ready = $false
for ($Attempt = 0; $Attempt -lt 30; $Attempt++) {
    try {
        $Health = Invoke-RestMethod -Uri "http://127.0.0.1:3011/health" -Headers $Headers -TimeoutSec 2
        if ($Health.ok) {
            $Ready = $true
            break
        }
    } catch {
    }
    Start-Sleep -Seconds 1
}
if (-not $Ready) {
    throw "Updater did not become ready. Check $StderrLog"
}

& docker container inspect $ContainerName *> $null
$ContainerExists = $LASTEXITCODE -eq 0
if ($ContainerExists) {
    $ContainerImage = (& docker inspect --format "{{.Image}}" $ContainerName).Trim()
    & docker run --rm --entrypoint curl $ContainerImage -fsS -H "Authorization: Bearer $Token" "http://host.docker.internal:3011/health" *> $null
    if ($LASTEXITCODE -ne 0) {
        throw "Docker Desktop cannot reach the updater through host.docker.internal:3011."
    }
}

Write-Host "Managed updater installed as the current-user scheduled task '$TaskName'."
Write-Host "Recreate qoder-api-proxy once so it can use the updater channel:"
Write-Host "docker compose --env-file `"$EnvFile`" -f `"$ComposeFile`" up -d --force-recreate qoder-api-proxy"
