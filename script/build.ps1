# script\build.ps1: Script to build the binary

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Change working directory to script's grandparents directory
Set-Location -Path (Get-Item (Split-Path -Parent $MyInvocation.MyCommand.Definition)).Parent.FullName

.\script\clean.ps1

# Get Git SHA and Version
$GIT_SHA = git rev-parse --short HEAD
$VERSION = git rev-parse --abbrev-ref HEAD
$LDFLAGS = "-s -w -X github.com/dremio/dremio-diagnostic-collector/v4/pkg/versions.GitSha=$GIT_SHA -X github.com/dremio/dremio-diagnostic-collector/v4/pkg/versions.Version=$VERSION"

$env:GOOS="windows"
$env:GOARCH="amd64"
# Build main binary (default-ddc.yaml removed in v4 — all config via CLI flags)
go build -trimpath -ldflags "$LDFLAGS" -o .\bin\ddc.exe
if (Get-Command "upx" -ErrorAction SilentlyContinue) { upx --best .\bin\ddc.exe }
