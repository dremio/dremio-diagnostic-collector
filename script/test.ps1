# script/test.ps1: Run test suite for application.

$ErrorActionPreference = "Stop"

$scriptPath = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location -Path $scriptPath

if ($env:DEBUG) {
    $DebugPreference = "Continue"
}

.\script\clean.ps1
.\script\build.ps1

go test -race -coverpkg=./... -coverprofile=covprofile ./...