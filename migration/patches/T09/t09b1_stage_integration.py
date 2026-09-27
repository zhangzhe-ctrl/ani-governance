#!/usr/bin/env python3
"""T09 option D: stage the protoc-driven upstream tests outside the build graph.

integration_test.go and external_template_test.go, plus the examples/ and
testdata/integration/ resources they read, are copied verbatim from the locked
module-cache archive into _integration_staging/ (the leading underscore makes
the go tool ignore the directory, so nothing here can compile, run, skip or
fail by accident). They are NOT deleted and NOT marked not_applicable: they are
registered in migration/required-integration-deferred.json as DEFERRED, to be
executed in the T13 tooling stage and at the latest before the T15 sign-off.

Re-running this script re-derives the manifest from the archive, so the recorded
hashes can always be checked against the pristine source.
"""
import json
import os
import subprocess
import sys

REPO = "/home/ubuntu/Workspace/ani-governance"
MODULE = "github.com/tx7do/go-wind-toolkit/protoc-gen-go-redact"
VERSION = "v0.0.0-20260831125122-5bb4931991b2"
STAGE = f"{REPO}/migration/patches/T09/_integration_staging"
MANIFEST = f"{REPO}/migration/patches/T09/T09-integration-staging.json"
DEFERRED = f"{REPO}/migration/required-integration-deferred.json"

FILES = [
    "integration_test.go",
    "external_template_test.go",
    "examples/CUSTOM_TEMPLATE.md",
    "examples/custom-template.tmpl",
    "examples/tests/message.proto",
    "examples/tests/message.pb.go",
    "examples/tests/message.pb.redact.go",
    "testdata/integration/test.proto",
    "testdata/integration/test.pb.go",
    "testdata/integration/test.pb.redact.go",
    "testdata/integration/test_grpc.pb.go",
]

# Every case in the two files, with the real external dependency each one needs.
# Verified by reading the archive sources, not guessed: protoc is exec'd for code
# generation, `go build -o protoc-gen-redact .` writes a plugin binary into the
# package directory, and some branches then `go build`/`go vet` ./testdata/integration.
CASES = [
    {"Package": MODULE + " (staged)", "Test": "TestIntegrationProtoCompilation", "Kind": "test",
     "File": "integration_test.go", "Line": 19,
     "needs": ["protoc", "protoc-gen-go (--go_out)", "built plugin binary ./protoc-gen-redact",
               "testdata/integration/test.proto", "go build ./testdata/integration", "go vet ./testdata/integration"],
     "writes_into_source_tree": ["protoc-gen-redact", "testdata/integration/test.pb.go",
                                 "testdata/integration/test.pb.redact.go", "testdata/integration/test_grpc.pb.go"],
     "honours_testing_short": True},
    {"Package": MODULE + " (staged)", "Test": "TestGeneratedCodeQuality", "Kind": "test",
     "File": "integration_test.go", "Line": 347,
     "needs": ["os.Stat on testdata/integration/test.proto"], "writes_into_source_tree": [],
     "honours_testing_short": True},
    {"Package": MODULE + " (staged)", "Test": "TestOptionalFieldsInGeneratedCode", "Kind": "test",
     "File": "integration_test.go", "Line": 380,
     "needs": ["protoc", "protoc-gen-go (--go_out)", "built plugin binary ./protoc-gen-redact",
               "testdata/integration/test.proto"],     "writes_into_source_tree": ["protoc-gen-redact", "testdata/integration/test.pb.redact.go"],
     "honours_testing_short": True},
    {"Package": MODULE + " (staged)", "Test": "TestExternalTemplateLoading", "Kind": "test",
     "File": "external_template_test.go", "Line": 16,
     "needs": ["protoc", "protoc-gen-go (--go_out)", "built plugin binary ./protoc-gen-redact",
               "examples/custom-template.tmpl", "redact --template handling"],
     "writes_into_source_tree": ["protoc-gen-redact", "testdata/integration/test.pb.redact.go"],
     "honours_testing_short": False},
    {"Package": MODULE + " (staged)", "Test": "TestTemplateParameterValidation", "Kind": "test",
     "File": "external_template_test.go", "Line": 171,
     "needs": ["nothing external: builds &Module{ModuleBase: &pgs.ModuleBase{}} and calls the production "
               "loadTemplateFromFile with an empty path and a directory (all 6 exec.Command sites in this file are "
               "inside TestExternalTemplateLoading)"],
     "writes_into_source_tree": [], "honours_testing_short": False,
     "note": ("the only staged case that is runnable on this host today; it stays DEFERRED because it lives in the "
              "same file as a protoc-driven case and the file cannot be activated without activating that one. "
              "No repository test executed here may be reported in its place.")},
    {"Package": MODULE + " (staged)", "Test": "BenchmarkCodeGeneration", "Kind": "benchmark",
     "File": "integration_test.go", "Line": 461,
     "needs": ["protoc", "built plugin binary ./protoc-gen-redact", "-bench"],
     "writes_into_source_tree": ["protoc-gen-redact"], "honours_testing_short": True},
]


