#!/usr/bin/env python3
"""T04 known-defect gate — independent of require_tests.py.

The ordinary mandatory-test gate (tools/migration/require_tests.py) keeps its own
missing/skip/fail rules unchanged. This checker covers exactly the tests listed in
migration/required-known-defects.json: locked upstream image-driver tests that are
isolated behind the `upstream_known_defects` build tag and are never allowed to be
reported as passes.

For every listed test the tool requires, in the tagged run under review:
  * a real terminal result, and that result must be `fail`;
  * a failure signature (assertion file:line plus error text) that matches, item by
    item, the signature of an identically organised pristine copy run in the same
    environment;
  * the moved test body to stay byte-for-byte identical to the locked upstream source.

Anything else blocks: missing, skip, timeout, panic, an unfinished run, a compile or
vet failure, an assertion site that moves, a signature that changes, a test that
suddenly passes (XPASS), or any *other* test failing in the tagged run. Only path,
elapsed time and random tokens are normalised; error content is always compared.

Exit codes: 0 all listed tests are KNOWN_BASELINE_FAILURE as expected, 1 otherwise.
"""

import argparse
import json
import re
import sys

TIME_RE = re.compile(r"\(\d+\.\d+(m?s)?\)")
ELAPSED_SUFFIX_RE = re.compile(r"Elapsed:\s*\d+\.\d+")
SITE_RE = re.compile(r"([\w./$+~-]+\.go):(\d+)")
# absolute prefixes carry no meaning across copies, and neither does the package directory
# that follows one: only the file name and line number identify a site
PREFIX_RE = re.compile(r"(/[^\s'\"()]*?)?(pkg/localdeps/go-utils/[A-Za-z0-9_.-]+/|/tmp/[A-Za-z0-9._@+-]+/)")
UUID_RE = re.compile(r"\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b")
HEX_RE = re.compile(r"\b[0-9a-fA-F]{16,}\b")
BASE64_RE = re.compile(r"\b[A-Za-z0-9+/]{24,}={0,2}\b")
PORT_RE = re.compile(r"\b(127\.0\.0\.1|localhost):\d{2,5}\b")


def normalise(text):
    text = TIME_RE.sub("(TIME)", text)
    text = ELAPSED_SUFFIX_RE.sub("", text)
    text = PREFIX_RE.sub("", text)
    text = UUID_RE.sub("<ID>", text)
    text = PORT_RE.sub("<ADDR>", text)
    text = BASE64_RE.sub("<TOKEN>", text)
    text = HEX_RE.sub("<HEX>", text)
    return re.sub(r"\s+", " ", text).strip()


def normalise_site(path):
    """Keep the file name of an assertion site; the directory differs per copy."""
    return path.rsplit("/", 1)[-1]


def read_events(path):
    events = []
    with open(path, encoding="utf-8", errors="replace") as handle:
        for line in handle:
            line = line.strip()
            if not line.startswith("{"):
                continue
            try:
                events.append(json.loads(line))
            except ValueError:
                continue
    return events


def collect(path):
    """Return per-test terminal action, output lines, plus package-level evidence."""
    tests = {}
    package_output = []
    action = "not_run"
    for event in read_events(path):
        act = event.get("Action")
        name = event.get("Test")
        out = event.get("Output") or ""
        pkg = event.get("Package") or ""
        if name:
            slot = tests.setdefault(name, {"action": "not_run", "output": [], "package": pkg})
            slot["output"].append(out)
            if act in ("pass", "fail", "skip"):
                slot["action"] = act
        if act in ("pass", "fail", "skip") and not name:
            action = act
        if not name and out:
            package_output.append(out)
    return action, tests, package_output


def signature(slot):
    """Assertion sites plus the complete failure transcript of one test, normalised.

    Every non-empty output line is kept: dropping anything but the wrapper labels would
    hide the error content that makes the defect identifiable.
    """
    sites, transcript = [], []
    for raw in slot["output"]:
        line = raw.rstrip("\n")
        stripped = line.strip()
        if not stripped or stripped.startswith(("=== RUN", "=== PAUSE", "=== CONT")):
            continue
        for match in SITE_RE.finditer(line):
            sites.append("%s:%s" % (normalise_site(match.group(1)), match.group(2)))
        transcript.append(normalise(stripped))
    return {"sites": sorted(set(sites)), "transcript": transcript}


