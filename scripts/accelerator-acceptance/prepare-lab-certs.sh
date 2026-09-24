#!/usr/bin/env bash
set -euo pipefail
# Independent CA for legacy quota_lab regression, never the Accelerator CA.
lab_dir=${1:?private task lab directory}
umask 077
mkdir -p "$lab_dir/certs"
cd "$lab_dir/certs"
if [[ ${2:-} != --extend-multisan ]]; then
  test ! -e ca.key
  openssl genrsa -out ca.key 2048
  openssl req -x509 -new -key ca.key -subj /CN=gov-acc-quota-lab-ca -days 2 -out ca.pem
fi
test -s ca.key
test -s ca.pem
for name in ani-governance ani-gpu-simulator other-service same-owner-alias mixed-owner; do
  if [[ ${2:-} == --extend-multisan && $name != same-owner-alias && $name != mixed-owner ]]; then continue; fi
  test ! -e "$name.key"
  openssl genrsa -out "$name.key" 2048
  san="DNS:$name"
  if [[ $name == same-owner-alias ]]; then san=DNS:ani-gpu-simulator,DNS:ani-gpu-simulator-alias; fi
  if [[ $name == mixed-owner ]]; then san=DNS:ani-gpu-simulator,DNS:ani-second-owner; fi
  printf 'subjectAltName=%s\n' "$san" > "$name.ext"
  openssl req -new -key "$name.key" -subj "/CN=$name" -out "$name.csr"
  openssl x509 -req -in "$name.csr" -CA ca.pem -CAkey ca.key -CAcreateserial -days 2 -extfile "$name.ext" -out "$name.pem"
done
