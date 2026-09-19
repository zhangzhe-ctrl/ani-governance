#!/usr/bin/env python3
"""Offline integrity verification of the two exported BSR module payloads."""
import hashlib
import json
import re
from pathlib import Path

root = Path(__file__).resolve().parent
manifest = json.loads((root / "manifest.json").read_text())
sha = lambda data: hashlib.sha256(data).hexdigest()
shake = lambda data: hashlib.shake_256(data).hexdigest(64)
assert sha((root / manifest["lock_snapshot"]).read_bytes()) == manifest["lock_sha256"]
assert len(manifest["modules"]) == manifest["module_count"] == 2
for module in manifest["modules"]:
    source = root / module["source_dir"]
    files = sorted(p for p in source.rglob("*") if p.is_file())
    assert {p.relative_to(source).as_posix() for p in files} == set(module["source_files"])
    for p in files:
        record = module["source_files"][p.relative_to(source).as_posix()]
        assert sha(p.read_bytes()) == record["sha256"]
        assert p.stat().st_size == record["bytes"]
    # Buf v1.57.2 digest.go and cas-manifest.go are preserved in digest-reference/.
    file_manifest = "".join(f"shake256:{shake(p.read_bytes())}  {p.relative_to(source).as_posix()}\n" for p in files)
    deps = sorted(d["digest"] for d in module["dependencies"])
    cache_deps = sorted(re.findall(r"    digest: (\S+)", (root / module["module_cache_metadata"]).read_text()))
    assert deps == cache_deps
    actual = "b5:" + shake("\n".join(["shake256:" + shake(file_manifest.encode()), *deps]).encode())
    assert actual == module["locked_digest"] == module["computed_digest"]
    assert json.loads((root / module["commit_info"]).read_text())["commit"] == module["commit"]
    assert sha((root / module["license"]["path"]).read_bytes()) == module["license"]["sha256"]
    print(module["module"], module["commit"], "pass", actual)
print("Two locked BSR payloads verified; dependency closure and BSR license provenance remain outside this proof.")
