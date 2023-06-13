# script\build.ps1: Build binary
Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$scriptPath = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location -Path $scriptPath

$GIT_SHA = (git rev-parse --short HEAD)
$VERSION = (git rev-parse --abbrev-ref HEAD)
$LDFLAGS = "-X github.com/dremio/dremio-diagnostic-collector/pkg/versions/cmd.GitSha=$GIT_SHA -X github.com/dremio/dremio-diagnostic-collector/pkg/versions.Version=$VERSION"

go build -ldflags $LDFLAGS -o .\bin\ddc

$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -ldflags $LDFLAGS -o .\bin\linux\ddc

Copy-Item -Path .\default-ddc.yaml -Destination .\bin\ddc.yaml