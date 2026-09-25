#!/usr/bin/env python3
"""Classify every added and removed line in the 212 consumer diffs T10 produced.

The rule T10 must hold to is: a consumer edit changes imports and nothing else. `git diff
-U0` against the start commit gives exactly the changed lines; each one is then either
  import        a spec inside an import declaration ("path", optionally aliased)
  import-block  a delimiter or a blank/comment line that only gofmt moved with the block
  other         anything else, which would be a semantic or formatting change outside imports

`other` lines are printed in full, because that list is what a reviewer has to read.
"""

import collections
import json
import os
import re
import subprocess
import sys

REPO = "/home/ubuntu/Workspace/ani-governance"
BASE = "HEAD"
IMPORT_SPEC = re.compile(r'^\s*(?:_|\w+)?\s*"[^"]+"$')
DELIMITER = re.compile(r"^\s*(?:\(|\)|//|/\*|\*)?\s*$")


def git(*args):
    return subprocess.run(["git", "-C", REPO] + list(args), capture_output=True,
                          text=True, check=True).stdout


def classify(path):
    """Return (counts, other_lines) for one file."""
    diff = git("diff", "-U0", BASE, "--", path)
    in_imports = False
    counts = collections.Counter()
    other = []
    for line in diff.splitlines():
        if line.startswith("+++") or line.startswith("---") or line.startswith("@@"):
            # a hunk header carries no content; the import state is re-derived per side below
            if line.startswith("@@"):
                in_imports = False
            continue
        if not (line.startswith("+") or line.startswith("-")):
            continue
        body = line[1:]
        stripped = body.strip()
        if stripped == "import (" or stripped.startswith("import ("):
            in_imports = True
            counts["import-block"] += 1
            continue
        if IMPORT_SPEC.match(body) and body.count('"') == 2:
            counts["import"] += 1
            continue
        if in_imports and DELIMITER.match(body):
            counts["import-block"] += 1
            continue
        if stripped == ")":
            in_imports = False
        counts["other"] += 1
        other.append(line)
    return counts, other


def main():
    files = [l.strip() for l in
             open(os.path.join(REPO, "migration/patches/T10/T10-consumer-files.txt"),
                  encoding="utf-8") if l.strip()]
    totals = collections.Counter()
    per_file = {}
    all_other = []
    for path in files:
        counts, other = classify(path)
        totals.update(counts)
        per_file[path] = dict(counts)
        if other:
            all_other.append({"file": path, "lines": other})
    report = {
        "schema_version": "t10-diff-audit/1",
        "base": BASE,
        "files_audited": len(files),
        "line_classes": dict(totals),
        "files_with_non_import_changed_lines": len(all_other),
        "non_import_changed_lines": all_other,
        "conclusion": "every changed line is an import spec or import-block line"
                      if not all_other else
                      "see non_import_changed_lines",
    }
    out = os.path.join(REPO, "migration/patches/T10/T10-diff-audit.json")
    with open(out, "w", encoding="utf-8") as handle:
        json.dump(report, handle, indent=1, ensure_ascii=False)
        handle.write("\n")
    print(json.dumps({k: report[k] for k in ("files_audited", "line_classes",
                                             "files_with_non_import_changed_lines")}, indent=1))
    for item in all_other[:20]:
        print("%s: %d" % (item["file"], len(item["lines"])))
        for line in item["lines"][:6]:
            print("   ", line)
    return 0


if __name__ == "__main__":
    sys.exit(main())
