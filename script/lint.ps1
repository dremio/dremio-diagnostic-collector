# script/lint.ps1: Run gofmt and golangci-lint run

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$scriptPath = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location -Path $scriptPath

Write-Output "Running gofmt..."
go fmt ./...

Write-Output "Executing golangci-lint run"
golangci-lint run -E exportloopref,revive,gofmt -D structcheck