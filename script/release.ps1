# script\release.ps1: build binaries in all supported platforms and upload them with the gh client

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$scriptPath = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location -Path $scriptPath

Write-Output "Checking if gh is installed..."
Get-Date -Format "HH:mm:ss"

if (-not (Get-Command "gh" -ErrorAction SilentlyContinue)) {
    Write-Output "gh not found, installing..."
    Get-Date -Format "HH:mm:ss"
    Write-Output "Install gh and try again from https://github.com/cli/cli/releases"
    exit 1
}

$GIT_SHA = git rev-parse --short HEAD
$VERSION = $args[0]
$LDFLAGS = "-X github.com/dremio/dremio-diagnostic-collector/pkg/versions.GitSha=$GIT_SHA -X github.com/dremio/dremio-diagnostic-collector/pkg/versions.Version=$VERSION"

Write-Output "Cleaning bin folder..."
Get-Date -Format "HH:mm:ss"
.\script\clean

Write-Output "Building linux-amd64..."
Get-Date -Format "HH:mm:ss"
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -ldflags $LDFLAGS -o ./bin/ddc
cp ./default-ddc.yaml ./bin/ddc.yaml
Compress-Archive -Path ./bin/ddc, ./bin/ddc.yaml -DestinationPath ./bin/ddc-linux-amd64.zip

mkdir -p ./bin/linux
Move-Item -Path ./bin/ddc -Destination ./bin/linux/ddc
Move-Item -Path ./bin/ddc.yaml -Destination ./bin/ddc.yaml

Write-Output "Building linux-arm64..."
Get-Date -Format "HH:mm:ss"
$env:GOARCH = "arm64"
go build -ldflags $LDFLAGS -o ./bin/ddc
Compress-Archive -Path ./bin/ddc, ./bin/linux/ddc, ./bin/ddc.yaml -DestinationPath ./bin/ddc-linux-arm64.zip

Write-Output "Building darwin-os-x-amd64..."
Get-Date -Format "HH:mm:ss"
$env:GOOS = "darwin"
go build -ldflags $LDFLAGS -o ./bin/ddc
Compress-Archive -Path ./bin/ddc, ./bin/linux/ddc, ./bin/ddc.yaml -DestinationPath ./bin/ddc-darwin-amd64.zip

Write-Output "Building darwin-os-x-arm64..."
Get-Date -Format "HH:mm:ss"
$env:GOARCH = "arm64"
go build -ldflags $LDFLAGS -o ./bin/ddc
Compress-Archive -Path ./bin/ddc, ./bin/linux/ddc, ./bin/ddc.yaml -DestinationPath ./bin/ddc-darwin-arm64.zip

Write-Output "Building windows-amd64..."
Get-Date -Format "HH:mm:ss"
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go build -ldflags $LDFLAGS -o ./bin/ddc.exe
Compress-Archive -Path ./bin/ddc.exe, ./bin/linux/ddc, ./bin/ddc.yaml -DestinationPath ./bin/ddc-windows-amd64.zip

Write-Output "Creating GitHub release..."
gh release create $VERSION --title $VERSION -F changelog.md ./bin/ddc-windows-amd64.zip ./bin/ddc-darwin-arm64.zip ./bin/ddc-darwin-amd64.zip ./bin/ddc-linux-arm64.zip ./bin/ddc-linux-amd64.zip
