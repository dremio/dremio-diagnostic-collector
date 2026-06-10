# Script to build binaries in all supported platforms

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Change working directory to script's grandparents directory
Set-Location -Path (Get-Item (Split-Path -Parent $MyInvocation.MyCommand.Definition)).Parent.FullName

# Get Git SHA and Version
$GIT_SHA = git rev-parse --short HEAD
$VERSION = $args[0]
$LDFLAGS = "-s -w -X github.com/dremio/dremio-diagnostic-collector/v4/pkg/versions.GitSha=$GIT_SHA -X github.com/dremio/dremio-diagnostic-collector/v4/pkg/versions.Version=$VERSION"

Write-Output "Cleaning bin folder"
Get-Date -Format "HH:mm:ss"
.\script\clean

Write-Output "Building linux-amd64"
Get-Date -Format "HH:mm:ss"
$env:GOOS="linux"
$env:GOARCH="amd64"
go build -trimpath -ldflags "$LDFLAGS" -o ./bin/ddc
if (Get-Command "upx" -ErrorAction SilentlyContinue) { upx --best ./bin/ddc }
Compress-Archive -Path ./bin/ddc, ./README.md, ./FAQ.md -DestinationPath ./bin/ddc-linux-amd64.zip

Write-Output "Building linux-arm64"
Get-Date -Format "HH:mm:ss"
$env:GOARCH="arm64"
go build -trimpath -ldflags "$LDFLAGS" -o ./bin/ddc
if (Get-Command "upx" -ErrorAction SilentlyContinue) { upx --best ./bin/ddc }
Compress-Archive -Path ./bin/ddc, ./README.md, ./FAQ.md -DestinationPath ./bin/ddc-linux-arm64.zip

# UPX on macOS can cause Gatekeeper/code-signing issues — skip for darwin
Write-Output "Building darwin-os-x-amd64"
Get-Date -Format "HH:mm:ss"
$env:GOOS="darwin"
$env:GOARCH="amd64"
go build -trimpath -ldflags "$LDFLAGS" -o ./bin/ddc
Compress-Archive -Path ./bin/ddc, ./README.md, ./FAQ.md -DestinationPath ./bin/ddc-mac-intel.zip

Write-Output "Building darwin-os-x-arm64"
Get-Date -Format "HH:mm:ss"
$env:GOARCH="arm64"
go build -trimpath -ldflags "$LDFLAGS" -o ./bin/ddc
Compress-Archive -Path ./bin/ddc, ./README.md, ./FAQ.md -DestinationPath ./bin/ddc-mac-m-series.zip

Write-Output "Building windows-amd64"
Get-Date -Format "HH:mm:ss"
$env:GOOS="windows"
$env:GOARCH="amd64"
go build -trimpath -ldflags "$LDFLAGS" -o ./bin/ddc.exe
if (Get-Command "upx" -ErrorAction SilentlyContinue) { upx --best ./bin/ddc.exe }
Compress-Archive -Path ./bin/ddc.exe, ./README.md, ./FAQ.md -DestinationPath ./bin/ddc-windows-amd64.zip

Write-Output "Building windows-arm64"
Get-Date -Format "HH:mm:ss"
$env:GOOS="windows"
$env:GOARCH="arm64"
go build -trimpath -ldflags "$LDFLAGS" -o ./bin/ddc.exe
# upx does not support win64/arm64 — ship uncompressed.
Compress-Archive -Path ./bin/ddc.exe, ./README.md, ./FAQ.md -DestinationPath ./bin/ddc-windows-arm64.zip
