# Builds the Miyabi desktop app (Tauri v2 shell + Go server).
# Requires: Go, Node/pnpm, and a Rust toolchain (rustc) on this machine.
# The same steps run in CI (.github/workflows/desktop-tauri.yml).
$ErrorActionPreference = "Stop"
Set-Location (Resolve-Path (Join-Path $PSScriptRoot ".."))

# 1. Frontend — embedded into the Go binary via //go:embed web/dist.
pnpm --dir web install --frozen-lockfile
pnpm --dir web build

# 2. Go server — bundled as a resource so the installer nests it under runtime/.
New-Item -ItemType Directory -Force -Path "src-tauri/runtime" | Out-Null
go build -ldflags "-s -w -H=windowsgui" -o "src-tauri/runtime/miyabi-server.exe" ./cmd

# 3. Package (NSIS installer), then a second shell build with the portable
#    identity so an installed copy and a portable folder can run side by side.
pnpm dlx @tauri-apps/cli@^2 build
pnpm dlx @tauri-apps/cli@^2 build --no-bundle --config src-tauri/tauri.portable.conf.json

# 4. Assemble installer + portable zip (same layout as CI).
# `mainBinaryName = miyabi`: tauri-cli renames the shell binary, so both the
# installer and the portable zip ship miyabi.exe.
$shell = "src-tauri/target/release/miyabi.exe"
if (-not (Test-Path $shell)) { throw "desktop shell was not produced: $shell" }
$nsis = Get-ChildItem "src-tauri/target/release/bundle/nsis/*.exe" | Select-Object -First 1
if (-not $nsis) { throw 'the NSIS installer was not produced' }

$version = (Get-Content web/package.json -Raw | ConvertFrom-Json).version
$package = "miyabi-$version-windows-x86_64-tauri"
$stage = "dist/$package"
if (Test-Path $stage) { Remove-Item -Recurse -Force $stage }
New-Item -ItemType Directory -Force -Path $stage | Out-Null
Copy-Item $shell (Join-Path $stage 'miyabi.exe')
New-Item -ItemType Directory -Force -Path (Join-Path $stage 'runtime') | Out-Null
Copy-Item "src-tauri/runtime/miyabi-server.exe" (Join-Path $stage 'runtime/miyabi-server.exe')
# Only the portable folder carries the marker. The installer writes none.
New-Item -ItemType File -Force -Path (Join-Path $stage 'runtime/portable') | Out-Null
Copy-Item LICENSE $stage
Copy-Item docs/desktop-usage.md (Join-Path $stage '使用说明.md')
Compress-Archive -Path (Join-Path $stage '*') -DestinationPath "dist/$package-portable.zip" -Force
Copy-Item $nsis.FullName "dist/$package-setup.exe" -Force
Write-Host "artifacts written to dist/"
