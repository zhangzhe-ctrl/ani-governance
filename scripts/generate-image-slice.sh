#!/usr/bin/env bash
set -Eeuo pipefail
# IMG-01 contract slice. Run only on Fedora in an exact-SHA task checkout.
# Build in a staging directory; never clean or regenerate unrelated API output.
cd "$(dirname "$0")/.."
BUF=${BUF:?set BUF to a verified buf v1.60.0 executable}
test "$("$BUF" --version)" = 1.60.0
test "$(go version -m "$BUF" | awk '$1 == "mod" {print $2 "@" $3; exit}')" = github.com/bufbuild/buf@v1.60.0
stage=$(mktemp -d "${TMPDIR:?set a task-private TMPDIR}/image-api.XXXXXX")
trap 'rm -rf -- "$stage"' EXIT
cp -a api "$stage/api"
(
 cd "$stage/api"
 "$BUF" lint --path protos/admin/service/v1/i_image.proto --path protos/catalog/service/v1/image.proto
 "$BUF" build --path protos/admin/service/v1/i_image.proto --path protos/catalog/service/v1/image.proto
 "$BUF" generate --template buf.image.gen.yaml
)
for output in catalog/service/v1/image.pb.go admin/service/v1/i_image.pb.go admin/service/v1/i_image_grpc.pb.go admin/service/v1/i_image_http.pb.go; do
 test -f "$stage/api/gen/go/$output"
 mkdir -p "api/gen/go/$(dirname "$output")"
 cp "$stage/api/gen/go/$output" "api/gen/go/$output"
done