def sha256(path):
    with open(path, "rb") as fh:
        return __import__("hashlib").sha256(fh.read()).hexdigest()


def main():
    mc = subprocess.run(["go", "env", "GOMODCACHE"], capture_output=True, text=True,
                        check=True, env={**os.environ, "GOFLAGS": ""}).stdout.strip()
    src = f"{mc}/{MODULE}@{VERSION}"
    if not os.path.isdir(src):
        sys.exit(f"FAIL: locked archive absent from the read-only module cache: {src}")

    os.makedirs(STAGE, exist_ok=True)
    entries = []
    for rel in FILES:
        s, d = f"{src}/{rel}", f"{STAGE}/{rel}"
        if not os.path.isfile(s):
            sys.exit(f"FAIL: {rel} is not in the locked archive any more")
        os.makedirs(os.path.dirname(d), exist_ok=True)
        with open(s, "rb") as fh:
            data = fh.read()
        with open(d, "wb") as fh:
            fh.write(data)
        os.chmod(d, 0o444)
        entries.append({"path": rel, "bytes": len(data), "sha256": sha256(d),
                        "archive_sha256": sha256(s), "identical_to_archive": sha256(d) == sha256(s)})
        if not entries[-1]["identical_to_archive"]:
            sys.exit(f"FAIL: staged {rel} is not byte-identical to the archive")

    json.dump({
        "schema_version": "t09-integration-staging/1",
        "purpose": "non-active staging for the protoc-driven upstream tests deferred by the 2026-09-25 T09 authorization (option D)",
        "source_module": MODULE, "source_version": VERSION,
        "source_archive": src,
        "stage_directory": STAGE.replace(REPO + "/", ""),
        "why_outside_the_build": ("the directory name starts with an underscore, which the go tool ignores, so these "
                                  "sources cannot compile, run, skip or fail in any repository command; they are kept "
                                  "verbatim rather than deleted, tagged with a permanent skip, or marked not_applicable"),
        "pristine_no_redact_import_fix_needed": "both files import what they use already; neither is affected by T09-B1",
        "execution_deferred_to": "T13 tooling stage; no later than the T15 final sign-off",
        "execution_requirements_for_that_round": [
            "install or build protoc and the needed plugins in a task-private tool directory with recorded version, "
            "go version -m output and hash; never overwrite the user's GOBIN",
            "run from a disposable work tree / temporary generation directory, because the cases write a plugin "
            "binary and generated .pb files into their own package directory",
            "reuse the single local redact Proto and runtime plus the approved go_package override; never write into "
            "api/gen/go or pkg/localdeps, and never re-introduce an upstream same-named definition",
            "test-harness changes only (tool paths, temp dirs, the approved local generation mapping); assertions and "
            "production code stay as archived, and pre/post must use the same tool set"
        ],
        "files": entries,
        "cases": CASES,
        "was_in_the_original_T09_required_profile": False,
    }, open(MANIFEST, "w"), indent=1)
    open(MANIFEST, "a").write("\n")

    # the ledger entry that must not be forgotten
    deferred = {"schema_version": "t09-integration-deferred/1",
                "note": "DEFERRED-required integration cases. These are not failures and not skips: they are cases "
                        "whose sources and resources are preserved and hashed but whose execution is scheduled for "
                        "the T13 tooling stage (at the latest before T15 sign-off) because protoc is absent on this "
                        "host. T09 and every later checkpoint must restate them as DEFERRED; only a real run with "
                        "recorded tool versions may turn one into pass.",
                "status": "DEFERRED",
                "source_module": f"{MODULE} {VERSION}",
                "staging_manifest": "migration/patches/T09/T09-integration-staging.json",
                "due": {"earliest": "T13 (tooling stage)", "latest": "T15 (final sign-off)"},
                "tests": CASES}
    json.dump(deferred, open(DEFERRED, "w"), indent=1)
    open(DEFERRED, "a").write("\n")

    print(f"staged {len(entries)} files under {STAGE.replace(REPO + '/', '')} (all byte-identical to the archive)")
    for e in entries:
        print(f"  {e['sha256'][:16]}  {e['bytes']:>7}  {e['path']}")
    print(f"registered {len(CASES)} DEFERRED cases -> migration/required-integration-deferred.json")


sys.exit(main())
