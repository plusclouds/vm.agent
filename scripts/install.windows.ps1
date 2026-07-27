<#
.SYNOPSIS
    Installs (or upgrades) the PlusClouds VM agent as a Windows service.

.DESCRIPTION
    Resolves the latest GitHub release (or a pinned version), downloads
    plusclouds.windows, installs it under Program Files, registers it as a
    Windows service set to start automatically on boot and restart on
    failure, and starts it. Safe to re-run for upgrades: it stops the
    existing service, replaces the binary, and re-registers the service
    configuration in place.

    There is no config file to manage — the agent reads all of its runtime
    configuration (identity, NATS, allowed operations, logging, autoheal)
    from the config-drive the platform attaches to the VM.

.PARAMETER Version
    A specific release tag (e.g. "v2.2.0"). Defaults to the latest release.

.EXAMPLE
    # Install the latest release (run PowerShell as Administrator)
    irm https://raw.githubusercontent.com/plusclouds/vm.agent/master/scripts/install.windows.ps1 | iex

.EXAMPLE
    # Pin a specific release
    $script = irm https://raw.githubusercontent.com/plusclouds/vm.agent/master/scripts/install.windows.ps1
    & ([scriptblock]::Create($script)) -Version v2.2.0

.EXAMPLE
    # From a local checkout
    .\scripts\install.windows.ps1              # latest
    .\scripts\install.windows.ps1 -Version v2.2.0   # pinned
#>
[CmdletBinding()]
param(
    [string]$Version = "latest"
)

$ErrorActionPreference = "Stop"

$Repo        = "plusclouds/vm.agent"
$ServiceName = "PlusCloudsAgent"
$InstallDir  = Join-Path $env:ProgramFiles "PlusClouds"
$BinaryPath  = Join-Path $InstallDir "plusclouds-agent.exe"
$DataDir     = Join-Path $env:ProgramData "PlusClouds"
$LogDir      = Join-Path $DataDir "logs"
$CacheDir    = Join-Path $DataDir "cache"

function Log($msg)  { Write-Host "[install] $msg" }
function Fail($msg) { Write-Error "[install] ERROR: $msg"; exit 1 }

# --- pre-flight ----------------------------------------------------------------

$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Fail "must be run as Administrator (right-click PowerShell -> Run as Administrator)"
}

if ($env:PROCESSOR_ARCHITECTURE -ne "AMD64") {
    Fail "unsupported architecture: $env:PROCESSOR_ARCHITECTURE (only amd64 is published today)"
}

# Windows Server 2012 R2/2016 default to TLS 1.0/1.1 for outbound HTTPS in
# .NET, which GitHub rejects — force TLS 1.2 before any web request below.
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

# --- resolve version -------------------------------------------------------------

if ($Version -eq "latest") {
    Log "resolving latest release..."
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" `
        -Headers @{ "User-Agent" = "plusclouds-agent-installer" }
    $Version = $release.tag_name
    if (-not $Version) { Fail "could not resolve latest release version" }
}
Log "installing version $Version"

$ReleaseUrl = "https://github.com/$Repo/releases/download/$Version"

# --- stop existing service before replacing the binary ---------------------------

$existing = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($existing -and $existing.Status -eq "Running") {
    Log "stopping existing $ServiceName service..."
    Stop-Service -Name $ServiceName -Force
}

# --- directories -------------------------------------------------------------------

New-Item -ItemType Directory -Force -Path $InstallDir, $DataDir, $LogDir, $CacheDir | Out-Null

# --- download + install binary --------------------------------------------------------

$tmpBin = New-TemporaryFile
Log "downloading plusclouds.windows $Version..."
try {
    Invoke-WebRequest -Uri "$ReleaseUrl/plusclouds.windows" -OutFile $tmpBin -UseBasicParsing
} catch {
    Fail "download failed — check that $Version exists at https://github.com/$Repo/releases"
}
Log "sha256: $((Get-FileHash -Path $tmpBin -Algorithm SHA256).Hash)"

Copy-Item -Path $tmpBin -Destination $BinaryPath -Force
Remove-Item -Path $tmpBin -Force
Log "installed $BinaryPath"

# --- default log path -----------------------------------------------------------------

# Unlike systemd (StandardOutput=journal), a Windows service has no console
# to capture stdout — without somewhere to write, the agent's logs simply
# vanish. Set machine-wide environment variables (read by the agent's
# config loader, PLUSCLOUDS_AGENT_* prefix) so there's always a usable
# default; config-drive metadata can still override log.file/iso.cache_path
# if the platform sets them explicitly.
$logPath = Join-Path $LogDir "agent.log"
[Environment]::SetEnvironmentVariable("PLUSCLOUDS_AGENT_LOG_FILE", $logPath, "Machine")
[Environment]::SetEnvironmentVariable("PLUSCLOUDS_AGENT_ISO_CACHE_PATH", (Join-Path $CacheDir "pc-meta-data.json"), "Machine")

# --- register the service ---------------------------------------------------------------

# The binary path must be quoted for the SCM to parse it correctly — it
# lives under "Program Files", which contains a space.
$quotedBinPath = '"{0}"' -f $BinaryPath

if ($existing) {
    Log "service already registered, updating configuration..."
    sc.exe config $ServiceName binPath= $quotedBinPath start= auto | Out-Null
} else {
    Log "registering $ServiceName service..."
    New-Service -Name $ServiceName `
        -BinaryPathName $quotedBinPath `
        -DisplayName "PlusClouds VM Agent" `
        -Description "Manages this VM on behalf of the PlusClouds platform via NATS." `
        -StartupType Automatic | Out-Null
}

# Auto-restart on crash, mirroring the systemd unit's Restart=always/RestartSec=5.
sc.exe failure $ServiceName reset= 86400 actions= restart/5000/restart/5000/restart/5000 | Out-Null

# --- start -------------------------------------------------------------------------------

Log "starting $ServiceName..."
Start-Service -Name $ServiceName

Log ""
Log "Install complete. $ServiceName is set to start automatically on boot."
Log "  Get-Service $ServiceName"
Log "  Get-Content `"$logPath`" -Wait   (once the agent has logged something)"
Log ""
Log "The agent reads its identity and runtime config from the config-drive"
Log "the platform attaches to this VM — nothing to edit by hand. If this VM"
Log "has no config-drive attached (e.g. local testing), set"
Log "PLUSCLOUDS_AGENT_NATS_AGENT_UUID / _API_KEY as machine environment"
Log "variables ([Environment]::SetEnvironmentVariable(..., 'Machine')) and"
Log "restart the service."
