# script\build.ps1: Script to build the binary

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Change working directory to script's grandparents directory
Set-Location -Path (Get-Item (Split-Path -Parent $MyInvocation.MyCommand.Definition)).Parent.FullName

.\script\clean.ps1

# Get Git SHA and Version
$GIT_SHA = git rev-parse --short HEAD
$VERSION = git rev-parse --abbrev-ref HEAD
$LDFLAGS = "-X github.com/dremio/dremio-diagnostic-collector/v3/pkg/versions.GitSha=$GIT_SHA -X github.com/dremio/dremio-diagnostic-collector/v3/pkg/versions.Version=$VERSION"

New-Item -ItemType File -Path .\cmd\root\ddcbinary\output\ddc.zip -Force
# This assumes that you have 'go' installed in your environment
$env:GOOS="linux"
$env:GOARCH="amd64"
go build -ldflags "$LDFLAGS" -o .\bin\ddc

# Use Compress-Archive to create zip file and then move it
Compress-Archive -Path .\bin\ddc -DestinationPath .\bin\ddc.zip
Move-Item -Force -Path  .\bin\ddc.zip -Destination .\cmd\root\ddcbinary\output\ddc.zip
Remove-Item -Path .\bin\ddc

$env:GOOS="windows"
$env:GOARCH="amd64"
# Build again and copy default-ddc.yaml
go build -ldflags "$LDFLAGS" -o .\bin\ddc.exe
Copy-Item -Path .\default-ddc.yaml -Destination .\bin\ddc.yaml
