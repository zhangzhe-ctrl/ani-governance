<#
.SYNOPSIS
Docker Desktop utility function library
.DESCRIPTION
Provides common functions for Docker installation, configuration, and verification, with automatic pre-install detection to avoid duplicates
.NOTES
Encoding: UTF-8 (NO BOM) | Compatible: PowerShell 5.1+
#>

# Import the common utility library (if not already imported)
if (-not $global:CommonUtilsLoaded) {
    $LibDir = if ($PSScriptRoot) { $PSScriptRoot } else { Split-Path -Parent $MyInvocation.MyCommand.Path }
    . (Join-Path $LibDir "common-utils.ps1")
}

# ========== Detection functions ==========
function Test-DockerDesktopInstalled {
    <#
    .SYNOPSIS
    Detect whether Docker Desktop is installed
    .DESCRIPTION
    Detects via multiple methods: commands, registry, file paths, package managers
    .OUTPUTS
    [bool] Returns $true if installed, otherwise $false
    #>
    
    # 1. Check whether the docker command is available (fastest)
    if (Get-Command docker -ErrorAction SilentlyContinue) {
        try {
            $null = & docker --version 2>$null
            Log "  [DETECTED] Docker command available"
            return $true
        } catch {}
    }
    
    # 2. Check the registry (Winget/official installer)
    $regPaths = @(
        "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*",
        "HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*",
        "HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*"
    )
    
    foreach ($regPath in $regPaths) {
        $items = Get-ItemProperty $regPath -ErrorAction SilentlyContinue | 
            Where-Object { $_.DisplayName -like "*Docker Desktop*" -or $_.DisplayName -eq "Docker Desktop" }
        if ($items) {
            Log "  [DETECTED] Docker Desktop found in registry"
            return $true
        }
    }
    
    # 3. Check the installation directories
    $installPaths = @(
        "$env:ProgramFiles\Docker\Docker\Docker Desktop.exe",
        "${env:ProgramFiles(x86)}\Docker\Docker\Docker Desktop.exe",
        "$env:LOCALAPPDATA\Docker\Docker Desktop.exe"
    )
    
    foreach ($path in $installPaths) {
        if (Test-Path $path) {
            Log "  [DETECTED] Docker Desktop.exe found at: $path"
            return $true
        }
    }
    
    # 4. Check whether it is already installed via Scoop
    if (Get-Command scoop -ErrorAction SilentlyContinue) {
        $scoopList = & scoop list 2>$null
        if ($scoopList -match '\bdocker\b') {
            Log "  [DETECTED] Docker found in Scoop packages"
            return $true
        }
    }
    
    # 5. Check whether it is already installed via Winget
    if (Get-Command winget -ErrorAction SilentlyContinue) {
        try {
            $wingetList = & winget list --id Docker.DockerDesktop --exact 2>$null
            if ($wingetList -match 'Docker Desktop') {
                Log "  [DETECTED] Docker Desktop found via Winget"
                return $true
            }
        } catch {}
    }
    
    return $false
}

