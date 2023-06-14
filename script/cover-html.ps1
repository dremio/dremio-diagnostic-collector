# script\cover.ps1: Run go tool cover and open the coverage report in a web browser

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Change working directory to script's grandparents directory
Set-Location -Path (Get-Item (Split-Path -Parent $MyInvocation.MyCommand.Definition)).Parent.FullName

Write-Output "Running go tool cover..."
go tool cover -html=covprofile
