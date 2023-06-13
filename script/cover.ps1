# script\cover.ps1: Run the coverage

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$scriptPath = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location -Path $scriptPath

if ($env:DEBUG) {
    $DebugPreference = "Continue"
}

go tool cover -func=covprofile