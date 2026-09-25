#!/usr/bin/env python3
"""T09-B1: build the must-pass profile from what actually exists in the tree.

Two sources, neither hand-picked:
  * the 78 top-level tests `go test -list` reports for the taken-over generator
    package (this is what T09-B1 blocked, and it is now enumerated from the
    source instead of copied from an old report, so a test added or renamed
    upstream cannot silently drop out of the gate);
  * the 16 tests the original T09 receipt already required, asserted to still
    exist so the earlier scope is re-run rather than quietly shrunk.
"""
import json
import subprocess
import sys

GEN = "go-wind-admin/pkg/localdeps/go-wind-toolkit/protoc-gen-go-redact"
RUNTIME = GEN + "/redact/v1"
SERVER = "go-wind-admin/app/admin/service/internal/server"
SERVICE = "go-wind-admin/app/admin/service/internal/service"
PAGINATION = "go-wind-admin/pkg/localdeps/go-crud/pagination"

REPO = "/home/ubuntu/Workspace/ani-governance"
OLD_PROFILE = "migration/patches/T09/T09-required-profile.json"

# package -> relative dir passed to `go test -list`
DIRS = {
    GEN: "pkg/localdeps/go-wind-toolkit/protoc-gen-go-redact",
    RUNTIME: "pkg/localdeps/go-wind-toolkit/protoc-gen-go-redact/redact/v1",
    SERVER: "app/admin/service/internal/server",
    SERVICE: "app/admin/service/internal/service",
    PAGINATION: "pkg/localdeps/go-crud/pagination",
}


def listed(pkg_dir):
    out = subprocess.run(["go", "test", "-mod=readonly", "-list", ".*", "./" + pkg_dir],
                         cwd=REPO, capture_output=True, text=True, check=True).stdout
    names = []
    for line in out.splitlines():
        line = line.strip()
        if not line or line.startswith(("ok ", "? ", "PASS", "---")) or line.endswith("[no test files]"):
            continue
        if "/" in line:            # -list also echoes parent/subtest forms; keep top level
            continue
        # `go test -list` reports benchmarks too, but they only run under -bench,
        # so requiring one here would guarantee a false "missing" in the gate.
        if line.startswith("Benchmark"):
            continue
        names.append(line)
    return sorted(set(names))


def main():
    rows = []
    for pkg in (GEN, RUNTIME):
        for t in listed(DIRS[pkg]):
            rows.append({"Package": pkg, "Test": t})
    gen_count = len(rows)

    old = json.load(open(f"{REPO}/{OLD_PROFILE}"))
    present = {(r["Package"], r["Test"]) for r in rows}
    added, missing = [], []
    for r in old:
        if (r["Package"], r["Test"]) in present:
            missing.append(r)          # already covered by the enumeration above
            continue
        names = listed(DIRS[r["Package"]])
        if r["Test"] in names:
            added.append(r)
            rows.append(r)
        else:
            sys.exit(f"FAIL: the previously required {r['Package']}/{r['Test']} "
                     f"is no longer reported by `go test -list` in {DIRS[r['Package']]}")

    if len({(r["Package"], r["Test"]) for r in rows}) != len(rows):
        sys.exit("FAIL: duplicate rows in the profile")

    with open(f"{REPO}/migration/patches/T09/T09B1-required-profile.json", "w") as fh:
        json.dump(rows, fh, indent=1, sort_keys=True)
        fh.write("\n")

    print(f"generator package top-level tests enumerated: {gen_count - len([r for r in rows if r['Package'] == RUNTIME])}")
    print(f"runtime package tests: {len([r for r in rows if r['Package'] == RUNTIME])}")
    print(f"old-16 already covered by the enumeration: {len(missing)}")
    print(f"old-16 added from the other packages: {len(added)}")
    print(f"total required: {len(rows)}")
    for name in ("TestRuleInformation", "TestRuleInformationComplexCases"):
        hit = [r for r in rows if r["Test"] == name]
        print(f"  {name}: {'REQUIRED in ' + hit[0]['Package'] if hit else 'ABSENT'}")
    print(f"per-package: { {p: sum(1 for r in rows if r['Package'] == p) for p in sorted({r['Package'] for r in rows})} }")


main()
