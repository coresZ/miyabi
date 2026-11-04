# Windows-only local launcher: builds the frontend/sidecar when needed, then
# builds and starts the Tauri shell in debug mode. Release/installer builds stay
# in scripts/build-desktop.ps1.
#
# Usage:
#   powershell -NoProfile -ExecutionPolicy Bypass -File scripts/run-desktop.ps1 [-Force]
#   scripts\run-desktop.bat [-Force]   (double-click friendly)
#
# -Force rebuilds web/dist even when it already exists.

#requires -Version 5.1

[CmdletBinding()]
param(
    [switch] $Force
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

function Stop-WithError([string] $Message) {
    Write-Host "ERROR: $Message" -ForegroundColor Red
    exit 1
}

# Resolve a tool from PATH first, then from explicit fallback paths.
function Resolve-Tool {
    param(
        [string] $Name,
        [string[]] $Fallbacks = @()
    )
    $command = Get-Command $Name -ErrorAction SilentlyContinue
    if ($command -and $command.Source) {
        return $command.Source
    }
    foreach ($candidate in $Fallbacks) {
        if ($candidate -and (Test-Path -LiteralPath $candidate)) {
            return $candidate
        }
    }
    return $null
}

Write-Host '== Miyabi desktop (debug) =='

$cargoFallbacks = @()
if ($env:CARGO_HOME) { $cargoFallbacks += Join-Path $env:CARGO_HOME 'bin\cargo.exe' }
if ($env:USERPROFILE) { $cargoFallbacks += Join-Path $env:USERPROFILE '.cargo\bin\cargo.exe' }
$cargoExe = Resolve-Tool 'cargo' $cargoFallbacks
if (-not $cargoExe) {
    Stop-WithError 'cargo not found. Install Rust (https://rustup.rs) or add .cargo\bin to PATH.'
}

$cargoDir = Split-Path -Parent $cargoExe
if ($cargoDir -and -not (($env:Path -split ';') -contains $cargoDir)) {
    $env:Path = "$cargoDir;$env:Path"
}

$rustcExe = $null
if ($cargoDir) { $rustcExe = Join-Path $cargoDir 'rustc.exe' }
if (-not ($rustcExe -and (Test-Path -LiteralPath $rustcExe))) {
    $rustc = Get-Command rustc -ErrorAction SilentlyContinue
    if ($rustc) { $rustcExe = $rustc.Source }
}
$hostTriple = ''
if ($rustcExe -and (Test-Path -LiteralPath $rustcExe)) {
    $hostLine = (& $rustcExe -vV) | Where-Object { $_ -like 'host:*' } | Select-Object -First 1
    if ($hostLine) { $hostTriple = $hostLine.Substring(5).Trim() }
}

$goFallbacks = @()
if ($env:GOROOT) { $goFallbacks += Join-Path $env:GOROOT 'bin\go.exe' }
if ($env:ProgramFiles) { $goFallbacks += Join-Path $env:ProgramFiles 'Go\bin\go.exe' }
$goExe = Resolve-Tool 'go' $goFallbacks
if (-not $goExe) {
    Stop-WithError 'go not found. Install Go 1.27+ or add it to PATH.'
}

if (Get-Process -Name miyabi-desktop -ErrorAction SilentlyContinue) {
    Stop-WithError 'The desktop shell is already running. Close it before rebuilding.'
}

if ($env:CARGO_BUILD_TARGET) {
    Stop-WithError 'CARGO_BUILD_TARGET is set; this launcher expects a host debug build. Unset it and retry.'
}

Write-Host "==> repo: $root"
Write-Host "==> cargo: $(& $cargoExe -V)"
if ($hostTriple) { Write-Host "==> rustc host: $hostTriple" }
Write-Host "==> go: $(& $goExe version)"

if ($hostTriple -and $hostTriple -notlike '*-windows-msvc') {
    Write-Warning "rustc host is $hostTriple; the Windows desktop shell expects a *-windows-msvc toolchain."
}

$manifest = Join-Path $root 'src-tauri\Cargo.toml'
$metadataText = (& $cargoExe metadata --no-deps --format-version 1 --manifest-path $manifest 2>$null) -join "`n"
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($metadataText)) {
    Stop-WithError 'cargo metadata failed. Check src-tauri\Cargo.toml and the Rust toolchain.'
}
try {
    $metadata = $metadataText | ConvertFrom-Json
} catch {
    Stop-WithError "cargo metadata returned invalid JSON: $($_.Exception.Message)"
}
$targetDir = $metadata.target_directory
if (-not $targetDir) {
    Stop-WithError 'cargo metadata did not return target_directory.'
}
$debugRuntime = Join-Path $targetDir 'debug\runtime'
Write-Host "==> target dir: $targetDir"

