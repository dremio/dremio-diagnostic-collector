# script\test.ps1: Run test suite for application.

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Change working directory to script's grandparents directory
Set-Location -Path (Get-Item (Split-Path -Parent $MyInvocation.MyCommand.Definition)).Parent.FullName

if ($env:DEBUG) {
    $DebugPreference = "Continue"
}

go test -covermode atomic -coverprofile=covprofile ./...