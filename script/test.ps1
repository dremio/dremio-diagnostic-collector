# script/test.ps1: Run test suite for application.

$ErrorActionPreference = "Stop"

Set-Location -Path (Get-Item (Split-Path -Parent $MyInvocation.MyCommand.Definition)).Parent.FullName


if ($env:DEBUG) {
    $DebugPreference = "Continue"
}

.\script\clean.ps1
.\script\build.ps1


go test ./...