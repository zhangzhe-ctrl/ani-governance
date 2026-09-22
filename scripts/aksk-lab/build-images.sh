#!/usr/bin/env bash
# Run on ubuntu after building the exact recorded source into RUN/bin.
set -euo pipefail
R=${ANI_AKSK_LAB_RUN:-/home/ubuntu/workspace/aksk-vpc-20260922}
cd "$R"
mkdir -p images/governance images/network
cp bin/governance images/governance/server
cp bin/admin images/governance/admin
cp /home/ubuntu/.local/share/ani-network-service/bin/atlas-v1.3.0 images/governance/atlas
cp -R governance/migrations images/governance/migrations
cp bin/network images/network/server
# This preloaded base has /bin/sh for the explicit Atlas/admin initialization job.
base=$(docker image inspect busybox:1.37 --format '{{.Id}}')
printf '%s\n' "$base" > evidence/runtime-base-image.txt
cat > images/governance/Dockerfile <<EOF
FROM busybox:1.37
COPY server admin /app/
COPY atlas /bin/atlas
COPY migrations /app/migrations/
ENTRYPOINT ["/app/server"]
EOF
cat > images/network/Dockerfile <<EOF
FROM busybox:1.37
COPY server /app/server
USER 65532:65532
ENTRYPOINT ["/app/server"]
EOF
gov_hash=$(sha256sum bin/governance | cut -c1-16)
net_hash=$(sha256sum bin/network | cut -c1-16)
gov_image="ani-governance:aksk-20260922-$gov_hash"
net_image="ani-network-service:aksk-20260922-$net_hash"
docker build --pull=false --network=none -t "$gov_image" images/governance
docker build --pull=false --network=none -t "$net_image" images/network
python3 - "$gov_image" "$net_image" <<'PY'
import json,sys
from pathlib import Path
Path('images.json').write_text(json.dumps(dict(governance=sys.argv[1], network=sys.argv[2]), indent=2))
PY
docker save "$gov_image" "$net_image" -o images/application-images.tar
for node in kind-test-control-plane kind-test-worker kind-test-worker2; do
  docker exec -i "$node" ctr -n k8s.io images import - < images/application-images.tar
done
docker image inspect "$gov_image" "$net_image" --format '{{.RepoTags}} {{.Id}}' > evidence/built-images.txt
sha256sum bin/governance bin/admin bin/network > evidence/binaries.sha256
