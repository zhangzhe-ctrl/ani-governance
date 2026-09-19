<#
.SYNOPSIS
Go environment configuration utility library (enhanced detection)
.DESCRIPTION
Provides common functions for Go runtime installation, environment variable configuration, and plugin/CLI tool installation, with smart skipping of already-installed items
.NOTES
Encoding: UTF-8 (NO BOM) | Compatible: PowerShell 5.1+
#>

# ========== Import the common utility library ==========
if (-not $global:CommonUtilsLoaded) {
    $LibDir = if ($PSScriptRoot) { $PSScriptRoot } else { Split-Path -Parent $MyInvocation.MyCommand.Path }
    . (Join-Path $LibDir "common-utils.ps1")
}

# ========== Detection functions ==========
function Test-GoToolInstalled {
    <#
    .SYNOPSIS
    Detect whether a Go tool is installed
    .PARAMETER ToolName
    Executable file name of the tool (without .exe), e.g. 'kratos', 'protoc-gen-go'
    .OUTPUTS
    [bool] Returns $true if installed
    #>
    param([string]$ToolName)
    
    # 1. Check GOBIN first
    if ($env:GOBIN -and (Test-Path (Join-Path $env:GOBIN "$ToolName.exe"))) {
        return $true
    }
    
    # 2. Check GOPATH/bin
    if ($env:GOPATH -and (Test-Path (Join-Path $env:GOPATH "bin\$ToolName.exe"))) {
        return $true
    }
    
    # 3. Check whether the command exists in PATH
    if (Get-Command $ToolName -ErrorAction SilentlyContinue) {
        return $true
    }
    
    return $false
}

function Test-GoProxyConfigured {
    <#
    .SYNOPSIS
    Detect whether the Go proxy configuration matches the expected value
    .PARAMETER ExpectedProxy
    The expected GOPROXY value
    .OUTPUTS
    [bool] Returns $true if the configuration matches
    #>
    param([string]$ExpectedProxy)
    
    try {
        $currentProxy = & go env GOPROXY 2>$null
        # Supports multiple comma-separated proxies; order does not matter
        $expectedList = $ExpectedProxy -split ',' | ForEach-Object { $_.Trim() }
        $currentList = $currentProxy -split ',' | ForEach-Object { $_.Trim() }
        
        # Check whether all expected proxies are present in the current configuration
        $allMatch = $true
        foreach ($exp in $expectedList) {
            if ($currentList -notcontains $exp) {
                $allMatch = $false
                break
            }
        }
        
        if ($allMatch) {
            Log "  [DETECTED] GOPROXY already configured: $currentProxy"
            return $true
        }
    } catch {}
    
    return $false
}

# ========== Go runtime installation (with detection) ==========
function Install-GoRuntime {
    Log "Checking Go runtime..."
    
    # 🔍 Detect whether Go is already installed
    if (Get-Command go -ErrorAction SilentlyContinue) {
        try {
            $goVersion = & go version 2>&1
            SuccessLog "Go already installed: $goVersion"
            return $true
        } catch {}
    }
    
    Log "Go not found, installing via Scoop..."
    try {
        & scoop install go 2>&1 | Out-Null
        if ($LASTEXITCODE -eq 0) {
            $goVersion = & go version 2>&1
            SuccessLog "Go installed: $goVersion"
            return $true
        } else {
            Warn "Go installation failed (exit code: $LASTEXITCODE)"
            return $false
        }
    } catch {
        ErrorLog "Failed to install Go: $($_.Exception.Message)"
        return $false
    }
}

# ========== Environment variable configuration (fixed return value) ==========
function Set-GoEnvironment {
    param(
        [string]$GoPath = (Join-Path $env:USERPROFILE "go"),
        [string]$GoProxy = "https://goproxy.io,direct"
    )

    Log "Configuring Go environment..."
    
    # Create GOPATH
    if (-not (Test-Path $GoPath)) {
        Log "  Creating GOPATH: $GoPath"
        New-Item -Path $GoPath -ItemType Directory -Force | Out-Null
    }
    
    # Configure the current session
    $env:GOPATH = $GoPath
    Log "  [OK] GOPATH = $GoPath (current session)"
    
    # Configure GOBIN
    $goBinPath = Join-Path $GoPath "bin"
    if (-not (Test-Path $goBinPath)) {
        New-Item -Path $goBinPath -ItemType Directory -Force | Out-Null
    }
    $env:GOBIN = $goBinPath
    
    # Add to PATH (avoid duplicates)
    if ($env:PATH -notlike "*$goBinPath*") {
        $env:PATH = "$goBinPath;$env:PATH"
        Log "  [OK] Added to PATH: $goBinPath"
    }
    
    # ✅ Return a hashtable for clearer call sites
    return @{
        GoPath = $GoPath
        GoBin = $goBinPath
    }
}

