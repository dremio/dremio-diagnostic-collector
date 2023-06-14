# script\cover.ps1: Run the coverage

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Change working directory to script's grandparents directory
Set-Location -Path (Get-Item (Split-Path -Parent $MyInvocation.MyCommand.Definition)).Parent.FullName

if ($env:DEBUG) {
    $DebugPreference = "Continue"
}

go tool cover -func=covprofile