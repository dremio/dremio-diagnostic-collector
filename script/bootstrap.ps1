# script\bootstrap.ps1: Resolve all dependencies that the application requires to run.

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Change working directory to script's grandparents directory
Set-Location -Path (Get-Item (Split-Path -Parent $MyInvocation.MyCommand.Definition)).Parent.FullName

Write-Output "putting jar from ttop into lib dir.."
Get-Date -Format "HH:mm:ss"
mkdir -Force .\cmd\local\jvmcollect\lib
Invoke-WebRequest https://github.com/rsvihladremio/jvm-tools/releases/download/0.22-SNAPSHOT/sjk-0.22-SNAPSHOT.jar  -OutFile .\cmd\local\jvmcollect\lib\sjk.jar 

Write-Output "Checking if license-header-checker is installed..."
Get-Date -Format "HH:mm:ss"

if (-not (Get-Command "license-header-checker" -ErrorAction SilentlyContinue)) {
    Write-Output "license-header-checker not found, installing..."
    Get-Date -Format "HH:mm:ss"
    go install github.com/lluissm/license-header-checker/cmd/license-header-checker@latest
}

Write-Output "Checking if golangci-lint is installed..."
Get-Date -Format "HH:mm:ss"

if (-not (Get-Command "golangci-lint" -ErrorAction SilentlyContinue)) {
    Write-Output "golangci-lint not found, installing..."
    Get-Date -Format "HH:mm:ss"
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.55.2
}

Write-Output "Checking if gosec is installed..."
Get-Date -Format "HH:mm:ss"

if (-not (Get-Command "gosec" -ErrorAction SilentlyContinue)) {
    Write-Output "gosec not found, installing..."
    Get-Date -Format "HH:mm:ss"
    go install github.com/securego/gosec/v2/cmd/gosec@v2.16.0
}
