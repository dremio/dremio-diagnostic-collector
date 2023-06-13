# script\cover.ps1: Run go tool cover and open the coverage report in a web browser

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

Write-Output "Running go tool cover..."
go tool cover -html=covprofile
