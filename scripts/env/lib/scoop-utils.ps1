<#
.SYNOPSIS
Scoop package manager utility library
.DESCRIPTION
Provides common functions for Scoop installation, configuration, and package management
.NOTES
Saved encoding: UTF-8 with BOM | Compatible: PowerShell 5.1+
#>

# Import the common utility library (if not already imported)
if (-not $global:CommonUtilsLoaded) {
    $LibDir = if ($PSScriptRoot) { $PSScriptRoot } else { Split-Path -Parent $MyInvocation.MyCommand.Path }
    . (Join-Path $LibDir "common-utils.ps1")
}

# ========== Scoop installation function ==========
function Install-Scoop {
    <#
    .SYNOPSIS
    Install the Scoop package manager
    .DESCRIPTION
    Install Scoop and configure basic settings
    #>
    Log "Installing Scoop..."
    try {
        Set-ExecutionPolicy RemoteSigned -Scope CurrentUser -Force -ErrorAction SilentlyContinue
        [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
        Invoke-RestMethod -Uri https://get.scoop.sh -UseBasicParsing | Invoke-Expression
        Log "Scoop installed successfully"
    } catch {
        ErrorLog "Scoop install failed: $($_.Exception.Message)"
        return $false
    }
    return $true
}

# Scoop configuration function
function Add-ScoopBuckets {
    <#
    .SYNOPSIS
    Add Scoop buckets
    .PARAMETER Buckets
    List of buckets to add (array)
    #>
    param(
        [string[]]$Buckets = @('main', 'extras')
    )

    Log "Configuring Scoop Buckets..."
    $bucketNames = @(& scoop bucket list 2>$null | ForEach-Object {
        if ($_ -is [string]) { $_.Trim() } else { $_.Name }
    })

    foreach ($bucket in $Buckets) {
        if ($bucketNames -notcontains $bucket) {
            Log "  Adding bucket: $bucket"
            try {
                & scoop bucket add $bucket --no-update 2>$null
                Log "    [OK] Bucket added: $bucket"
            } catch {
                Warn "    [FAILED] Failed to add bucket $bucket : $($_.Exception.Message)"
            }
        } else {
            Log "  [SKIP] Bucket already exists: $bucket"
        }
    }
}

function Install-ScoopPackages {
    <#
    .SYNOPSIS
    Install packages via Scoop
    .PARAMETER Packages
    List of packages to install (array)
    #>
    param(
        [Parameter(Mandatory=$true)]
        [string[]]$Packages
    )

    Log "Installing Scoop packages..."
    foreach ($pkg in $Packages) {
        # Check whether the package is already installed
        if (Get-Command $pkg -ErrorAction SilentlyContinue) {
            Log "  [SKIP] $pkg already installed"
            continue
        }

        Log "  Installing: $pkg"
        try {
            & scoop install $pkg 2>$null
            if ($LASTEXITCODE -eq 0) {
                Log "    [OK] Successfully installed: $pkg"
            } else {
                Warn "    [FAILED] Installation failed: $pkg (exit code: $LASTEXITCODE)"
            }
        } catch {
            Warn "    [ERROR] Failed to install $pkg : $($_.Exception.Message)"
        }
    }
}

# Scoop initialization function
function Initialize-Scoop {
    <#
    .SYNOPSIS
    Initialize Scoop (install Scoop and base packages)
    .PARAMETER Buckets
    List of buckets to add
    .PARAMETER Packages
    List of packages to install
    #>
    param(
        [string[]]$Buckets = @('main', 'extras'),
        [string[]]$Packages = @('wget', 'unzip', 'git', 'jq', 'make', 'grep', 'gawk', 'sed', 'touch', 'mingw', 'nodejs', 'go')
    )

    # 1. Check and install Scoop
    if (-not (Get-Command scoop -ErrorAction SilentlyContinue)) {
        if (-not (Install-Scoop)) {
            return $false
        }
    } else {
        Log "Scoop already installed, skip installation"
    }

    # 2. Configure buckets
    Add-ScoopBuckets -Buckets $Buckets

    # 3. Install packages
    Install-ScoopPackages -Packages $Packages

    return $true
}
