#!/usr/bin/env python3
"""Build the PGV generation scope from an accepted git commit, never from a candidate tree.

For every accepted *.pb.validate.go under the Buf module root, find the proto source it was
generated from: PGV output usually has no `// source:` header, so the sibling .pb.go header is
used. Nothing is guessed and nothing is counted to match an expected number.
"""
import argparse, collections, json, re, subprocess, sys

SOURCE_RE = re.compile(r"^\s*//\s*source:\s*(\S.*?)\s*$", re.M)

def git_show(rev, path):
    r = subprocess.run(["git", "show", f"{rev}:{path}"], capture_output=True, text=True)
    return r.stdout if r.returncode == 0 else None

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", required=True)
    ap.add_argument("--rev", required=True)
    ap.add_argument("--root", default="api", help="Buf module root in the repository")
    ap.add_argument("--out-domain", default="api/gen/go", help="generated Go output domain")
    ap.add_argument("--out", required=True)
    a = ap.parse_args()
    def git(*args):
        return subprocess.run(["git", "-C", a.repo, *args], capture_output=True, text=True, check=True).stdout

    listing = git("ls-tree", "-r", "--name-only", a.rev).splitlines()
    protos = [p for p in listing if p.endswith(".proto")]
    validators = [p for p in listing if p.startswith(a.out_domain + "/") and p.endswith(".pb.validate.go")]
    gotos = {p for p in listing if p.startswith(a.out_domain + "/") and p.endswith(".go")}

    mapping, unresolved = {}, []
    for v in validators:
        text = git_show(a.rev, v) or ""
        m = SOURCE_RE.search(text)
        src, how = (m.group(1), "own-header") if m else (None, None)
        if src is None:
            sib = v[: -len(".pb.validate.go")] + ".pb.go"
            stext = git_show(a.rev, sib) if sib in gotos else None
            if stext:
                m2 = SOURCE_RE.search(stext)
                if m2:
                    src, how = m2.group(1), "sibling-pb.go-header"
        if src is None:
            unresolved.append({"validator": v, "reason": "no source header on the validator or its sibling .pb.go"})
            continue
        # protoc reports the path as buf passed it, i.e. relative to the template's input
        # directory, so resolve it against the module root's input directories and, failing
        # that, require a unique suffix match in the accepted tree.
        proto_path, rule = None, None
        for cand in (f"{a.root}/protos/{src}", f"{a.root}/{src}", src):
            if git_show(a.rev, cand) is not None:
                proto_path, rule = cand, "input-directory" if cand == f"{a.root}/protos/{src}" else "direct"
                break
        if proto_path is None:
            hits = [x for x in protos if x.endswith("/" + src)]
            if len(hits) == 1:
                proto_path, rule = hits[0], "unique-suffix"
            else:
                unresolved.append({"validator": v, "reason": f"source {src} unresolvable (suffix hits: {len(hits)}) at {a.rev[:7]}"})
                continue
        how = f"{how}+{rule}"
        mapping.setdefault(proto_path, []).append({"validator": v, "via": how})

    dupes = {}
    for src, rows in mapping.items():
        names = {r["validator"].rsplit("/", 1)[-1] for r in rows}
        if len(names) > 1:
            dupes[src] = sorted(names)

    out = {
        "schema_version": "t15-pgv-scope/1",
        "rev": a.rev, "buf_module_root": a.root, "output_domain": a.out_domain,
        "accepted_validators": len(validators),
        "mapped": len(mapping),
        "mapped_validator_rows": sum(len(v) for v in mapping.values()),
        "source_resolution_via": dict(collections.Counter(r["via"] for v in mapping.values() for r in v)),
        "unresolved": unresolved,
        "duplicate_source_collisions": dupes,
        "proto_paths": sorted(mapping),
        "resolution_rules": dict(collections.Counter(r["via"] for v in mapping.values() for r in v)),
        "validators_by_proto": {s: sorted(r["validator"] for r in v) for s, v in sorted(mapping.items())},
    }
    json.dump(out, open(a.out, "w"), indent=2, ensure_ascii=False)
    print(json.dumps({k: out[k] for k in ("accepted_validators", "mapped", "mapped_validator_rows",
                                          "source_resolution_via")}, indent=1))
    print("unresolved:", len(unresolved), "collisions:", len(dupes))
    for u in unresolved[:5]:
        print("  ", u["validator"], "->", u["reason"])
    return 1 if unresolved or dupes else 0

sys.exit(main())
