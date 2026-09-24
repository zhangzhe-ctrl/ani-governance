# DB-03 / DELETE-02: actual PostgreSQL process fault tests

`final.log` / `final.exit` record the passing normal run, with UTC start/finish
and `final.source.sha256` for the quota implementation, generated queries and
tests. `initial.log` retains the first passing run before crash-state assertions
were expanded. No production transaction, SQL, configuration or RPC was added.
Final publication SHA/module pairing is recorded by the coordinating task.

The isolated Fedora database `gov_acc_fault` was restored from the explicitly
migrated populated Governance snapshot. It is separate from joint-B and data
verification databases. Tests assert the actual runtime is non-owner,
NOSUPERUSER/NOBYPASSRLS with no database/schema CREATE or TEMP and no RLS/policy.
The runtime DSN remains only in the task's protected remote file.

`TestQuotaProcessCommitRecovery` executes Occupy, local cancel, owner DELETE,
Release and synchronization ACK CAS in separate OS processes. Test-only pgx
QueryTracer pauses immediately before COMMIT is sent, or after PostgreSQL reports
successful COMMIT but before the repository returns. The parent sends SIGKILL,
asserts the actual signal and absence of a response, then reads operation counts,
charge counts, receipts, balances and sync acknowledgment before replay. Each
case is replayed by two fresh OS processes; operations and refunds remain unique.
The ten PID-bearing cases are in the log. Release is the production persistence
transaction used by ReportQuotaRelease; callback mTLS/authentication and owner
reliable notification are verified separately by the joint transport suite.

`TestQuotaProcessDeleteClaimRace` starts two independent OS processes behind a
shared ready barrier for each of 12 rounds. The same CREATE is either canceled
with zero attempts/no claim/full refund, or claimed once with retained quota and
a durable owner DELETE. Both legal outcomes occurred in the recorded run. Every
round checks balance invariants and DELETE replay in another fresh process.

Commands (all executed on Fedora under the exclusive `gov-fault.lock`):

```sh
cd /home/chabking/gov-acc-v12-01-20260923/gov-fault
export GOWORK=off GOMAXPROCS=2 GOFLAGS=-p=2
export GOMODCACHE=/home/chabking/gov-acc-v12-01-20260923/cache/gov-fault-mod
export GOCACHE=/home/chabking/gov-acc-v12-01-20260923/cache/gov-fault-build
export QUOTA_LAB_PG_DSN="$(cat /home/chabking/gov-acc-v12-01-20260923/task/gov-fault/runtime-dsn)"
go test -tags quota_pg ./app/admin/service/internal/data \
  -run '^TestQuotaProcess(CommitRecovery|DeleteClaimRace)$' -count=1 -v -timeout=8m
go test -race -tags quota_pg ./app/admin/service/internal/data \
  -run '^TestQuotaProcess(CommitRecovery|DeleteClaimRace)$' -count=1 -v -timeout=8m
```

The race invocation has a separate `race.log`, exit and UTC timestamps; do not
infer its result from the ordinary test log. These are software transaction
guarantees, not actual GPU execution or formal Inference owner evidence.
