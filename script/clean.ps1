# script\clean.ps1: Remove bin folder

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$scriptPath = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location -Path $scriptPath

Write-Output "Removing bin folder..."
Remove-Item -Path ./bin -Recurse -Force