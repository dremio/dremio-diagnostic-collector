# script\test.ps1: Run test suite for application.

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$scriptPath = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location -Path $scriptPath

if ($env:DEBUG) {
    $DebugPreference = "Continue"
}

go test -tags '!windows' -race -covermode atomic -coverprofile=covprofile ./...