#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
vuln=${GOVULNCHECK:-govulncheck}
leaks=${GITLEAKS:-gitleaks}
"$vuln" -version | grep -F 'Scanner: govulncheck@v1.7.0'
go version -m "$(command -v "$leaks")" | grep -E 'mod[[:space:]]+github.com/zricethezav/gitleaks/v8[[:space:]]+v8.30.1'
sha256sum "$(command -v "$vuln")" "$(command -v "$leaks")"
sha256sum -c scripts/gpu-audit-fixture-sha256.txt
"$vuln" -show version,verbose ./app/admin/service/... ./pkg/...
"$leaks" dir --no-banner --no-color --redact .
