# script/fix-license.ps1: Add license header to files missing it

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Change working directory to script's grandparents directory
Set-Location -Path (Get-Item (Split-Path -Parent $MyInvocation.MyCommand.Definition)).Parent.FullName

Write-Output "Executing license-header-check add"
license-header-checker -a license_header.txt . go
