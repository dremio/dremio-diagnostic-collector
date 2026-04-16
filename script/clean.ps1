# script\clean.ps1: Remove bin folder

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Change working directory to script's grandparents directory
Set-Location -Path (Get-Item (Split-Path -Parent $MyInvocation.MyCommand.Definition)).Parent.FullName

Write-Output "Removing bin folder..."

if (Test-Path .\bin\) {
    Remove-Item -Path .\bin\* -Recurse -Force
}

