# Builds the Go server that the Tauri shell launches from runtime/ and checks that
# it really embeds the frontend.
#
# A `-tags dev` build serves no frontend: every UI path answers with a 404 notice,
# which makes a shipped installer look like a routing defect. Building through
# this script keeps the bundled server in sync with web/dist, and the check below
# turns that mistake into a build error.
#
# Used by scripts/build-desktop.ps1, the desktop workflow, and the Tauri
# `beforeBuildCommand` hook.
[CmdletBinding()]
param(
    # Reinstall and rebuild web/dist even when it already exists. The release
    # build passes this; an on-demand Tauri build only fills in a missing dist.
    [switch] $RebuildFrontend
)

$ErrorActionPreference = 'Stop'
Set-Location (Resolve-Path (Join-Path $PSScriptRoot '..'))

# web/dist is embedded by //go:embed, so it must exist before `go build`.
if ($RebuildFrontend) {
    Write-Host 'Rebuilding the web frontend...'
    pnpm --dir web install --frozen-lockfile
    if ($LASTEXITCODE -ne 0) { throw 'pnpm install failed' }
    pnpm --dir web build
    if ($LASTEXITCODE -ne 0) { throw 'pnpm --dir web build failed' }
} elseif (-not (Test-Path 'web/dist/index.html')) {
    Write-Host 'Building the web frontend; web/dist is embedded into the server...'
    pnpm --dir web build
    if ($LASTEXITCODE -ne 0) { throw 'pnpm --dir web build failed' }
}
if (-not (Test-Path 'web/dist/index.html')) {
    throw 'web/dist/index.html is missing. Run pnpm --dir web build first.'
}

$server = Join-Path (Get-Location) 'src-tauri/runtime/miyabi-server.exe'
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $server) | Out-Null

# modernc.org/sqlite is pure Go, so the bundled server needs no C toolchain.
$env:CGO_ENABLED = '0'
Write-Host "Building $server ..."
go build -trimpath -ldflags '-s -w -H=windowsgui' -o $server ./cmd
if ($LASTEXITCODE -ne 0) { throw 'go build ./cmd failed' }

# `-tags dev` swaps embed.go for a stub that returns no filesystem, so reject it
# before the binary reaches an installer.
$buildInfo = (go version -m $server | Out-String)
if ($buildInfo -notmatch '(?m)^\s*path\s') {
    throw "cannot read the build settings of $server; rebuild it with go build ./cmd"
}
if ($buildInfo -match 'build\s+-tags=[^\r\n]*\bdev\b') {
    throw 'the server was built with -tags dev, so it embeds no frontend. Rebuild it without dev tags.'
}

Write-Host 'server built with the frontend embedded'
