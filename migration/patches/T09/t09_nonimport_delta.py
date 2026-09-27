#!/usr/bin/env python3
"""Compare every placed T09 Go/Proto source against the pristine locked archive
and classify each difference as import-string-only or something else.

Pristine source: $GOMODCACHE/.../protoc-gen-go-redact@v0.0.0-20260831125122-5bb4931991b2
(no network, module cache read-only)."""
import hashlib, os, re, subprocess, sys, json

UP = os.path.join(subprocess.run(["go","env","GOMODCACHE"],capture_output=True,text=True).stdout.strip(),
                  "github.com/tx7do/go-wind-toolkit/protoc-gen-go-redact@v0.0.0-20260831125122-5bb4931991b2")
REPO = "/home/ubuntu/Workspace/ani-governance"
PKG  = "pkg/localdeps/go-wind-toolkit/protoc-gen-go-redact"
OLD  = "github.com/tx7do/go-wind-toolkit/protoc-gen-go-redact"
NEW  = "go-wind-admin/pkg/localdeps/go-wind-toolkit/protoc-gen-go-redact"

IMPORT_LINE = re.compile(r'^\s*(?:[\w.]+\s+)?"' + re.escape(OLD) + r'(/[^"]*)?"\s*$')

rows = []
for root, _, files in os.walk(os.path.join(REPO, PKG)):
    for f in sorted(files):
        if not f.endswith(".go"): continue
        local = os.path.join(root, f)
        rel = os.path.relpath(local, os.path.join(REPO, PKG))
        pr = os.path.join(UP, rel.replace(os.sep, "/"))
        if not os.path.exists(pr):
            rows.append({"file": rel, "kind": "new-hand-written", "pristine": None,
                         "note": "authored for T09, not an upstream file"})
            continue
        a = open(pr, encoding="utf-8").read().splitlines()
        b = open(local, encoding="utf-8").read().splitlines()
        if a == b:
            rows.append({"file": rel, "kind": "byte-identical",
                         "sha256": hashlib.sha256("\n".join(a).encode()).hexdigest()})
            continue
        # rewrite only occurrences inside an import spec of the old module path
        diff = [(i, x, y) for i, (x, y) in enumerate(zip(a, b)) if x != y]
        extra = [(i, x, y) for i, x in enumerate(b) if False]
        changed = []
        ok_import_only = True
        ja, jb = list(a), list(b)
        # normalize pristine: swap the old module path for the new one anywhere
        norm = [l.replace(OLD, NEW) for l in a]
        if norm == jb:
            # every difference is exactly the module-path substring
            for i, (x, y) in enumerate(zip(a, b)):
                if x != y: changed.append({"line": i+1, "pristine": x.strip(), "local": y.strip(),
                                           "inside_import_spec": bool(IMPORT_LINE.match(x))})
            if all(c["inside_import_spec"] for c in changed):
                kind = "import-path-rewrite-only"
            else:
                kind = "MODULE-PATH-CHANGE-OUTSIDE-IMPORT-SPEC"
                ok_import_only = False
        else:
            kind = "NON-IMPORT-DIFFERENCE"
            u = subprocess.run(["diff","-u",pr,local],capture_output=True,text=True).stdout
            changed.append({"unified_diff": u})
        rows.append({"file": rel, "kind": kind, "lines_changed": len(changed), "detail": changed})

# redact.proto
prp = os.path.join(UP, "redact/v1/redact.proto")
loc = os.path.join(REPO, "api/localdeps/redact/redact/v1/redact.proto")
h = lambda p: hashlib.sha256(open(p,'rb').read()).hexdigest()
print(json.dumps({"upstream": UP,
                  "redact.proto": {"pristine_sha256": h(prp), "local_sha256": h(loc),
                                   "identical": h(prp)==h(loc)},
                  "go_files": rows}, indent=1, ensure_ascii=False))
