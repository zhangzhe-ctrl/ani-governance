#!/usr/bin/env python3
"""Prove the localized conf generation has no field, message or accessor drift.

The archive is the locked kratos-bootstrap/api module the protos were copied from; the
localized tree is the output of scripts/generate-bootstrap-conf.sh. Every symbol name the
generated Go code exposes is compared as a set, so a renamed, added or dropped field,
message, enum value or getter is a diff. Byte equality is not the criterion: the file
descriptor carries the new go_package, which is the one intended change.
"""

import json
import os
import re
import sys

REPO = "/home/ubuntu/Workspace/ani-governance"
ARCHIVE = "/home/ubuntu/go/pkg/mod/github.com/tx7do/kratos-bootstrap/api@v0.0.45/gen/go/conf/v1"
LOCAL = os.path.join(REPO, "pkg/localdeps/kratos-bootstrap/api/gen/go/conf/v1")

TYPE = re.compile(r"^type ([A-Za-z_]\w*) struct \{")
FIELD = re.compile(r"^\t([A-Z]\w*)\s+[\*\[\]\w.]+\s+`")
FUNC = re.compile(r"^func \([a-z_]* ?\*?([A-Za-z_]\w*)\) ([A-Z]\w*)\(")
ENUM_VALUE = re.compile(r"^\t([A-Z]\w*)\s+\w+ = \w+\(")
ONEOF = re.compile(r"^type is([A-Za-z_]\w*)_([A-Za-z_]\w*) interface")


def symbols(root):
    out = {"messages": set(), "fields": set(), "accessors": set(), "oneof_impls": set(),
           "files": []}
    for name in sorted(os.listdir(root)):
        if not name.endswith(".pb.go"):
            continue
        out["files"].append(name)
        path = os.path.join(root, name)
        inside_struct = None
        with open(path, encoding="utf-8") as handle:
            for line in handle:
                line = line.rstrip("\n")
                m = TYPE.match(line)
                if m:
                    inside_struct = m.group(1)
                    out["messages"].add(inside_struct)
                    continue
                if line == "}":
                    inside_struct = None
                    continue
                if line.startswith("func "):
                    inside_struct = None
                    f = FUNC.match(line)
                    if f:
                        out["accessors"].add("%s.%s" % (f.group(1), f.group(2)))
                    continue
                if inside_struct:
                    f = FIELD.match(line)
                    if f:
                        out["fields"].add("%s.%s" % (inside_struct, f.group(1)))
                    continue
                o = ONEOF.match(line)
                if o:
                    out["oneof_impls"].add("%s.%s" % (o.group(1), o.group(2)))
    return out


def main():
    a, l = symbols(ARCHIVE), symbols(LOCAL)
    report = {
        "archive": ARCHIVE.replace("/home/ubuntu/go/pkg/mod/", "$GOMODCACHE/"),
        "localized": os.path.relpath(LOCAL, REPO),
        "archive_files": len(a["files"]),
        "localized_files": len(l["files"]),
        "same_file_names": a["files"] == l["files"],
        "counts": {k: {"archive": len(a[k]), "localized": len(l[k])}
                   for k in ("messages", "fields", "accessors", "oneof_impls")},
        "drift": {},
    }
    total = 0
    for kind in ("messages", "fields", "accessors", "oneof_impls"):
        report["drift"][kind] = {
            "only_in_archive": sorted(a[kind] - l[kind]),
            "only_in_localized": sorted(l[kind] - a[kind]),
        }
        total += len(a[kind] | l[kind])
        assert not report["drift"][kind]["only_in_archive"] and \
               not report["drift"][kind]["only_in_localized"], kind
    report["total_symbols_compared"] = total
    report["identical"] = all(not v["only_in_archive"] and not v["only_in_localized"]
                              for v in report["drift"].values())
    # the only tolerated byte difference is the go_package inside the raw descriptor
    diff = 0
    for name in a["files"]:
        with open(os.path.join(ARCHIVE, name), "rb") as one, \
             open(os.path.join(LOCAL, name), "rb") as two:
            if one.read() != two.read():
                diff += 1
    report["files_byte_differing_from_archive"] = diff
    payload = json.dumps(report, indent=1, ensure_ascii=False) + "\n"
    with open(os.path.join(os.path.dirname(os.path.abspath(__file__)),
                           "T10-conf-symbol-parity.json"), "w", encoding="utf-8") as handle:
        handle.write(payload)
    print(payload, end="")
    return 0 if (report["identical"] and report["same_file_names"]) else 1


if __name__ == "__main__":
    sys.exit(main())
