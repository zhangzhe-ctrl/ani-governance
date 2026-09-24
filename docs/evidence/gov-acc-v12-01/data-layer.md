# Governance pgx/sqlc data implementation and evidence

This report supplements the historical `data-pause.md` checkpoint. Final joint
version/SHA and CI status are recorded by the coordinating release report. A data
test does not prove mTLS, BFF authorization, provider evidence or actual GPU use.

## Persistence boundary

`QuotaLedgerRepo`, `QuotaAdminRepo`, `PlanQuotaRepo`, dispatch, atomic DELETE,
release receipts and GPU usage synchronization use the generated `quotasql`
methods. `quotaTransaction` obtains a database/sql connection, unwraps the pgx
stdlib connection inside its Raw callback and owns exactly one pgx transaction
until commit/rollback inside that callback. The connection does not escape.
The production `postgres` configuration selects pgx while preserving the shared
pool and tracing configuration. Ent model values remain compatibility DTOs;
there is no second Ent transaction for the same ledger.

All tenant business reads, locks and writes retain tenant identity. Deliberately
global definition/plan catalogs are not tenant business rows. Worker candidate
queries are separately named, bounded and return tenant identity; scoped claim,
read and CAS follow. No RLS policy, tenant GUC or runtime role switching is used.

CREATE stores its complete frozen charge vector. DELETE either cancels/refunds
an unattempted CREATE or records owner deletion under the same transaction and
tenant-first locking order. Independent `sys_gpu_delete_acceptances` persists
DELETE idempotency/results. A repeated local cancellation validates the original
complete vector and fully released totals before accepting a new DELETE key.
GPU callbacks require the complete GPU subset and prior durable DELETE intent;
non-GPU cumulative partial release retains its prior behavior.

Projection leases, retries and ACKs are independent of business dispatch. A new
revision invalidates the previous lease/generation. ACK/Retry/Block require an
actual active claim and matching tenant/revision/generation; a late response
cannot confirm an unclaimed revision or overwrite an acknowledged result.

PlanQuota listing retains the existing query JSON/AIP/structured converters,
nested boolean filters, comparisons, IN/ranges, null operators, case-sensitive
and insensitive text operators, regex and full-text search. Whitelisted JSONPath
predicates and sorting descriptors are bound data to fixed SQL. Multi-column
sorting uses typed database ranks; no SQL statement is assembled in Go.
Timestamp comparisons use PostgreSQL datetime semantics including UTC offsets.
Paired page/page_size, offset/limit, legacy token/offset precedence and noPaging
limits match the existing pagination package. Tampered tokens fail closed.
Field masks and audit DTO fields are preserved.

## Schema and tools

The sole schema authority is `ent/schema` → the pinned Ent exporter plus its
reviewed deferred self-reference constraint → `app/admin/service/schema.sql`.
`sqlc.yaml` consumes that exact export. Atlas migrations are:

- `20260923130000_gpu_sqlc.sql`: required tenant/plan fields, composite relations,
  GPU tables and constraints, preserving unrelated historical users/files data.
- `20260923130100_gpu_no_rls_catalog.sql`: explicit no-RLS/policy cleanup and the
  two formal GPU definitions; no execution capability or quota is enabled.
- `20260923130200_gpu_sync_ref.sql`: full usage ref relation, JSON/null checks,
  revision/state checks and independent retry blocking.

sqlc is v1.30.0; the Fedora binary SHA-256 is
`0d78f76148ed0459d878b101fc61c9955ca73e06b0a5bc2725bb3592d6946266`.
Ent generation is v0.14.6; Atlas community is v0.36.0. Generation evidence records
configuration/schema/query/output checksums. No handwritten duplicate schema is
maintained and service startup never performs these migrations.

## Data evidence mapping

Raw/redacted logs are under `data/`, with resumed runs under `data/resume/`.
The following are individual data assertions, not whole-matrix substitutes.

