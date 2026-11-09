# go_legacy build script
# Usage: .\build.ps1          (build only)
#        .\build.ps1 -Run     (build and run)

param([switch]$Run)

# Ensure bin directory exists
if (!(Test-Path "bin")) { New-Item -ItemType Directory -Path "bin" | Out-Null }

Write-Host "[build] compiling go_legacy..." -ForegroundColor Cyan
go build -o bin\go_legacy.exe .

if ($LASTEXITCODE -ne 0) {
    Write-Host "[build] FAILED" -ForegroundColor Red
    exit 1
}

Write-Host "[build] OK - bin\go_legacy.exe" -ForegroundColor Green
Write-Host "[build] All DLLs are embedded in the executable" -ForegroundColor Green

if ($Run) {
    Write-Host "[run] starting go_legacy..." -ForegroundColor Yellow
    .\bin\go_legacy.exe @args
}
