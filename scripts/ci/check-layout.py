#!/usr/bin/env python3
"""Check current repository layout and the formal server/admin dependency graph."""

from __future__ import annotations

import argparse
import json
import pathlib
import re
import subprocess
import sys


RETIRED_PREFIXES = (
    "migration/", "docs/evidence/", "third_party/",
    "app/admin/service/cmd/quota-gpu-simulator/",
    "app/admin/service/internal/quotalab/",
    "scripts/model-lab/", "scripts/network-lab/",
)
RETIRED_REFERENCES = (
    "cmd/quota-gpu-simulator", "internal/quotalab", "scripts/model-lab/",
    "scripts/network-lab/", "migration/pgv-scope.json",
    "migration/patches/T15/T15-tool-lock.json", "gpu-quota.yml",
)
TESTUTIL = "go-wind-admin/app/admin/service/tests/testutil"


def run(*args: str, cwd: pathlib.Path) -> bytes:
    return subprocess.run(args, cwd=cwd, check=True, stdout=subprocess.PIPE).stdout


def packages(raw: str) -> list[dict]:
    decoder = json.JSONDecoder()
    result = []
    index = 0
    while index < len(raw):
        index = len(raw) - len(raw[index:].lstrip())
        if index == len(raw):
            break
        value, consumed = decoder.raw_decode(raw, index)
        result.append(value)
        index = consumed
    return result


def check(repo: pathlib.Path, deps_json: pathlib.Path | None) -> list[str]:
    errors = []
    try:
        tracked = [path.decode() for path in run("git", "ls-files", "-z", cwd=repo).split(b"\0") if path]
    except subprocess.CalledProcessError as exc:
        return [f"git ls-files failed: {exc}"]
    if not tracked:
        errors.append("no tracked files")
    for name in tracked:
        if name.startswith(RETIRED_PREFIXES):
            errors.append(f"retired path tracked: {name}")
        if name.endswith("export_test.go"):
            errors.append(f"cross-package test export: {name}")
        if re.search(r"(?:repo_testkit|_testkit)\.go$", name):
            errors.append(f"test helper in production file: {name}")
        if name.startswith("app/admin/service/tests/contracts/") and name.endswith(".go") and not name.endswith("_test.go"):
            errors.append(f"contract implementation outside test file: {name}")
        if name.startswith("scripts/tests/") and name.endswith(".py") and pathlib.Path(name).name not in {"__init__.py", "check-repo-entrypoints.py"} and not pathlib.Path(name).name.startswith("test_"):
            errors.append(f"script test naming: {name}")
        active = (name == "Makefile" or name.startswith((".github/workflows/", "scripts/ci/", "tools/localdeps/gow/"))
                  or (name.startswith("app/admin/service/") and name.endswith(".go") and not name.endswith("_test.go")))
        if active and (repo / name).is_file() and name != "scripts/ci/check-layout.py":
            content = (repo / name).read_text(errors="replace")
            for retired in RETIRED_REFERENCES:
                if retired in content:
                    errors.append(f"retired reference {retired}: {name}")
            if name.startswith("app/admin/service/") and not name.startswith("app/admin/service/tests/") and name.endswith(".go") and "/ent/" not in name:
                if TESTUTIL in content or re.search(r'"testing"', content):
                    errors.append(f"test dependency in production source: {name}")
                if re.search(r"\b(?:func|type)\s+\w*ForTest\b", content):
                    errors.append(f"test export in production source: {name}")
    try:
        raw = deps_json.read_text() if deps_json else run(
            "go", "list", "-deps", "-json", "./app/admin/service/cmd/server", "./app/admin/service/cmd/admin", cwd=repo
        ).decode()
        deps = packages(raw)
        if not deps:
            errors.append("empty formal dependency graph")
        seen = {dep.get("ImportPath") for dep in deps}
        for target in ("go-wind-admin/app/admin/service/cmd/server", "go-wind-admin/app/admin/service/cmd/admin"):
            if target not in seen:
                errors.append(f"missing formal target: {target}")
        for dep in deps:
            path = dep.get("ImportPath", "")
            if path == TESTUTIL or path.startswith(TESTUTIL + "/") or "/quotalab" in path:
                errors.append(f"test/lab package in formal dependency graph: {path}")
            if dep.get("Error") or dep.get("Incomplete"):
                errors.append(f"incomplete formal dependency: {path}")
    except (OSError, subprocess.CalledProcessError, json.JSONDecodeError) as exc:
        errors.append(f"formal dependency graph unavailable: {exc}")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--repo", type=pathlib.Path, required=True)
    parser.add_argument("--deps-json", type=pathlib.Path, help="fixture for checker tests only")
    args = parser.parse_args()
    errors = check(args.repo.resolve(), args.deps_json)
    for error in errors:
        print(error, file=sys.stderr)
    if errors:
        return 1
    print("layout and formal dependency graph: pass")
    return 0


if __name__ == "__main__":
    sys.exit(main())
