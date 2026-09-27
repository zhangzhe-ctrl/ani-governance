#!/usr/bin/env python3
"""Measure the residual tx7do dependency surface after the T10 takeover.

Two questions have to be answered separately, because a commented-out import and a live
one look identical to grep:

  1. which github.com/tx7do/... packages are really in the build graph, taken from
     `go list -deps` for the default and the quota_lab tag sets;
  2. whether any package still resolved from outside the repository drags the old
     kratos-bootstrap conf/logger/config into that same graph, which would give the
     binary two copies of the type the takeover just replaced.

The second question is answered on the transitive closure of every external survivor, not
on the source text.
"""

import json
import subprocess

REPO = "/home/ubuntu/Workspace/ani-governance"
GATE = ["bash", "/home/ubuntu/tx7do-pilot-run/T07/with_lab_env.sh"]
TAGSETS = {"default": [], "quota_lab": ["-tags", "quota_lab"], "quota_pg": ["-tags", "quota_pg"]}


def deps(tags):
    out = subprocess.run(GATE + ["env", "GOFLAGS=-mod=readonly", "go", "list", "-deps"] + tags +
                         ["./...", "./tools/migration"], cwd=REPO, capture_output=True, text=True)
    assert out.returncode == 0, out.stderr[-2000:]
    return [l.strip() for l in out.stdout.splitlines() if l.strip()]


def main():
    report = {"module": "go-wind-admin", "external_prefix": "github.com/tx7do/"}
    for name, tags in TAGSETS.items():
        graph = deps(tags)
        external = sorted({p for p in graph if p.startswith("github.com/tx7do/")})
        # does any external survivor still reach a package that the takeover localized?
        localized_roots = ("github.com/tx7do/kratos-bootstrap", "github.com/tx7do/go-utils")
        report[name] = {
            "packages_in_graph": len(graph),
            "external_tx7do_packages": external,
            "external_tx7do_count": len(external),
            "localized_roots_still_external": sorted(
                {p for p in external if p.startswith(localized_roots)
                 and not p.startswith("github.com/tx7do/go-wind-toolkit")}),
        }
    # a second copy of conf/logger would show up as the old package in the default graph
    text = json.dumps(report, indent=1, ensure_ascii=False)
    with open("/home/ubuntu/Workspace/ani-governance/migration/patches/T10/T10-residual-deps.json", "w",
              encoding="utf-8") as handle:
        handle.write(text + "\n")
    print(text)


if __name__ == "__main__":
    main()
