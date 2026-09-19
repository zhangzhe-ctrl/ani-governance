#!/usr/bin/env bash
# Fedora only. Refresh task-owned port forwards after a pod restart.
set -euo pipefail
R=${1:?task directory required}
GOV="$R/work/governance"
if ! test -f "$GOV/go.mod"; then GOV="$GOV/backend"; fi
test -f "$GOV/go.mod"
LAB="$GOV/scripts/model-lab"
bash "$LAB/cluster.sh" 'bash -s' <<'REMOTE'
set -euo pipefail
cd /home/ubuntu/gov-model-20260919-01
for pair in governance:17788:7788 model:17990:19090 model:17991:19091; do
  IFS=: read -r service localport remoteport <<< "$pair"
  pidfile="forward-$localport.pid"
  if test -f "$pidfile"; then
    pid=$(cat "$pidfile")
    if test -r "/proc/$pid/cmdline" && tr '\0' ' ' < "/proc/$pid/cmdline" | grep -q "port-forward svc/$service $localport:$remoteport"; then kill "$pid"; fi
  fi
  nohup kubectl -n gov-model-20260919-01 port-forward "svc/$service" "$localport:$remoteport" --address=127.0.0.1 >"forward-$localport.log" 2>&1 < /dev/null &
  echo $! > "$pidfile"
done
REMOTE
if test -f "$R/private/tunnel.pid"; then
  pid=$(cat "$R/private/tunnel.pid")
  if test -r "/proc/$pid/cmdline" && tr '\0' ' ' < "/proc/$pid/cmdline" | grep -q 'ssh -N -L 127.0.0.1:17788'; then kill "$pid"; fi
fi
export SSH_ASKPASS=/home/chabking/ani-installer-runs/platform-20260918/access/askpass.sh
export SSH_ASKPASS_REQUIRE=force
nohup setsid -w ssh -N -L 127.0.0.1:17788:127.0.0.1:17788 -L 127.0.0.1:17990:127.0.0.1:17990 -L 127.0.0.1:17991:127.0.0.1:17991 -o ExitOnForwardFailure=yes -o ServerAliveInterval=15 ubuntu@172.16.101.10 > "$R/private/tunnel.log" 2>&1 < /dev/null &
echo $! > "$R/private/tunnel.pid"