def extract_blocks(text, names):
    out = {}
    for name in names:
        marker = "func %s(t *testing.T) {" % name
        if marker not in text:
            out[name] = None
            continue
        start = text.index(marker)
        end = text.index("\n}\n", start) + len("\n}\n")
        out[name] = text[start:end].rstrip("\n")
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--list", required=True, help="required-known-defects.json")
    ap.add_argument("--log", required=True, help="go test -json log of the tagged run under review")
    ap.add_argument("--exit", dest="exit_file", required=True, help="file holding that run's exit code")
    ap.add_argument("--reference-log", required=True, help="pristine copy's tagged go test -json log")
    ap.add_argument("--reference-exit", dest="reference_exit_file", required=True)
    ap.add_argument("--label", default="candidate")
    ap.add_argument("--reference-label", default="pristine")
    ap.add_argument("--moved-file", help="tag-owned file holding the isolated tests, to prove "
                                         "they are still verbatim copies of the locked source")
    ap.add_argument("--locked-file", help="archived pristine copy of the upstream test source")
    ap.add_argument("--out", help="write a JSON report here")
    args = ap.parse_args()

    required = [(e["Package"], e["Test"]) for e in json.load(open(args.list, encoding="utf-8"))]
    cand_action, cand, cand_pkg_out = collect(args.log)
    ref_action, ref, ref_pkg_out = collect(args.reference_log)
    cand_exit = open(args.exit_file, encoding="utf-8").read().strip()
    ref_exit = open(args.reference_exit_file, encoding="utf-8").read().strip()

    entries, blocking = [], []

    for pkg, name in required:
        c, r = cand.get(name), ref.get(name)
        entry = {"Package": pkg, "Test": name,
                 "candidate_action": c["action"] if c else "missing",
                 "reference_action": r["action"] if r else "missing"}
        if c is None or r is None:
            entry["status"] = "MISSING_RUN"
            blocking.append("%s: %s" % (entry["status"], name))
            entries.append(entry)
            continue
        cs, rs = signature(c), signature(r)
        entry["candidate_signature"] = cs
        entry["reference_signature"] = rs
        entry["candidate_output_sha256"] = sha_of(c["output"])
        entry["reference_output_sha256"] = sha_of(r["output"])
        panic = [line.strip() for line in c["output"] + r["output"] if "panic:" in line.lower()]

        if panic:
            # reported as itself, with the original text, never folded into the generic bucket
            entry["status"] = "PANIC_PRESERVED"
            entry["panic_lines"] = [normalise(l) for l in dict.fromkeys(panic)]
            blocking.append("PANIC_PRESERVED (reported verbatim, never generalised): %s" % name)
        elif c["action"] == "pass" and r["action"] == "fail":
            entry["status"] = "XPASS"
            blocking.append("XPASS (must be traced, not silently accepted): %s" % name)
        elif c["action"] != "fail":
            entry["status"] = c["action"].upper()
            blocking.append("%s: %s" % (entry["status"], name))
        elif r["action"] != "fail":
            entry["status"] = "REFERENCE_NOT_FAILING"
            blocking.append("%s: %s" % (entry["status"], name))
        elif cs["sites"] != rs["sites"] or cs["transcript"] != rs["transcript"]:
            entry["status"] = "SIGNATURE_MISMATCH"
            blocking.append("SIGNATURE_MISMATCH: %s" % name)
        else:
            entry["status"] = "KNOWN_BASELINE_FAILURE"
        entries.append(entry)

    # any other test failing in the tagged run is a new defect, not inherited debt
    listed = {name for _, name in required}
    others = {n: s["action"] for n, s in cand.items()
              if n not in listed and s["action"] != "pass" and not n.startswith("Example")}
    if others:
        blocking.append("NEW_FAILURES_IN_TAGGED_RUN: %s" % sorted(others))

    if cand_exit == "0":
        blocking.append("TAGGED_RUN_IS_GREEN: the known defects did not reproduce; nothing may be labelled")
    if ref_exit == "0":
        blocking.append("REFERENCE_RUN_IS_GREEN: the pristine copy no longer shows the expected failures")

    verbatim = []
    if args.moved_file and args.locked_file:
        moved_text = open(args.moved_file, encoding="utf-8").read()
        locked_text = open(args.locked_file, encoding="utf-8").read()
        names = [name for _, name in required]
        moved_blocks = extract_blocks(moved_text, names)
        locked_blocks = extract_blocks(locked_text, names)
        for name in names:
            m, l = moved_blocks.get(name), locked_blocks.get(name)
            same = bool(m) and bool(l) and m.rstrip("\n") == l.rstrip("\n")
            verbatim.append({"Test": name, "identical_to_locked_source": same,
                             "sha256_moved": sha_of([m or ""]),
                             "sha256_locked_source": sha_of([l or ""])})
            if not same:
                blocking.append("MOVED_TEST_NOT_VERBATIM: %s" % name)
    panicky = [line for line in cand_pkg_out if "panic:" in line.lower()]
    if panicky:
        blocking.append("PACKAGE_LEVEL_PANIC: %s" % [normalise(l) for l in panicky][:3])

    report = {
        "meaning": ("isolated locked-upstream defects: each must actually run, must fail, and must "
                    "fail the same way as the identically organised pristine copy in the same "
                    "environment; a failure is never recorded as a pass"),
        "candidate_label": args.label,
        "reference_label": args.reference_label,
        "candidate_exit_code": cand_exit,
        "reference_exit_code": ref_exit,
        "candidate_package_action": cand_action,
        "reference_package_action": ref_action,
        "tag": "upstream_known_defects",
        "known_defect_count": len(required),
        "status_counts": {s: sum(1 for e in entries if e["status"] == s)
                          for s in sorted({e["status"] for e in entries})},
        "tests": entries,
        "isolated_file": args.moved_file,
        "locked_source_copy": args.locked_file,
        "moved_tests_verbatim": verbatim,
        "allowed_failure_function_set": [name for _, name in required],
        "normalisation_rules": ["elapsed time", "absolute prefix of the copy under test",
                                "uuid", "long hex", "base64-ish token", "host:port"],
        "error_content_compared": True,
        "blocking": blocking,
        "verdict": "PASS" if not blocking else "BLOCKED",
        "note": ("the fully tagged run is NOT green by design: these %d cases are recorded as "
                 "KNOWN_BASELINE_FAILURE and the run's own non-zero exit code is preserved"
                 % len(required)),
    }
    text = json.dumps(report, indent=1, ensure_ascii=False)
    if args.out:
        with open(args.out, "w", encoding="utf-8") as handle:
            handle.write(text + "\n")
    print(text)
    return 0 if not blocking else 1


def sha_of(lines):
    import hashlib
    return hashlib.sha256("".join(lines).encode("utf-8", "replace")).hexdigest()


if __name__ == "__main__":
    sys.exit(main())
