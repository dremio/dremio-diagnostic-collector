# script/lint.ps1: Run gofmt and golangci-lint run

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Change working directory to script's grandparents directory
Set-Location -Path (Get-Item (Split-Path -Parent $MyInvocation.MyCommand.Definition)).Parent.FullName

Write-Output "Running gofmt..."
go fmt ./...

Write-Output "Executing golangci-lint run"
golangci-lint run -E exportloopref,revive,gofmt -D structcheck

Write-Output "executing license-header-checker"
license-header-checker license_header.txt . go
