#!/usr/bin/env python3
"""Classify the repo format gate (scripts/verify-gpu-format.sh) before and after T10.

For every file the gate reports as unformatted in the working tree, decide whether the
gofmt hunks touch import specs only, and whether the same file's HEAD copy is already
unformatted. The gate's own coverage set grows when a task touches a file (it unions a
frozen list with `git diff <base>` and untracked Go files), so a file can be reported
for the first time because of an import edit while the formatting defect it reports is
older than this task. Both facts are kept apart here.
"""

import json
import os
import re
import subprocess
import sys

REPO = "/home/ubuntu/Workspace/ani-governance"
GATE = "/home/ubuntu/tx7do-pilot-run/T07/with_lab_env.sh"
IMPORT_SPEC = re.compile(r'^\s*(?:_|\.\w*|[A-Za-z_][A-Za-z0-9_]*)?\s*"[^"]*"\s*$')


def sh(args):
    return subprocess.run(args, cwd=REPO, capture_output=True, text=True).stdout


def gate_report():
    out = sh(["bash", GATE, "bash", "-c", "bash scripts/verify-gpu-format.sh"])
    return [l.strip() for l in out.splitlines() if l.strip()]


def classify(path):
    diff = sh(["bash", GATE, "gofmt", "-d", path])
    changed = [l[1:] for l in diff.splitlines()
               if l[:1] in "+-" and not l.startswith(("+++", "---"))]
    other = [c.rstrip() for c in changed if not IMPORT_SPEC.match(c.rstrip())]
    tmp = "/tmp/t10_head_copy.go"
    with open(tmp, "w", encoding="utf-8") as handle:
        handle.write(sh(["git", "show", "HEAD:" + path]))
    head_dirty = bool(sh(["bash", GATE, "gofmt", "-l", tmp]).strip())
    return {"file": path,
            "gofmt_changed_lines": len(changed),
            "gofmt_non_import_lines": len(other),
            "non_import_examples": other[:4],
            "head_copy_already_unformatted": head_dirty}


def main():
    base_report = [l.strip() for l in open(sys.argv[1], encoding="utf-8").read().splitlines() if l.strip()] \
        if len(sys.argv) > 1 else []
    current = gate_report()
    rows = [classify(f) for f in sorted(set(current) - set(base_report))]
    report = {
        "gate": "scripts/verify-gpu-format.sh",
        "unformatted_now": sorted(set(current)),
        "unformatted_at_head": sorted(set(base_report)),
        "only_now_T10_surface_area": sorted(set(current) - set(base_report)),
        "only_at_head_fixed_by_T10": sorted(set(base_report) - set(current)),
        "classification_of_the_T10_surface_area": rows,
        "verdict_note": ("import-only hunks are T10's own doing and must be gofmt'd; a file whose "
                         "HEAD copy is already unformatted carries a defect older than T10 that the "
                         "gate only now covers because T10 touched the file"),
    }
    text = json.dumps(report, indent=1, ensure_ascii=False)
    with open(os.path.join(REPO, "migration/patches/T10/T10-format-gate.json"), "w", encoding="utf-8") as handle:
        handle.write(text + "\n")
    print(text)


if __name__ == "__main__":
    main()
