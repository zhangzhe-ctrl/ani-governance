#!/usr/bin/env python3
"""Build migration/patches/T10/T10-required-profile.json (grow-only must-pass set).

Floor: the accepted T09-B1 profile may never shrink. On top of it, every named test
that ran to a pass in the T10 affected run inside (a) a package placed by T10 and
(b) a package whose import specs T10 rewrote is added. Tests that are not in the
must-pass set are not excluded from any run: they still execute, and whatever they
do is reported by name in the receipt.
"""

import json
import os
import sys

BASE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(BASE, "..", "..", ".."))
LOG = sys.argv[1] if len(sys.argv) > 1 else "/home/ubuntu/tx7do-pilot-run/T10/T10-affected-tests.log.jsonl"

def t10_localized_packages():
    """Directories T10 itself placed, taken from the placement manifest."""
    manifest = json.load(open(os.path.join(BASE, "T10-placement.json"), encoding="utf-8"))
    out = set()
    for row in manifest["files"]:
        directory = os.path.dirname(row["file"]).replace("\\", "/")
        out.add("go-wind-admin" if not directory else "go-wind-admin/" + directory)
    out.add("go-wind-admin/pkg/localdeps/kratos-bootstrap/api/gen/go/conf/v1")
    return out

# named upstream defects carried into the repo by the takeover; they run in every gate
# and are reported as failures, they are simply not assertable as passes
INHERITED_DEFECTS = {
    "TestGenerateOrderIdWithTenantIdCollision",
    "TestBootstrapWithNameVersion",
    "TestWhiteListNormalization",
}


def consumer_packages():
    out = set()
    path = os.path.join(REPO, "migration/patches/T10/T10-consumer-files.txt")
    for line in open(path, encoding="utf-8"):
        rel = line.strip()
        if not rel:
            continue
        directory = os.path.dirname(rel).replace("\\", "/")
        out.add("go-wind-admin" if not directory else "go-wind-admin/" + directory)
    return out


def outcomes(path):
    result = {}
    packages = {}
    for line in open(path, encoding="utf-8", errors="replace"):
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            event = json.loads(line)
        except ValueError:
            continue
        action, name, pkg = event.get("Action"), event.get("Test"), event.get("Package") or ""
        if name and action in ("pass", "fail", "skip"):
            result[(pkg, name)] = action
        elif action in ("pass", "fail", "skip"):
            packages[pkg] = action
    return result, packages


def main():
    localized = t10_localized_packages()
    result, packages = outcomes(LOG)
    floor = json.load(open(os.path.join(REPO, "migration/patches/T09/T09B1-required-profile.json"), encoding="utf-8"))
    consumers = consumer_packages()
    rows = {(r["Package"], r["Test"]) for r in floor}
    added_localized, added_consumers, skipped = set(), set(), {}
    for (pkg, name), action in sorted(result.items()):
        if "/" in name:  # subtests are covered by their parent
            continue
        if pkg not in localized and pkg not in consumers:
            continue
        if action != "pass":
            key = "%s#%s" % (pkg, name)
            skipped[key] = action
            continue
        if pkg in localized:
            added_localized.add((pkg, name))
        else:
            added_consumers.add((pkg, name))
    rows |= added_localized | added_consumers
    profile = [{"Package": p, "Test": n} for p, n in sorted(rows)]

    missing = [r for r in profile if result.get((r["Package"], r["Test"])) != "pass"]
    assert not missing, "the must-pass set may only contain tests that actually passed: %s" % missing
    assert {(r["Package"], r["Test"]) for r in floor} <= {(r["Package"], r["Test"]) for r in profile}, \
        "the must-pass set may never shrink below the accepted T09-B1 floor"

    report = {
        "schema_version": "t10-required-profile/1",
        "log": LOG,
        "floor_T09B1": len(floor),
        "total_required": len(profile),
        "added_from_localized_packages": len(added_localized),
        "added_from_rewritten_consumers": len(added_consumers),
        "consumer_packages": sorted(consumers),
        "localized_packages_with_tests": sorted({p for p, _ in added_localized}),
        "packages_without_test_files": sorted(p for p in localized if p not in {k[0] for k in result}),
        "not_assertable_as_pass": skipped,
        "inherited_upstream_defects_still_running": sorted(INHERITED_DEFECTS),
        "excludes_any_run": False,
    }
    with open(os.path.join(BASE, "T10-required-profile.json"), "w", encoding="utf-8") as handle:
        json.dump(profile, handle, indent=1, ensure_ascii=False)
        handle.write("\n")
    with open(os.path.join(BASE, "T10-required-profile.build.json"), "w", encoding="utf-8") as handle:
        json.dump(report, handle, indent=1, ensure_ascii=False)
        handle.write("\n")
    print(json.dumps(report, indent=1, ensure_ascii=False))


if __name__ == "__main__":
    main()
