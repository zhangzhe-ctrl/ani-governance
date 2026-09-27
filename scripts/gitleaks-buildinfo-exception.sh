#!/usr/bin/env bash
set -euo pipefail
# Review aid for the T14 buildinfo exception in .gitleaks.toml: proves the exception is
# pinned to the two paths and the one dirhash, and never silences the rule otherwise.
# Run with GITLEAKS pointing at the locked gitleaks v8.30.1.
cd "$(dirname "$0")/.."
leaks=${GITLEAKS:-gitleaks}
hash=ZbhmVJ4yq5RZDUsyP8lcBcGMsjsaTqXEFt6isdtMDfA=
line=$'dep\tgithub.com/getkin/kin-openapi\tv0.149.0\th1:'$hash
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

scan() { # scan <dir> -> leak count, printed as "<name> <count>"
    # gitleaks matches allowlist paths against the path as walked, so scanning "."
    # reproduces the repository form; an absolute scan root would not.
    local dir=$1
    local name=$2
    local report="$work/$name.json"
    (cd "$dir" && "$leaks" dir --no-banner --report-format json --report-path "$report" . >/dev/null 2>&1) || true
    python3 -c "import json,sys;print('$name', len(json.load(open(sys.argv[1]))))" "$report"
}

mkdir -p "$work/otherpath" "$work/olddigest/migration/patches/T14/buildinfo" "$work/gosum"
printf '%s\n' "$line" > "$work/otherpath/not-buildinfo.txt"
printf '%s\n' "$line" > "$work/olddigest/migration/patches/T14/buildinfo/admin.txt"
printf 'dep\tgithub.com/example/kin-openapi\tv9.9.9\th1:AAAAVVVV1111bbbb2222CCCCdddd3333EEEEffff4444G=\n' \
    >> "$work/olddigest/migration/patches/T14/buildinfo/admin.txt"
cp go.sum "$work/gosum/go.sum"
printf '%s\n' "$line" > "$work/gosum/other.txt"
for d in otherpath olddigest gosum; do cp .gitleaks.toml "$work/$d/.gitleaks.toml"; done

# same content, another path: still reported -> the exception does not extend past its paths
scan "$work/otherpath" other_path_still_reported | grep -q ' 1$'
# inside an exempt path, a different digest: still reported -> the exception does not cover the file
scan "$work/olddigest" other_digest_in_exempt_path_still_reported | grep -q ' 1$'
# the content class the scanner already exempts by name, next to a file it does not
scan "$work/gosum" go_sum_copy_exempt_but_twin_reported | grep -q ' 1$'
scan . repository_tree | grep -q ' 0$' && echo "controls pass: exception is path-and-content pinned, tree clean"
