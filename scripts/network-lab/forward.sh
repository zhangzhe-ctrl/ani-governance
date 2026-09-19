#!/usr/bin/env bash
set -euo pipefail
R=${1:?}; OLD=${2:?}
OLD_GOV="$OLD/work/governance"
if ! test -f "$OLD_GOV/go.mod"; then OLD_GOV="$OLD_GOV/backend"; fi
test -f "$OLD_GOV/go.mod"
bash "$OLD_GOV/scripts/model-lab/forward.sh" "$OLD"
bash "$OLD_GOV/scripts/model-lab/cluster.sh" 'bash -s' <<'REMOTE'
set -euo pipefail
cd /home/ubuntu/gov-network-20260919-01
for pair in 18990:19090 18991:19091; do
 port=${pair%%:*}; f="forward-$port.pid"
 if test -f "$f"; then
  pid=$(cat "$f")
  if test -r "/proc/$pid/cmdline" && tr '\0' ' ' < "/proc/$pid/cmdline" | grep -q "port-forward svc/network $pair"; then kill "$pid"; fi
 fi
 nohup kubectl -n gov-model-20260919-01 port-forward svc/network "$pair" --address=127.0.0.1 > "forward-$port.log" 2>&1 < /dev/null &
 echo $! > "$f"
done
REMOTE
if test -f "$R/private/tunnel.pid"; then
 pid=$(cat "$R/private/tunnel.pid")
 if test -r "/proc/$pid/cmdline" && tr '\0' ' ' < "/proc/$pid/cmdline" | grep -q 'ssh -N -L 127.0.0.1:18990'; then kill "$pid"; fi
fi
export SSH_ASKPASS=/home/chabking/ani-installer-runs/platform-20260918/access/askpass.sh SSH_ASKPASS_REQUIRE=force
nohup setsid -w ssh -N -L 127.0.0.1:18990:127.0.0.1:18990 -L 127.0.0.1:18991:127.0.0.1:18991 -o ExitOnForwardFailure=yes -o ServerAliveInterval=15 ubuntu@172.16.101.10 > "$R/private/tunnel.log" 2>&1 < /dev/null &
echo $! > "$R/private/tunnel.pid"
