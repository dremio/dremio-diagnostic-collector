# script\update.ps1: Update application to run for its current checkout.

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$scriptPath = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location -Path $scriptPath

Write-Output "==> Running bootstrap..."

.\script\bootstrap

Write-Output "==> Cleaning bin folder..."

.\script\clean