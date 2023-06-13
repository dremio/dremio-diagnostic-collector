# script\audit.ps1: runs gosec against the mod file to find security issues

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$scriptPath = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location -Path $scriptPath

Write-Output "Running gosec..."
Invoke-Expression "gosec -exclude=G204,G402 ./..."
