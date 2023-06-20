# Script to build binaries in all supported platforms and upload them with the gh client

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Change working directory to script's grandparents directory
Set-Location -Path (Get-Item (Split-Path -Parent $MyInvocation.MyCommand.Definition)).Parent.FullName

# Get Version
$VERSION = $args[0]

# Check if gh is installed
if (-Not (Get-Command "gh" -ErrorAction SilentlyContinue)) {
    Write-Output "gh not found. Please install gh and try again https://github.com/cli/cli/releases"
    Exit 1
}
# Get Git SHA and Version
$GIT_SHA = git rev-parse --short HEAD
$VERSION = $args[0]

Write-Output "Running release-build script…"
Get-Date -Format "HH:mm:ss"
.\script\release-build.ps1 $VERSION

# Run gh release command
gh release create $VERSION --title $VERSION -F changelog.md ./bin/ddc-windows-amd64.zip ./bin/ddc-mac-m-series.zip ./bin/ddc-mac-intel.zip ./bin/ddc-linux-arm64.zip ./bin/ddc-linux-amd64.zip
