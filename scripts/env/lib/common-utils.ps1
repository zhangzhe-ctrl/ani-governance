<#
.SYNOPSIS
Common utility function library
.DESCRIPTION
Provides common functions such as logging and error handling
.NOTES
Encoding: UTF-8 (NO BOM) | Compatible: PowerShell 5.1+
#>

function Log {
    param([string]$Message)
    Write-Host "==> $Message" -ForegroundColor Cyan
}

function Warn {
    param([string]$Message)
    Write-Host "[WARN] $Message" -ForegroundColor Yellow
}

function ErrorLog {
    param([string]$Message)
    Write-Host "[ERROR] $Message" -ForegroundColor Red
}

function SuccessLog {
    param([string]$Message)
    Write-Host "[OK] $Message" -ForegroundColor Green
}

function InfoLog {
    param([string]$Message)
    Write-Host "[INFO] $Message" -ForegroundColor White
}

function Initialize-ErrorHandling {
    <#
    .SYNOPSIS
    Configure error handling preferences
    .DESCRIPTION
    Set to Continue to prevent non-fatal errors from interrupting the script
    #>
    $script:ErrorActionPreference = 'Continue'
}

$global:CommonUtilsLoaded = $true
