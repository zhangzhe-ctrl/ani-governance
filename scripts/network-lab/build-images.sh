#!/usr/bin/env bash
# Fedora only; needs binaries built from the recorded source snapshot.
set -euo pipefail
R=${1:?}; OLD=${2:?}
OLD_GOV="$OLD/work/governance"
if ! test -f "$OLD_GOV/go.mod"; then OLD_GOV="$OLD_GOV/backend"; fi
test -f "$OLD_GOV/go.mod"
LAB="$OLD_GOV/scripts/model-lab"
tags=()
for name in ani-governance ani-network-service; do
 mkdir -p "$R/images/$name"
 cp "$R/$name" "$R/images/$name/server"
 cat > "$R/images/$name/Dockerfile" <<'EOF'
FROM debian@sha256:12c396bd585df7ec21d5679bb6a83d4878bc4415ce926c9e5ea6426d23c60bdc
COPY server /server
USER 65532:65532
ENTRYPOINT ["/server"]
EOF
 digest=$(sha256sum "$R/$name" | cut -c1-12)
 tag="$name:gov-network-20260919-01-$digest"
 docker build --network=none -t "$tag" "$R/images/$name"
 tags+=("$tag")
done
python3 - "$R/inputs/images.json" "${tags[@]}" <<'PY'
import json,sys
from pathlib import Path
Path(sys.argv[1]).write_text(json.dumps(dict(governance=sys.argv[2],network=sys.argv[3],postgres='postgres:17.11-bookworm')))
PY
docker save "${tags[@]}" -o "$R/inputs/backend-images.tar"
bash "$LAB/cluster.sh" 'mkdir -p /home/ubuntu/gov-network-20260919-01' < /dev/null
cat "$R/inputs/backend-images.tar" | bash "$LAB/cluster.sh" 'cat > /home/ubuntu/gov-network-20260919-01/backend-images.tar'
cat /home/chabking/ani-installer-runs/platform-20260918/access/node-password | bash "$LAB/cluster.sh" 'sudo -S ctr -n k8s.io images import /home/ubuntu/gov-network-20260919-01/backend-images.tar'
