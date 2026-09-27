# T15 push-time evidence manifest

Generated for review branch publication. **No log body is reproduced here**: the absolute paths below stay on the review machine, each with a leading sha256 so a specific file can be requested and verified later without dumping everything into Git.

| conclusion | status | local path | sha256(16) | in-repo artifact carrying the conclusion |
|---|---|---|---|---|
| R4 first failure: Ent entry drift (exit 2) | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/r4-verify-gpu.log` | c6cca93f06507c7a | `migration/receipts/T15-r4-verify-gpu.json` |
| R4 second failure: audit leaks (exit 2) | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/r4b-verify-gpu.log` | 550b875bef4da299 | `migration/patches/T15/T15-gitleaks-exception.json` |
| R4 pass over 77dea49 (exit 0) | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/r4c-verify-gpu.log` | 582a8bd2415dc1b2 | `migration/receipts/T15-r4-verify-gpu.json` |
| Final gate over bcbe789 (exit 0, 18m6s) | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/final2-verify-gpu.log` | 8747884b1f9484e4 | `migration/patches/T15/T15-review-index.json` |
| Final fresh no-cache execution (74 pkgs, 0 cached) | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/final2-fresh.log` | d30902da8e272af3 | `migration/patches/T15/T15-review-index.json` |
| Named profile 7/7 (require_tests.py) | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/r3b-required-tests.log` | 1d88a234dba758b1 | `migration/receipts/T15-r3.json` |
| R3a tools-integration failing first run (exit 2) | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/r3a-tools-integration.log` | 9a31499c02c207c2 | `migration/patches/T15/T15-r3a-tools-integration.json` |
| R3a tools-integration pass (exit 0) | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/r3a-tools-integration-2.log` | 68385e91858afa84 | `migration/patches/T15/T15-r3a-tools-integration.json` |
| R3a five named protoc cases (exit 0) | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/r3a-named-cases.log` | 65d0104c338b3bc5 | `migration/patches/T15/T15-r3a-tools-integration.json` |
| R3a benchmark (measurement only) | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/r3a-benchmark.log` | a6a54e452ebaf16e | `migration/patches/T15/T15-r3a-tools-integration.json` |
| R3c captcha tagged candidate (exit 1 by design) | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/r3c/cand-captcha.log` | 4f7b47d384d31cba | `migration/patches/T15/T15-r3c-captcha-known-defects.json` |
| R3c captcha pristine reference (exit 1) | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/r3c/ref-captcha.log` | 39aa1cb8434a01ee | `migration/patches/T15/T15-r3c-captcha-known-defects.json` |
| R3c id candidate / reference (exit 1) | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/r3c/cand-id.log` | 114e1ceff323c856 | `migration/patches/T15/T15-r3c-id-known-defects.json` |
| Ent pre-change proof: gow still re-added 32 lines | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/ent-unify/before-gow.log` | 44349550ad68cf63 | `migration/patches/T15/T15-ent-entry-unification.json` |
| Ent order A (gow then gate) | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/ent-unify/orderA-step1-gow.log` | 44349550ad68cf63 | `migration/patches/T15/T15-ent-entry-unification.json` |
| Ent order B (gate then gow) | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/ent-unify/orderB-step2-gow.log` | 44349550ad68cf63 | `migration/patches/T15/T15-ent-entry-unification.json` |
| Ent feature-list negative control | LOCAL_ABSENT | `/home/ubuntu/tx7do-pilot-run/T15/ent-unify/negctl` | - | `migration/patches/T15/T15-ent-entry-unification.json` |
| Redact sample dependency closure (0 tx7do) | LOCAL_ABSENT | `/home/ubuntu/tx7do-pilot-run/T15/redact-deps` | - | `migration/patches/T15/T15-redact-runtime-dependency.json` |
| API entry matrix A-E (first pass) | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/api-fix/matrix-run2.log` | 47c5f90691f0f366 | `migration/patches/T15/T15-api-entry-unification.json` |
| API tail A2/B/C3 verification | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/api-fix/matrix2.sh` | 4c97535dbc5e259d | `migration/patches/T15/T15-sse-and-openapi-tail.json` |
| SSE original real failure (queue-length assert) | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/final-nocache.log` | db404fb7043dd314 | `migration/patches/T15/T15-inherited-sse-test-race.json` |
| SSE local 100 iterations verified (100 RUN/100 PASS) | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/sse-fix/local-100-v.log` | c6ef69edcb5bef71 | `migration/patches/T15/T15-sse-and-openapi-tail.json` |
| SSE local -race 30 iterations | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/sse-fix/local-patched-race.log` | 4c196135f02471fc | `migration/patches/T15/T15-sse-and-openapi-tail.json` |
| SSE pristine locked copy, patched and unpatched | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/sse-fix/kt` | (directory) | `migration/patches/T15/T15-sse-and-openapi-tail.json` |
| SSE test-only patch file | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/sse-fix/T15-sse-test-only.patch` | 19a71b86a8b017ff | `migration/patches/T15/T15-sse-and-openapi-tail.json` |
| OpenAPI tail patch file | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/sse-fix/T15-openapi-tail.patch` | 6557fae6eb1f971b | `migration/patches/T15/T15-sse-and-openapi-tail.json` |
| R5 legacy-client probe output | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/r5/evidence/probe-legacy.txt` | daa6ad19e8b8f97a | `migration/receipts/T15-r5.json` |
| R5 current-client probe output | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/r5/evidence/probe-new.txt` | 0c53e20f937f5e67 | `migration/receipts/T15-r5.json` |
| R5 14-case normalized comparison | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/r5/evidence/r5-comparison.json` | 0d6d104b04af29c9 | `migration/receipts/T15-r5.json` |
| R5 teardown record incl. the stale-pid mistake | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/r5/evidence/drop-task-db.log` | 2fb5fa31c2e9a695 | `migration/receipts/T15-r5.json` |
| Push-history scan report (5 findings, all digests of repo content) | LOCAL_ONLY | `/home/ubuntu/tx7do-pilot-run/T15/push-scan-history.json` | 0c772547b0b2eaa9 | `migration/review/T15-push-security-check.md` |

Every conclusion above is also stated in the linked in-repo artifact; the local files are the raw command logs, exit codes and preconditions, not additional claims.

- LOCAL_ONLY means the file exists at that path on the review machine and is **not** uploaded by `git push`.
- LOCAL_ABSENT means the path was recorded historically and has since been trimmed or was a transient container/worktree path; the linked artifact still carries the result.
- Deliberately not published: module cache, database dumps, the R5 administrator password and access-key file (mode 600 under the task directory), raw secret-scan values, and the untracked `_agent/` task inputs.
- Paths under `api/gen/go/**` named in the ledgers but absent from Git are the rejected out-of-scope validator candidates from the PGV ruling, plus one throwaway matrix artifact; they are evidence of a decision not taken, not missing files.