# ========== Go proxy configuration (with detection) ==========
function Set-GoProxy {
    param(
        [string]$GoProxy = "https://goproxy.io,direct",
        [bool]$GoModuleOn = $true
    )

    Log "Configuring Go proxy..."
    
    # 🔍 Detect whether it is already configured first
    if (Test-GoProxyConfigured -ExpectedProxy $GoProxy) {
        SuccessLog "GOPROXY already matches expected value, skip configuration"
        
        # But still check GO111MODULE
        if ($GoModuleOn) {
            $currentModule = & go env GO111MODULE 2>$null
            if ($currentModule -ne 'on') {
                Log "  Enabling GO111MODULE..."
                & go env -w GO111MODULE=on 2>&1 | Out-Null
            }
        }
        return $true
    }
    
    try {
        # Configure GOPROXY
        Log "  Setting GOPROXY: $GoProxy"
        & go env -w GOPROXY=$GoProxy 2>&1 | Out-Null
        
        # Configure GO111MODULE
        if ($GoModuleOn) {
            Log "  Enabling GO111MODULE..."
            & go env -w GO111MODULE=on 2>&1 | Out-Null
        }
        
        # Show the result
        $envInfo = & go env GOPROXY, GO111MODULE 2>&1
        Log "  [OK] Current config: GOPROXY=$($envInfo[0]), GO111MODULE=$($envInfo[1])"
        return $true
    } catch {
        ErrorLog "Failed to configure Go proxy: $($_.Exception.Message)"
        return $false
    }
}

# ========== Batch install Go packages (smart skip) ==========
function Install-GoPackages {
    param(
        [Parameter(Mandatory=$true)]
        [string[]]$Packages,
        [switch]$Force  # Force reinstallation
    )

    if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
        ErrorLog "Go not found, cannot install packages"
        return
    }

    foreach ($pkg in $Packages) {
        # Extract tool name: google.golang.org/protobuf/cmd/protoc-gen-go@latest → protoc-gen-go
        $toolName = ($pkg -split '/' | Select-Object -Last 1) -replace '@.*$', ''
        
        # 🔍 Detect whether already installed (unless forced)
        if (-not $Force -and (Test-GoToolInstalled -ToolName $toolName)) {
            Log "  [SKIP] $toolName already installed"
            continue
        }
        
        Log "  Installing: $pkg"
        try {
            & go install $pkg 2>&1 | Out-Null
            if ($LASTEXITCODE -eq 0) {
                SuccessLog "Installed: $toolName"
            } else {
                Warn "  [FAILED] $pkg (exit code: $LASTEXITCODE)"
            }
        } catch {
            ErrorLog "  [ERROR] Failed to install $pkg : $($_.Exception.Message)"
        }
    }
}

# ========== Plugin installation (unchanged, calls the functions above) ==========
function Install-GoPlugins {
    Log "Installing Protobuf compiler plugins..."
    
    $plugins = @(
        'google.golang.org/protobuf/cmd/protoc-gen-go@latest',
        'google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest',
        'github.com/go-kratos/kratos/cmd/protoc-gen-go-http/v2@latest',
        'github.com/go-kratos/kratos/cmd/protoc-gen-go-errors/v2@latest',
        'github.com/google/gnostic/cmd/protoc-gen-openapi@latest',
        'github.com/envoyproxy/protoc-gen-validate@latest',
        'github.com/tx7do/go-wind-toolkit/protoc-gen-go-redact@v0.0.0-20260831125122-5bb4931991b2'
    )
    
    Install-GoPackages -Packages $plugins
}

# ========== CLI tool installation (unchanged) ==========
function Install-GoCliTools {
    Log "Installing CLI scaffold tools..."
    
    $cliTools = @(
        'github.com/go-kratos/kratos/cmd/kratos/v2@latest',
        'github.com/google/gnostic@latest',
        'github.com/bufbuild/buf/cmd/buf@latest',
        'entgo.io/ent/cmd/ent@latest',
        'github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest',
        'github.com/tx7do/go-wind-toolkit/gowind/cmd/gow@v1.0.3'
    )
    
    if ($cliTools.Count -gt 0) {
        Install-GoPackages -Packages $cliTools
    } else {
        Log "No CLI tools to install"
    }
}

# ========== Initialization function (fixed function name calls) ==========
function Initialize-GoEnvironment {
    param(
        [string]$GoPath = (Join-Path $env:USERPROFILE "go"),
        [string]$GoProxy = "https://goproxy.io,direct",
        [bool]$SkipPlugins = $false,
        [bool]$SkipCliTools = $false
    )

    Log "========== Initializing Go Environment =========="
    
    # 1. Install the Go runtime
    $runtimeSuccess = Install-GoRuntime
    if (-not $runtimeSuccess) {
        Warn "Go runtime installation failed, skipping further setup"
        return $false
    }
    
    # 2. Configure environment variables ✅ Fixed: function renamed to Set-GoEnvironment
    Log ""
    $envConfig = Set-GoEnvironment -GoPath $GoPath
    $GoPath = $envConfig.GoPath  # Read from the hashtable for clarity
    
    # 3. Configure proxy ✅ Fixed: function renamed to Set-GoProxy
    Log ""
    $proxySuccess = Set-GoProxy -GoProxy $GoProxy -GoModuleOn $true
    if (-not $proxySuccess) {
        Warn "Go proxy configuration failed"
    }
    
    # 4. Install plugins
    if (-not $SkipPlugins) {
        Log ""
        Install-GoPlugins
    } else {
        Log "Skipping Go plugins installation per -SkipPlugins"
    }
    
    # 5. Install CLI tools
    if (-not $SkipCliTools) {
        Log ""
        Install-GoCliTools
    } else {
        Log "Skipping CLI tools installation per -SkipCliTools"
    }
    
    Log ""
    SuccessLog "Go Environment Setup Completed"
    return $true
}
