#!/usr/bin/env bash
# Fedora-only access to the user-authorized test cluster. No shared temp files.
set -euo pipefail
export SSH_ASKPASS=/home/chabking/ani-installer-runs/platform-20260918/access/askpass.sh
export SSH_ASKPASS_REQUIRE=force
exec setsid -w ssh -o BatchMode=no -o ConnectTimeout=10 ubuntu@172.16.101.10 "$@"
