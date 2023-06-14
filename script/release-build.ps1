# Script to build binaries in all supported platforms

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Change working directory to script's grandparents directory
Set-Location -Path (Get-Item (Split-Path -Parent $MyInvocation.MyCommand.Definition)).Parent.FullName

# Get Git SHA and Version
$GIT_SHA = git rev-parse --short HEAD
$VERSION = $args[0]
$LDFLAGS = "-X github.com/dremio/dremio-diagnostic-collector/pkg/versions.GitSha=$GIT_SHA -X github.com/dremio/dremio-diagnostic-collector/pkg/versions.Version=$VERSION"

Write-Output "Cleaning bin folder…"
Get-Date -Format "HH:mm:ss"
.\script\clean

Write-Output "Building linux-amd64…"
Get-Date -Format "HH:mm:ss"
$env:GOOS="linux"
$env:GOARCH="amd64"
go build -ldflags "$LDFLAGS" -o ./bin/ddc
Copy-Item -Path ./default-ddc.yaml -Destination ./bin/ddc.yaml
Compress-Archive -Path ./bin/ddc, ./bin/ddc.yaml -DestinationPath ./bin/ddc-linux-amd64.zip
Compress-Archive -Path ./bin/ddc -DestinationPath ./bin/ddc.zip
Move-Item -Path ./bin/ddc.zip -Destination ./cmd/root/ddcbinary/output/ddc.zip
Move-Item -Path ./bin/ddc.yaml -Destination ./bin/ddc.yaml

Write-Output "Building linux-arm64…"
Get-Date -Format "HH:mm:ss"
$env:GOARCH="arm64"
go build -ldflags "$LDFLAGS" -o ./bin/ddc
Compress-Archive -Path ./bin/ddc, ./bin/ddc.yaml -DestinationPath ./bin/ddc-linux-arm64.zip

Write-Output "Building darwin-os-x-amd64…"
Get-Date -Format "HH:mm:ss"
$env:GOOS="darwin"
$env:GOARCH="amd64"
go build -ldflags "$LDFLAGS" -o ./bin/ddc
Compress-Archive -Path ./bin/ddc, ./bin/ddc.yaml -DestinationPath ./bin/ddc-darwin-amd64.zip

Write-Output "Building darwin-os-x-arm64…"
Get-Date -Format "HH:mm:ss"
$env:GOARCH="arm64"
go build -ldflags "$LDFLAGS" -o ./bin/ddc
Compress-Archive -Path ./bin/ddc, ./bin/ddc.yaml -DestinationPath ./bin/ddc-darwin-arm64.zip

Write-Output "Building windows-amd64…"
Get-Date -Format "HH:mm:ss"
$env:GOOS="windows"
$env:GOARCH="amd64"
go build -ldflags "$LDFLAGS" -o ./bin/ddc.exe
Compress-Archive -Path ./bin/ddc.exe, ./bin/ddc.yaml -DestinationPath ./bin/ddc-windows-amd64.zip