| IDs / data portion | Actual evidence |
|---|---|
| RLS-01, RLS-02 | `TestQuotaGpuRestrictedRoleAndExplicitIsolation`: current runtime is non-owner, non-superuser, no BYPASSRLS/DDL/TEMP; real catalog has no RLS/policies; TEMP and NULL tenant writes fail. |
| DB-01, SQLC-05 upgrade portion | `old-upgrade-migrations.log`, `upgrade-digest-assert.log`, `TestQuotaGpuOldPopulatedUpgradeReplay`: restore the old backup into a separate database, explicitly apply three migrations, compare 51 old tables unchanged, then replay the original CREATE/receipt and continue cumulative release using the new repo and restricted runtime. Only two definitions and two empty tables are added by migration. |
| TENANT-01, DB-02 | `TestQuotaGpuProjectionNullAndForeignRefConstraints` and composite-FK tests reject null/missing/wrong JSON refs and known other-tenant relations. |
| TENANT-02/03 | Known other-tenant operation/charge reads, same business key in two tenants, scoped ACK/cancel, independent projection CAS and bounded candidate handling. Authenticated transport/cursor coverage is in joint/BFF evidence. |
| TENANT-04 | `tenant-mutation.log`: isolated remote source copy with GetOperation tenant predicate removed fails the actual known-other-tenant assertion, expected exit 1. Mutation never enters delivery source. |
| SQLC-01/02 | Real production constructor tests with tracing on/off plus real PG generic/GPU operations and process commit-fault tests exercise the same pgx transaction and generated queries. |
| SQLC-03/04 | `verify-quota-schema.sh`, `verify-quota-sqlc.sh`: exact regeneration, compile, production SQL/driver/Ent boundary scan and hashes. CI execution is a separate release fact. |
| API-07, SQLC-05 | `TestPlanQuotaPostgresListCompatibility` and old plan tests; `TestQuotaPostgresPolicyChanges` now uses actual sqlc plan writes and asserts occupied 6, limit 4, available 0, over_limit true. HTTP permissions are in BFF evidence. |
| CREATE / DELETE / RELEASE data portions | Existing generic quota PostgreSQL suite plus GPU same-key/vector/atomic-cancel/no-DELETE-refund negatives and complete-charge tests. Actual owner acceptance and cleanup are separate joint facts. |
| CREATE-04, RELEASE-02 data additions | `TestQuotaGpuPlanSwitchSerializesAdmissionAndDatabaseExpiry` holds a real tenant UPDATE lock while admission waits, then verifies the committed replacement plan applies, original idempotency survives, and database-current-time expiry denies a new request. `TestQuotaGpuReleaseNegativeVectorsAtomic` checks partial/excess/duplicate/wrong-code/wrong-charge/wrong-CREATE and mixed vectors; every rejection leaves charges, accounts and receipts unchanged. |
| DELETE-06 data portion | `TestQuotaGpuDeleteInvalidBindingAndChargesAtomic` rejects wrong owner, unknown resource, known resource in another actual tenant, missing GPU/all charges, changed original units and corrupt frozen vector. Full serialized operation/charge/account/acceptance/receipt snapshots remain unchanged, including deliberately damaged fixtures. The public layer has no caller-selected original CREATE/plan: trusted binding locates the original, and DELETE persists its original canonical; service contract validation covers malformed GPU plans. |
| SYNC-05/06 data portions | `TestQuotaGpuSyncLeaseRetryAndBlock`: concurrent workers, real lease expiry/takeover, late generation/revision failures, due-time retry, durable block/payload/error, new-revision recovery, no unclaimed ACK, foreign-tenant CAS denial, unchanged business attempts/balances. RPC failure classification is in joint evidence. |
| DB-03, DELETE-02 | `process/README.md`: ten OS SIGKILL commit points and twelve two-process claim/cancel races; repeated by the final normal/race data gate. |
| DB-04 | Per-table count and canonical JSON digest match after populated dump restore; `restore-continuation.log` claims/ACKs the restored terminal work without reseed/cleanup, preserving invariants. |
| DB-05 | `failed-migration.log` is an intentional failure; `failed-migration-rollback-assert.log` proves transactional DDL rollback and unchanged prior balances/releases. |
| PROC-04 | Separate data race command includes quota concurrency, plan writes, GPU sync and OS process recovery. Do not infer race success from the ordinary suite. |
| PROC-02 due-time data portion | `TestQuotaGpuClaimRechecksDueAfterTenantLock` confirms `pg_locks` reports the claimant actually waiting on the held tenant lock, postpones the scanned operation before releasing that lock, and asserts no claim, no attempt/generation increment and unchanged balances. Slow-owner batching behavior is covered by the coordinator's worker test. |

## Audit scope and reviewed exceptions

