# Builds the Miyabi desktop app (Tauri v2 shell + Go sidecar).
# Requires: Go, Node/pnpm, and a Rust toolchain (rustc) on this machine.
# The same steps run in CI (.github/workflows/desktop-tauri.yml).
$ErrorActionPreference = "Stop"
Set-Location (Resolve-Path (Join-Path $PSScriptRoot ".."))

# 1. Frontend — embedded into the Go binary via //go:embed web/dist.
pnpm --dir web install --frozen-lockfile
pnpm --dir web build

# 2. Go sidecar — named with the Rust target triple so Tauri finds it.
$triple = (& rustc -vV | Select-String '^host:').Line.Split(' ')[1].Trim()
New-Item -ItemType Directory -Force -Path "src-tauri/binaries" | Out-Null
go build -ldflags "-s -w -H=windowsgui" -o "src-tauri/binaries/miyabi-server-$triple.exe" ./cmd

# 3. Icons — generated from a scratch PNG, no binary assets committed.
node scripts/gen-icon.mjs src-tauri/app-icon.png 1024
pnpm dlx @tauri-apps/cli@^2 icon src-tauri/app-icon.png

# 4. Package (NSIS installer).
pnpm dlx @tauri-apps/cli@^2 build

# 5. Assemble installer + portable zip (same layout as CI).
# The release binary keeps the Cargo package name. productName only names the installer.
$shell = "src-tauri/target/release/miyabi-desktop.exe"
if (-not (Test-Path $shell)) { throw "desktop shell was not produced: $shell" }
$nsis = Get-ChildItem "src-tauri/target/release/bundle/nsis/*.exe" | Select-Object -First 1
if (-not $nsis) { throw 'the NSIS installer was not produced' }

$version = (Get-Content web/package.json -Raw | ConvertFrom-Json).version
$package = "miyabi-desktop-$version-windows-x86_64"
$stage = "dist/$package"
if (Test-Path $stage) { Remove-Item -Recurse -Force $stage }
New-Item -ItemType Directory -Force -Path $stage | Out-Null
Copy-Item $shell (Join-Path $stage 'Miyabi.exe')
Copy-Item "src-tauri/binaries/miyabi-server-$triple.exe" (Join-Path $stage 'miyabi-server.exe')
Copy-Item LICENSE $stage
Copy-Item docs/desktop-usage.md (Join-Path $stage '使用说明.md')
New-Item -ItemType File -Force -Path (Join-Path $stage 'portable') | Out-Null
Compress-Archive -Path (Join-Path $stage '*') -DestinationPath "dist/$package.zip" -Force
Copy-Item $nsis.FullName "dist/miyabi-desktop-$version-setup.exe" -Force
Write-Host "artifacts written to dist/"
