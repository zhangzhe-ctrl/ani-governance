#!/usr/bin/env bash
# Fedora only. Build and import only this task's backend images.
set -euo pipefail
R=${1:?task directory required}
GOV="$R/work/governance"
if ! test -f "$GOV/go.mod"; then GOV="$GOV/backend"; fi
test -f "$GOV/go.mod"
LAB="$GOV/scripts/model-lab"
mkdir -p "$R/images"
tags=()
for name in ani-governance ani-model-service; do
  mkdir -p "$R/images/$name"
  cp "$R/$name" "$R/images/$name/server"
  cat > "$R/images/$name/Dockerfile" <<'EOF'
FROM debian@sha256:12c396bd585df7ec21d5679bb6a83d4878bc4415ce926c9e5ea6426d23c60bdc
COPY server /server
USER 65532:65532
ENTRYPOINT ["/server"]
EOF
  digest=$(sha256sum "$R/$name" | cut -c1-12)
  tag="$name:gov-model-20260919-01-$digest"
  docker build --network=none -t "$tag" "$R/images/$name"
  tags+=("$tag")
done
python3 - "$R/inputs/images.json" "${tags[@]}" <<'PY'
import json,sys
from pathlib import Path
p=Path(sys.argv[1]); data=json.loads(p.read_text())
data.update(governance=sys.argv[2],model=sys.argv[3]);p.write_text(json.dumps(data))
PY
docker save "${tags[@]}" -o "$R/inputs/backend-images.tar"
cat "$R/inputs/backend-images.tar" | bash "$LAB/cluster.sh" 'cat > /home/ubuntu/gov-model-20260919-01/backend-images.tar'
cat /home/chabking/ani-installer-runs/platform-20260918/access/node-password | bash "$LAB/cluster.sh" 'sudo -S ctr -n k8s.io images import /home/ubuntu/gov-model-20260919-01/backend-images.tar'
python3 "$LAB/prepare.py" "$R"
cat "$R/private/runtime.json" | bash "$LAB/cluster.sh" 'kubectl apply -f -'
