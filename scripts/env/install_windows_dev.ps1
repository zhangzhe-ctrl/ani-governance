<#
.SYNOPSIS
Windows dev environment auto-setup script (Scoop/Docker/Go)
#>

param(
    [switch]$SkipDocker,
    [switch]$AutoConfirm
)

#Set-StrictMode -Version Latest
$ErrorActionPreference = 'Continue'  # Use Continue to avoid exiting on non-fatal errors
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

# Show version info
Write-Host "PowerShell Version: $($PSVersionTable.PSVersion)" -ForegroundColor Cyan

# ========== Import function libraries (load first, then configure) ==========
$libFiles = @(
    'common-utils.ps1',
    'scoop-utils.ps1', 
    'docker-utils.ps1',
    'go-utils.ps1',
    'host-utils.ps1'
)

foreach ($lib in $libFiles) {
    $libPath = Join-Path $ScriptDir "lib\$lib"
    if (Test-Path $libPath) {
        . $libPath
        Log "Loaded: $lib"
    } else {
        ErrorLog "Missing library: $libPath"
        exit 1
    }
}

Log "Library load test"
Initialize-ErrorHandling

# Check administrator privileges
$currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
$IsAdmin = $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $IsAdmin) {
    Warn "Not running as administrator! Docker auto-start and hosts configuration will be skipped."
}

# ========== Hosts configuration (requires administrator privileges)
if ($IsAdmin) {
    $services = @('postgres', 'mysql', 'redis', 'minio')
    Initialize-Hosts -Services $services -IP "127.0.0.1" -DomainSuffix ".local"
} else {
    Warn "Skipping hosts configuration (requires administrator privileges)"
}

# ========== Scoop installation
Initialize-Scoop -Buckets @('main', 'extras') -Packages @('wget', 'unzip', 'git', 'jq', 'make', 'grep', 'gawk', 'sed', 'touch', 'mingw', 'nodejs', 'go')

# ========== Docker installation
Initialize-Docker -SkipDocker $SkipDocker -IsAdmin $IsAdmin

# ========== Go environment configuration
Initialize-GoEnvironment -GoPath (Join-Path $env:USERPROFILE "go") -GoProxy "https://goproxy.io,direct" -SkipPlugins $false -SkipCliTools $false

# ========== Manual configuration tips
$goPathValue = $env:GOPATH
if (-not $goPathValue) { $goPathValue = Join-Path $env:USERPROFILE "go" }
Log "Environment setup completed (current session only)!"
$tips = @"
==================== MANUAL CONFIG TIPS ====================
1. To make GOPATH permanent:
   Add these lines to your PowerShell Profile:
   `$env:GOPATH = "$goPathValue"
   `$env:PATH += ";$goPathValue\bin"

2. Check service status (admin):
   Get-Service com.docker.service
"@
Write-Host $tips -ForegroundColor Green
