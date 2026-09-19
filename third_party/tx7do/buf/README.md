# Locked Buf source backup

Two modules were exported on Fedora using the official Buf 1.57.2 binary and a task-private cache. `manifest.json` records the exact locked commits, original lock digest, exported file SHA-256 values, dependency digest inputs, commands, and license provenance. Both B5 module digests were recomputed from the exported bytes using the preserved official Buf digest implementation as a reference and match the original lock.

Offline verification after copying this entire directory:

```sh
python3 verify.py
sha256sum -c SHA256SUMS
```

Re-export from the BSR on Fedora (use fresh output directories):

```sh
R=/home/chabking/ani-governance-runs/backend-root-20260919-01
BUF_CACHE_DIR="$R/buf-cache" "$R/buf-bin/buf" export buf.build/go-wind/redact:d1f98995227e44d6b839e578d9fa1466 --all --exclude-imports --output "$R/recheck-redact"
BUF_CACHE_DIR="$R/buf-cache" "$R/buf-bin/buf" export buf.build/tx7do/pagination:7e34dd27013f4c67bb09025c73a73980 --all --exclude-imports --output "$R/recheck-pagination"
```

The BSR modules did not export LICENSE or documentation files even with `--all`. The related pinned Go modules' LICENSE files are preserved separately under `related-upstream-license/`, with their original archive URLs, SHA-256 values and source comparison in `ORIGIN.json`. They are not inserted into the BSR source trees and do not prove which license was attached to those exact BSR commits. Redact differs from its Go-module copy only in line endings; pagination is not byte-identical even after line-ending normalization. The BSR source bytes have not been normalized or replaced.

This backup contains only the two requested module payloads. Pagination's gnostic dependency is recorded by locked commit/digest but its source is not included here. Other protobuf imports and the full generation toolchain are outside this backup. No original buf.lock was modified and no code generation or deployment was run.

The official digest implementation is preserved as `.go.txt` reference files, so it does not add unrelated Go packages to the application module. Source bytes and upstream URLs are unchanged.
