# script/test.ps1: Run test suite for application.

$ErrorActionPreference = "Stop"

$scriptPath = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location -Path $scriptPath

if ($env:DEBUG) {
    $DebugPreference = "Continue"
}

.\script\clean.ps1
.\script\build.ps1

$pkgs = Get-ChildItem -Recurse | Where-Object { $_.PSIsContainer -and $_.FullName -notmatch '\\vendor\\' } | ForEach-Object { $_.FullName }
$deps = $pkgs -join ','

"mode: atomic" | Out-File -FilePath covprofile

foreach ($pkg in $pkgs) {
    try {
        go test -race -cover -coverpkg "$deps" -coverprofile=profile.tmp $pkg
    }
    catch {
        Write-Host "Error occurred while running tests for package: $pkg"
        continue
    }

    if (Test-Path -Path "profile.tmp") {
        Get-Content -Path "profile.tmp" | Select-Object -Skip 1 | Add-Content -Path covprofile
        Remove-Item -Path "profile.tmp" -Force
    }
}