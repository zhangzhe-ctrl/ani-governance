# Retired ANI Governance automatic Windows development installer.
# Keep the former flags parseable; do not load libraries or alter the machine.
param(
    [switch]$SkipDocker,
    [switch]$AutoConfirm
)
[Console]::Error.WriteLine('Retired: read docs/development.md; no environment was changed.')
exit 2