# ========== Installation function (enhanced) ==========
function Install-DockerDesktop {
    <#
    .SYNOPSIS
    Install Docker Desktop (detect first to avoid duplicates)
    .DESCRIPTION
    1. Check whether it is already installed first
    2. Skip if already installed
    3. If not installed, prefer Winget; fall back to Scoop on failure
    #>
    
    # 🔍 Detect whether it is already installed first
    Log "Checking if Docker Desktop is already installed..."
    if (Test-DockerDesktopInstalled) {
        SuccessLog "Docker Desktop is already installed, skip installation"
        return $true
    }
    
    Log "Docker Desktop not detected, starting installation..."

    # 🚀 Try installing via Winget
    if (Get-Command winget -ErrorAction SilentlyContinue) {
        Log "  Using Winget to install Docker Desktop"
        try {
            # Winget installation normally requires interactive confirmation; add --silent to reduce prompts
            $wingetArgs = @(
                'install', '--id', 'Docker.DockerDesktop',
                '-e', '--accept-package-agreements', '--accept-source-agreements',
                '--silent', '--disable-interactivity'
            )
            & winget @wingetArgs 2>&1 | Out-Null
            
            # Winget returns 0 for success, -1 for restart/user interaction required
            if ($LASTEXITCODE -eq 0 -or $LASTEXITCODE -eq -1) {
                SuccessLog "Docker Desktop install submitted via Winget"
                Log "  Note: Docker Desktop may require manual completion or system restart"
                return $true
            } else {
                Warn "  Winget install returned exit code: $LASTEXITCODE"
            }
        } catch {
            Warn "  [FAILED] Winget install Docker failed: $($_.Exception.Message)"
        }
    }

    # 🔁 Winget failed/unavailable, try Scoop
    Log "  Winget not available or failed, trying Scoop..."
    if (Get-Command scoop -ErrorAction SilentlyContinue) {
        try {
            & scoop install docker 2>&1 | Out-Null
            if ($LASTEXITCODE -eq 0) {
                SuccessLog "Docker CLI installed via Scoop"
                Log "  Note: Scoop installs Docker CLI only, not Docker Desktop GUI"
                return $true
            } else {
                Warn "  Scoop install returned exit code: $LASTEXITCODE"
            }
        } catch {
            Warn "  [FAILED] Scoop install Docker failed: $($_.Exception.Message)"
        }
    }

    # ❌ All methods failed
    ErrorLog "All installation methods failed. Please install Docker Desktop manually from: https://www.docker.com/products/docker-desktop"
    return $false
}

# ========== Service configuration function (unchanged, slightly optimized) ==========
function Configure-DockerService {
    param([bool]$IsAdmin = $false)

    if (-not $IsAdmin) {
        Warn "Docker service configuration requires administrator privileges (skipped)"
        return $false
    }

    Log "Configuring Docker service..."
    $dockerServiceName = $null
    
    # Look for the Docker Desktop service first
    $services = @('com.docker.service', 'docker', 'dockerd')
    foreach ($svc in $services) {
        if (Get-Service -Name $svc -ErrorAction SilentlyContinue) {
            $dockerServiceName = $svc
            break
        }
    }

    if (-not $dockerServiceName) {
        Warn "  Docker service not found (Docker Desktop may not be fully installed yet)"
        return $false
    }

    Log "  Found Docker service: $dockerServiceName"
    try {
        Set-Service -Name $dockerServiceName -StartupType Automatic -ErrorAction Stop
        Start-Service -Name $dockerServiceName -ErrorAction Stop
        SuccessLog "Docker service configured and started"
        return $true
    } catch {
        Warn "  [FAILED] Failed to configure Docker service: $($_.Exception.Message)"
        return $false
    }
}

# ========== Verification function (unchanged) ==========
function Verify-DockerInstallation {
    Log "Verifying Docker installation..."
    if (Get-Command docker -ErrorAction SilentlyContinue) {
        try {
            $dockerVersion = & docker --version 2>&1
            SuccessLog "Docker is available: $dockerVersion"
            return $true
        } catch {
            Warn "  [WARNING] Docker command found but failed to run: $($_.Exception.Message)"
            return $false
        }
    } else {
        Warn "  [NOT FOUND] Docker command not available"
        return $false
    }
}

# ========== Initialization function (unchanged) ==========
function Initialize-Docker {
    param(
        [bool]$SkipDocker = $false,
        [bool]$IsAdmin = $false
    )

    if ($SkipDocker) {
        Log "Skipping Docker installation per -SkipDocker"
        return $true
    }

    Log "========== Initializing Docker =========="
    
    $installSuccess = Install-DockerDesktop
    $configSuccess = Configure-DockerService -IsAdmin $IsAdmin
    $verifySuccess = Verify-DockerInstallation

    if (-not $installSuccess -and -not $verifySuccess) {
        Warn "Docker setup incomplete. Please check installation manually."
    }
    
    return $true
}