`verify-gpu` performs generator/SQL gates, changed-source formatting, formal and
quota_lab builds, data/service/server/pkg tests, real PG, affected race, the
legacy lab suite, and pinned govulncheck v1.7.0/gitleaks v8.30.1 audits. External
joint assembly remains an explicit separate gate. Fixed CI provisioning creates
roles/schema/certificates explicitly; it does not start a migration-enabled app.

The initial gitleaks result is retained: 147 findings consisted of historical
checksum evidence, known development RSA fixtures, rejection fingerprints and
literal documentation/test examples. `.gitleaks.toml` uses exact path plus
secret/line matches; no directory or all-test exemption is used. The two RSA
fixture files are additionally SHA-256 locked in the audit script, and existing
production construction rejects that development key. A changed fixture forces
review. These exceptions do not authorize using those examples as live keys.
The documented cursor example is matched as its exact Base64 value and exact
decoded `test-signing-key-...` value; the scanner's recursive decoding is covered
without exempting the document or arbitrary keys.

govulncheck's resumed scan found zero reachable/called vulnerabilities and
reported additional uncalled package/module advisories. Its full output is
retained rather than describing the dependency graph as advisory-free.

`data/resume/verify-gpu-04.log` and `.meta` record `make verify-gpu` exit 0 on
2026-09-24, 02:45:53–02:53:03 UTC. The run includes actual PostgreSQL tests
(6.654 s), data race including OS fault recovery (54.463 s), service race
(1.172 s), and eight mTLS legacy-lab cases (7.173 s). Schema/sqlc regeneration,
formatting, three builds, affected default suites, vulnerability and secret
scans all passed. This is the pre-publication dependency snapshot; the exact
published Accelerator module and final-source rerun are separate release facts.
Earlier `verify-gpu-02/03` failures and raw/redacted audit findings remain in the
same directory. They are not retroactively relabeled as successful runs.

The final published-module run is `data/resume/verify-gpu-05.log` and `.meta`:
`make verify-gpu` exited 0 at 2026-09-24 03:17:52 UTC (started 03:09:30 UTC),
using Accelerator `v0.0.0-20260924030150-1d32dd9a9173`. PostgreSQL data tests
passed in 11.038 s, plan service PG in 0.183 s, data race in 58.214 s, service
race in 1.177 s and eight legacy lab cases in 8.235 s. This includes the final
DELETE/refund/plan-switch/due-recheck negatives. Audits reported zero reachable
vulnerabilities and zero unallowlisted secret findings. The retained advisories
above still apply. `verify-gpu-05-source.sha256` captures the tested source;
`verify-gpu-05-source-check.log` verifies every captured file was unchanged after
the complete run. Exact Git index/commit parity and hosted CI remain separate
coordinator release checks.

To repeat the task-local gate on Fedora, use the retained isolated data database
and independent lab database; never substitute a joint or restored business
database because the ordinary data suite intentionally resets its test ledger:

```sh
r=/home/chabking/gov-acc-v12-01-20260923
cd "$r/gov"
export GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2
export GOPROXY=https://proxy.golang.org,direct
export GOMODCACHE="$r/cache/gov-data-mod" GOCACHE="$r/cache/gov-data-build"
export SQLC="$r/bin/sqlc" GOV_ACC_TASK_ROOT="$r"
export GOVULNCHECK="$r/acc/.tools/bin/govulncheck" GITLEAKS="$r/acc/.tools/bin/gitleaks"
export QUOTA_LAB_PG_DSN="$(cat "$r/task/gov-data-resume/runtime-dsn")"
flock "$r/locks/gov-data.lock" make verify-gpu
```

## Retained databases and recovery

All execution is on Fedora under the task root
`/home/chabking/gov-acc-v12-01-20260923`, with GOWORK=off, GOMAXPROCS=2, GOFLAGS=-p=2,
private data caches and `locks/gov-data.lock`. The resume suite uses a new
`gov_acc_data_resume` database restored from the formally migrated populated
dump; historical pause, joint, restore, failure and mutation databases remain
intact. `gov_acc_upgrade_resume` separately verifies the populated old backup
upgrade and replay without seeding/cleanup. DSN files stay protected in task-only
directories and are not committed.

Backups are retained on Fedora. Stop new acceptance and relevant workers before
rollback. The old binary cannot safely consume new ACCELERATOR enums/frozen GPU
canonical data, so there is no automatic binary downgrade or destructive DOWN
migration after new business data exists. Restore a retained backup into a new
isolated database and verify rows/recovery before any operator-directed cutover.
