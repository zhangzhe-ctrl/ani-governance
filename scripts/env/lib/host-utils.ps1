<#
.SYNOPSIS
Hosts file management utility library
.DESCRIPTION
Provides add, remove, modify, and query operations for the hosts file
.NOTES
Saved encoding: UTF-8 with BOM | Requires administrator privileges | Compatible: PowerShell 5.1+
#>

# Import the common utility library (if not already imported)
if (-not (Test-Path variable:global:CommonUtilsLoaded)) {
    $LibDir = Split-Path -Parent $MyInvocation.MyCommand.Path
    . "$LibDir\common-utils.ps1"
    Set-Variable -Name CommonUtilsLoaded -Value $true -Scope Global
}

function Edit-Hosts {
    <#
    .SYNOPSIS
    Edit the hosts file (add or remove records)
    .DESCRIPTION
    Add or remove IP-to-domain mappings in the system hosts file
    .PARAMETER IP
    IP address
    .PARAMETER Domain
    Domain name
    .PARAMETER Operate
    Operation type: Add or Remove
    .EXAMPLE
    Edit-Hosts -IP "127.0.0.1" -Domain "postgres.local" -Operate "Add"
    .EXAMPLE
    Edit-Hosts -IP "127.0.0.1" -Domain "postgres.local" -Operate "Remove"
    #>
    [CmdletBinding()]
    param(
        [Parameter(Mandatory=$true)]
        [string]$IP,
        [Parameter(Mandatory=$true)]
        [string]$Domain,
        [ValidateSet("Add","Remove")]
        [string]$Operate = "Add"
    )

    $hostsFile = "$env:SystemRoot\System32\drivers\etc\hosts"

    # Verify administrator privileges
    $currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
    if (-not $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        ErrorLog "Please run this script as administrator"
        return $false
    }

    try {
        $pattern = "^\s*$IP\s+$Domain\s*$"
        
        if ($Operate -eq "Add") {
            $content = Get-Content -Path $hostsFile -Raw -Encoding UTF8
            
            if ($content -match $pattern) {
                Log "Record already exists, skipping duplicate add: $IP $Domain"
                return $true
            }
            
            Add-Content -Path $hostsFile -Value "`n$IP $Domain" -Encoding UTF8
            SuccessLog "Added successfully: $IP $Domain"
        }
        else {
            $lines = Get-Content -Path $hostsFile -Encoding UTF8
            $newLines = $lines | Where-Object { $_ -notmatch $pattern }
            
            if ($lines.Count -eq $newLines.Count) {
                Warn "Record does not exist, nothing to remove: $IP $Domain"
                return $true
            }
            
            Set-Content -Path $hostsFile -Value $newLines -Encoding UTF8
            SuccessLog "Removed successfully: $IP $Domain"
        }

        # Flush the DNS cache
        ipconfig /flushdns | Out-Null
        Log "DNS cache flushed"
        
        return $true
    }
    catch {
        ErrorLog "Operation failed: $($_.Exception.Message)"
        return $false
    }
}

function Initialize-Hosts {
    <#
    .SYNOPSIS
    Initialize hosts records in batch
    .DESCRIPTION
    Add hosts records in batch for multiple services
    .PARAMETER Services
    Array of service names
    .PARAMETER IP
    IP address (default 127.0.0.1)
    .PARAMETER DomainSuffix
    Domain suffix (default .local)
    .EXAMPLE
    Initialize-Hosts -Services @("postgres", "mysql", "redis") -IP "127.0.0.1"
    #>
    param(
        [Parameter(Mandatory=$true)]
        [string[]]$Services,
        [string]$IP = "127.0.0.1",
        [string]$DomainSuffix = ".local"
    )

    Log "========== Initializing Hosts Records =========="
    
    $successCount = 0
    $failCount = 0
    
    foreach ($service in $Services) {
        $domain = "$service$DomainSuffix"
        $result = Edit-Hosts -IP $IP -Domain $domain -Operate "Add"
        
        if ($result) {
            $successCount++
        } else {
            $failCount++
        }
    }
    
    Log ""
    Log "Done: $successCount succeeded, $failCount failed"
    
    return ($failCount -eq 0)
}

