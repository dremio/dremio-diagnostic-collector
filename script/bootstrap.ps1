# script\bootstrap.ps1: Resolve all dependencies that the application requires to run.

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Change working directory to script's grandparents directory
Set-Location -Path (Get-Item (Split-Path -Parent $MyInvocation.MyCommand.Definition)).Parent.FullName

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
    $previousToolchain = $env:GOTOOLCHAIN
    $env:GOTOOLCHAIN = 'go1.27.4'
    try {
        go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
    } finally {
        $env:GOTOOLCHAIN = $previousToolchain
    }
}

Write-Output "Checking if gosec is installed..."
Get-Date -Format "HH:mm:ss"

if (-not (Get-Command "gosec" -ErrorAction SilentlyContinue)) {
    Write-Output "gosec not found, installing..."
    Get-Date -Format "HH:mm:ss"
    $previousToolchain = $env:GOTOOLCHAIN
    $env:GOTOOLCHAIN = 'go1.27.4'
    try {
        go install github.com/securego/gosec/v2/cmd/gosec@v2.30.0
    } finally {
        $env:GOTOOLCHAIN = $previousToolchain
    }
}