# Some sandboxes deny %TEMP% writes to children of Rust build scripts. Keeping
# compiler temporaries under the target dir avoids the cl.exe C1083 failure and
# is harmless on a normal machine.
$clTemp = Join-Path $targetDir 'cl-temp'
New-Item -ItemType Directory -Force -Path $clTemp | Out-Null
$env:TEMP = $clTemp
$env:TMP = $clTemp
Write-Host "==> compiler temp: $clTemp"

# Keep debug WebView2 data separate from installed/portable builds. Sharing the
# default folder makes WebView2 creation fail with ERROR_BUSY when another
# instance holds it.
if (-not $env:WEBVIEW2_USER_DATA_FOLDER) {
    $webviewData = Join-Path $targetDir 'webview2-debug'
    New-Item -ItemType Directory -Force -Path $webviewData | Out-Null
    $env:WEBVIEW2_USER_DATA_FOLDER = $webviewData
}
Write-Host "==> webview2 data: $env:WEBVIEW2_USER_DATA_FOLDER"

$dist = Join-Path $root 'web\dist\index.html'
$needsFrontend = $Force.IsPresent -or -not (Test-Path -LiteralPath $dist)
if ($needsFrontend) {
    $node = Resolve-Tool 'node'
    $pnpm = Resolve-Tool 'pnpm'
    if (-not $node) { Stop-WithError 'node not found. Install Node 22+ and pnpm.' }
    if (-not $pnpm) {
        Stop-WithError 'pnpm not found. Install pnpm (for example: corepack enable) or add it to PATH.'
    }
    Write-Host "==> node: $(& $node -v)"
    Write-Host "==> pnpm: $(& $pnpm -v)"
    Write-Host '==> building the frontend'
    & $pnpm --dir web install --frozen-lockfile
    if ($LASTEXITCODE -ne 0) { Stop-WithError 'pnpm install failed. Check web\pnpm-lock.yaml and the Node version.' }
    & $pnpm --dir web build
    if ($LASTEXITCODE -ne 0) { Stop-WithError 'pnpm build failed.' }
} else {
    Write-Host '==> web/dist exists; skipping the frontend build (use -Force after UI changes)'
}

$runtimeDir = Join-Path $root 'src-tauri\runtime'
New-Item -ItemType Directory -Force -Path $runtimeDir, $debugRuntime | Out-Null
# Debug runs keep prefs and data under target\debug instead of %APPDATA%, which
# avoids permission problems in restricted sessions.
New-Item -ItemType File -Force -Path (Join-Path $debugRuntime 'portable') | Out-Null
$server = Join-Path $runtimeDir 'miyabi-server.exe'

Write-Host '==> building the Go server'
$env:CGO_ENABLED = '0'
& $goExe build -trimpath -ldflags '-s -w -H=windowsgui' -o $server ./cmd
if ($LASTEXITCODE -ne 0) {
    Stop-WithError 'go build failed. Check go.mod, the Go version, and the ./cmd package.'
}

# sidecar.rs resolves runtime\miyabi-server.exe relative to the shell binary.
try {
    Copy-Item -LiteralPath $server -Destination (Join-Path $debugRuntime 'miyabi-server.exe') -Force
} catch {
    Stop-WithError "cannot update the debug sidecar; close any running debug instance. $($_.Exception.Message)"
}

$lockArgs = @()
if (Test-Path -LiteralPath (Join-Path $root 'src-tauri\Cargo.lock')) {
    $lockArgs = @('--locked')
}

Write-Host '==> building the Tauri shell (debug)'
& $cargoExe build --manifest-path $manifest @lockArgs
if ($LASTEXITCODE -ne 0) {
    Stop-WithError 'cargo build failed. Check the MSVC C++ build tools; if cl.exe reports C1083 on a temp file, set TEMP/TMP to a writable directory.'
}

Write-Host '==> launching the Tauri shell (keep this window open while the app runs)'
& $cargoExe run --manifest-path $manifest @lockArgs
exit $LASTEXITCODE
