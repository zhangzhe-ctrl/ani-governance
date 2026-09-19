#!/usr/bin/env python3
"""Fedora-only offline module transport from an exact existing Model Git object.

No replace/go.work, source mutation, tags or remote publication. The output is
a file GOPROXY of the original complete module plus an SHA-256 manifest.
"""
import datetime
import hashlib
import json
import pathlib
import subprocess
import sys
import zipfile

repo, output = map(pathlib.Path, sys.argv[1:3])
revision = "474f37a1df638b955cb546104c35830e04ca2ccd"
module = "github.com/zhangzhe-ctrl/ani-model-service"

def git(*args):
    return subprocess.check_output(["git", "-C", str(repo), *args])

stamp = datetime.datetime.fromtimestamp(int(git("show", "-s", "--format=%ct", revision)), datetime.timezone.utc)
version = "v0.0.0-" + stamp.strftime("%Y%m%d%H%M%S") + "-" + revision[:12]
dest = output / module / "@v"
dest.mkdir(parents=True, exist_ok=True)
files = git("ls-tree", "-rz", "--name-only", revision).decode().split("\0")
with zipfile.ZipFile(dest / (version + ".zip"), "w", zipfile.ZIP_DEFLATED) as archive:
    for name in sorted(filter(None, files)):
        if name.startswith(".git"):
            continue
        entry = zipfile.ZipInfo(module + "@" + version + "/" + name, (1980, 1, 1, 0, 0, 0))
        entry.compress_type = zipfile.ZIP_DEFLATED
        archive.writestr(entry, git("show", revision + ":" + name))
(dest / (version + ".mod")).write_bytes(git("show", revision + ":go.mod"))
(dest / (version + ".info")).write_text(json.dumps({"Version": version, "Time": stamp.isoformat().replace("+00:00", "Z")}))
(dest / "list").write_text(version + "\n")
manifest = {"module": module, "version": version, "source_commit": revision, "files": {str(p.relative_to(output)): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(dest.iterdir())}}
(output / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
print(version)
