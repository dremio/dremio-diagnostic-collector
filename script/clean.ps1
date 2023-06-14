# script\clean.ps1: Remove bin folder

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Change working directory to script's grandparents directory
Set-Location -Path (Get-Item (Split-Path -Parent $MyInvocation.MyCommand.Definition)).Parent.FullName

Write-Output "Removing bin folder and .\cmd\root\ddcbinary\output folder contents..."
Remove-Item -Path .\cmd\root\ddcbinary\output\* -Recurse -Force

Remove-Item -Path .\bin -Recurse -Force
