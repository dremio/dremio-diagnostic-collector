# script\update.ps1: Update application to run for its current checkout.

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Change working directory to script's grandparents directory
Set-Location -Path (Get-Item (Split-Path -Parent $MyInvocation.MyCommand.Definition)).Parent.FullName

Write-Output "==> Running bootstrap..."

.\script\bootstrap

Write-Output "==> Cleaning bin folder..."

.\script\clean