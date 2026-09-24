# Governance data safe-pause checkpoint

User requested pause before final validation. No commit or push was made by the
data agent. This is an incomplete working tree, not a passing delivery candidate.

## Implemented scope

Complete quota ledger, dispatch, cancel/delete, cumulative release, account and
plan operations use the single pgx transaction and generated sqlc query layer.
GPU usage projection and independent delete acceptance persist tenant-preserving
relations. Ent/Atlas remains schema authority; sqlc consumes its exported schema.
Production postgres configuration is adapted to pgx without changing the caller's
configuration; otelsql is unwrapped only inside the database/sql Raw callback.

## Recorded passing evidence

Evidence files are in `data/`; raw populated dumps remain restricted to Fedora.

- `pg-suite-02.log`: original quota/plan and new GPU PostgreSQL tests passed with
  the actual restricted runtime role, before the latest PlanQuota listing extension.
- `race-01.log`: affected quota concurrency, cancel/claim, lease guard and GPU
  data race subset passed before the final listing extension.
- `production-pgx-composition.log`: actual production constructor trace on/off
  used the pgx data layer; startup migration remains rejected.
- `tenant-mutation.log`: remote copy removing GetOperation's tenant restriction
  failed its known-other-tenant assertion, expected exit 1. Mutation is not in
  delivery source.
- `json-constraints.log`: null/missing/wrong JSON refs and cross-tenant composite
  reference negatives passed against real PostgreSQL.
- `schema-verify.log` and `sqlc-verify-final-02.log`: Ent/schema and sqlc exact
  regeneration passed before the latest PlanQuota listing extension. The first
  sqlc verification attempt omitted required SQLC environment and failed; its log
  is retained separately.
- `restore-source-digests.txt`, `restore-target-digests.txt`,
  `restore-digests.diff`: populated source and separately restored database have
  identical per-table counts and canonical row JSON digests; diff is empty.
- `restore-continuation.log`: without reseeding or cleanup, restored revision 2
  sync work was claimed, ACKed, and not claimed again; quota invariants held.
- `failed-migration.log` plus `failed-migration-rollback-assert.log`: injected
  legacy NULL tenant caused the transactional upgrade to fail; prior rows and
  balance/released values remained intact and new DDL rolled back.

DB-03 real OS failures and dual-process races are recorded by the process agent
under `process/`. quota_lab, Network/auth and real BFF are owned by the BFF agent;
they must not be inferred from the data suite results.

## Current failing / unverified work

The latest PlanQuota listing extension preserves query/filter parsing using the
existing converters, typed JSONPath predicates bound to fixed SQL, multi-column
rank ordering, page/offset/noPaging, and audit DTO fields. Its current test run
`plan-compatibility-02.log` exited 1: empty filters build `$ ? (true)`, which
PostgreSQL rejects with SQLSTATE 42601. Nonempty EQ filtering passed before the
same test reached its unfiltered assertion. Do not treat this extension as done.
Resume by correcting the empty predicate and add real-PG coverage for nested
AND/OR, comparisons, IN/range, regex/search, nulls, multiple sort directions,
pagination and masks. Numeric JSON arrays, nullable inequality and legacy token
semantics also need review; no compatibility claim is made for those yet.

`make verify-gpu` was expanded to include builds, relevant default/PG/race tests,
legacy lab and pinned audits, but the full entry point has NOT passed. Its launch
stopped before Make because the format manifest named the newly added
`app/admin/service/tests/gpucontract/capacity_test.go` before that source was copied
to the data agent's remote tree (gofmt lstat error, exit 2). Re-sync the complete
final tree and refresh `scripts/gpu-format-files.txt` before resuming.

The broad initial formatting probe found pre-existing unrelated unformatted
files. The final checker uses all files changed from the accepted baseline plus
a frozen manifest for source archives; it does not mass-format unrelated code.
Latest scanner/CI/audit scripts have not yet passed final execution. CI itself is
not_verified. No final SHA/source manifest or all-matrix completion is claimed.

## Safe retained state and resume

Fedora root: `/home/chabking/gov-acc-v12-01-20260923`.

- Source: `gov/`; private caches `cache/gov-data-mod`, `cache/gov-data-build`.
- `locks/gov-data.lock` was released after the bounded test exited.
- `pause-transactions.log`: task PostgreSQL had zero client backend rows and
  therefore no open client transaction at pause. No data-agent worker remains.
- Container `gov-acc-v12-01-gov-pg`, PostgreSQL 16.10, task label
  `gov-acc-v12-01-gov`, loopback port 25433 is retained intentionally.
- Databases: `gov_acc_data`, `gov_acc_restore`, `gov_acc_failed`,
  `gov_acc_mutation`, `gov_acc_joint_b`, `gov_acc_test_owner`, `gov_acc_lab`,
  `gov_acc_fault`, `gov_acc_formal`, and Atlas `gov_acc_dev`. Do not run cleanup
  helpers against restore/failed/mutation/joint databases.
- Restricted DSN files (mode 0600) remain under `task/joint-b`, `task/lab`,
  `task/gov-fault`, and `task/formal-gov`; no credentials are included here.
- Backups `evidence/gov-data/old-populated.dump`, `new-populated.dump` are retained;
  formal identity-only clone source is `task/formal-gov/identity-snapshot.dump`.
- Fixed tools: sqlc v1.30.0 in `bin/sqlc`; Atlas community v0.36.0 in `bin/atlas`.
  sqlc version and SHA-256 are in generation evidence. All further generation,
  formatting, execution and testing must remain on Fedora with GOWORK=off,
  GOMAXPROCS=2, GOFLAGS=-p=2 and the task-private caches/lock.

No DROP/DOWN, database destruction, credential deletion or dependency publication
was performed during pause. The parent agent controls final resumption.
