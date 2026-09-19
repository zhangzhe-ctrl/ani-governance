$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path

# 1. Load the library
. "$ScriptDir\common-utils.ps1"

Initialize-ErrorHandling

# 2. Test the logging functions
Log "Test log"
Warn "Test warning"
ErrorLog "Test error"
SuccessLog "Test success"
InfoLog "Test info"

# 3. Verify the flag
if ($global:CommonUtilsLoaded) { Write-Host "✓ Library loaded successfully" -ForegroundColor Green }

# 4. Test whether the trap takes effect (deliberately triggers a non-fatal error)
Get-ChildItem "C:\NonExistentPath" -ErrorAction Stop