#!/usr/bin/env python3
"""Replay scripts/verify-gpu-format.sh for historical commits without touching the tree.

The gate's file set is `scripts/gpu-format-files.txt` unioned with every Go file that
differs from GOV_GPU_BASE (0fbe1a69 by default) at the revision under test. Because the
second half grows every time a task touches another file, a commit can inherit an old
formatting defect and only report it once some later task edits that same file. Blobs are
extracted to a scratch directory and gofmt'd with the pinned toolchain; nothing in the
repository is written or formatted by this script.
"""

import os
import subprocess
import sys

REPO = "/home/ubuntu/Workspace/ani-governance"
GATE = ["bash", "/home/ubuntu/tx7do-pilot-run/T07/with_lab_env.sh"]
BASE = os.environ.get("GOV_GPU_BASE", "0fbe1a69e49cc23dc7a1696b62f68c34a7c6a48a")
SCRATCH = "/tmp/t10_gate_replay"


def git(*args):
    return subprocess.run(["git", "-C", REPO] + list(args), capture_output=True, text=True).stdout


def gofmt(args_paths):
    return subprocess.run(GATE + ["gofmt", "-l"] + args_paths, capture_output=True, text=True).stdout


def report(rev):
    frozen = [l.strip() for l in git("show", "%s:scripts/gpu-format-files.txt" % rev).splitlines()
              if l.strip() and not l.startswith("#")]
    extra = [l.strip() for l in git("diff", "--name-only", "--diff-filter=ACMR", BASE, rev, "--", "*.go").splitlines()
             if l.strip()]
    dirty = []
    for index, path in enumerate(sorted(set(frozen) | set(extra))):
        blob = git("show", "%s:%s" % (rev, path))
        if not blob:
            continue
        os.makedirs(SCRATCH, exist_ok=True)
        tmp = os.path.join(SCRATCH, "%s_%04d.go" % (rev[:7], index))
        with open(tmp, "w", encoding="utf-8") as handle:
            handle.write(blob)
        if gofmt([tmp]).strip():
            dirty.append(path)
        os.remove(tmp)
    return len(set(frozen) | set(extra)), dirty


if __name__ == "__main__":
    for rev in sys.argv[1:]:
        short = git("rev-parse", "--short", rev).strip()
        covered, dirty = report(rev)
        print("%s %-14s covered=%-5d unformatted=%d" % (rev[:7], short, covered, len(dirty)))
        for d in dirty:
            print("        ", d)
