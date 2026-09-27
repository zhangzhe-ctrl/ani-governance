# T15 pre-push security check (public repository)

Scope: the 99 commits in `d27847b..0c976ed`, i.e. everything that would newly become public on
`review/tx7do-takeover`. Remote `main` is exactly `d27847b`, so nothing in the range is published yet.

Tool and rules: gitleaks v8.30.1 (`go version -m` verified) with the repository's own
`.gitleaks.toml` (`[extend] useDefault = true`), the same pinned scanner and rules the
`verify-gpu-audit` gate uses. History mode, output redacted, JSON report kept on the review
machine (see `migration/review/T15-evidence-manifest.md`).

    gitleaks git --redact --log-opts "d27847b99479553b1a56fc84bc4ee8f1a0992e1e..HEAD" .
    99 commits scanned, 13.91 MB, leaks found: 5

## Findings and their verification

All five are `generic-api-key` hits produced by keyword adjacency, and every underlying value is
already published content in this repository. No real credential, token, private key, database
dump, private request body, module cache or `_agent/` material is in the range.

| # | commit | file | what matched | verification |
|---|---|---|---|---|
| 1 | `46cd217` | `migration/patches/T15/T15-sse-and-openapi-tail.json` | a 40-hex **git SHA** preceded by the key name `sse_test_and_openapi_tail` (substring `api`) | commit ids of this branch; the key was renamed in `bcbe789`, so HEAD is clean; the historical line is a commit hash |
| 2 | `20c676d` | `migration/patches/T10/T10-B3-gitleaks-exceptions.json` | `secret_sha256` + 64-hex | equals `sha256("eyJpZCI6MTAwfQ==")` → prefix `7bbcf775`: the pagination cursor example printed by `pkg/localdeps/go-crud/pagination/README.md` |
| 3 | `20c676d` | same | same | equals `sha256("eyJpZCI6MjAwfQ==")` → prefix `33e57c18`: the second documented cursor example |
| 4 | `20c676d` | same | same | equals `sha256(<jwt engine test token>)` → prefix `c615d094`: the token asserted by `pkg/localdeps/kratos-authn/engine/jwt/jwt_test.go`, signed with the literal key `"test"` written two lines above it in the same file |
| 5 | `20c676d` | same | same | equals `sha256("b7907194f4ef659c…")` → prefix `966d8abd`: the digest of `migration/patches/T05/_orig/kratos-authn-engine-metadata.go`, itself a Go source file in this repository |

So findings 2-5 are **digests of digests of repository content**: strings recorded at a time when the
ledger stored full digests, later shortened to `secret_sha256_first8` in the current tree (which is
why `gitleaks dir` over HEAD reports nothing). A SHA-256 of a value that is already published in the
same repository reveals no secret and cannot authenticate anything.

Working-tree scan at the pushed HEAD: `gitleaks dir` reports **no leaks**.

No history was rewritten (no amend, rebase, squash or filter), no rule was disabled, no
directory-wide exemption was added, and `.gitleaksignore` was not created or extended: the accepted
mechanism in this program is a per-rule, per-path, per-value allowlist in `.gitleaks.toml`, and the
five items above need none because none of them is a credential.

## CI that this branch will trigger

One workflow exists, `.github/workflows/gpu-quota.yml` ("GPU quota data gate"), and its trigger is a
bare `push:` plus `pull_request:`, so it **will run** on `review/tx7do-takeover`.

Checked against the "must not deploy/publish/write a business environment" condition:

- `permissions: contents: read` - the job token cannot write repository content.
- No `environment:`, no deploy action, no `gh release`, no registry login or push, no
  `workflow_dispatch` chaining, no cloud credentials, no secrets referenced at all.
- Its database is a job-scoped `postgres:16.10` service container on the runner with trust auth,
  created and destroyed with the job; it never points at a business DSN.
- Steps: pinned tool installs, `scripts/ci-quota-pg.sh` (explicit migration + restricted role),
  lab certificate prep, `make verify-gpu`, `actions/upload-artifact` of the evidence directory
  (30-day retention), and a final echo of the coverage boundary.

So this is normal build/test/audit only, and pushing is permitted. Nothing was disabled, no
protection rule was touched and no check was bypassed.

## Deliberately not pushed

`_agent/` (untracked task inputs), the task directory with raw logs, DSN files, the R5
administrator password and access-key file, the extracted pre-takeover tree, the locked upstream
module copies, the module cache, and the two historical database dumps (which are not on this
machine at all - see `migration/receipts/T15-r6.json`).
