# Gov / Acc software-contract integration evidence

Final supplement, 2026-09-24: the checkpoint below is retained as historical
evidence. The final published Accelerator module is
`v0.0.0-20260924030150-1d32dd9a9173`. All six joint scenarios completed with
exit 0 in [normal mode](joint-resume/20260924T031542Z/test.log) (140.884s) and
[race mode](joint-resume/20260924T031813Z/test.log) (161.297s), including both
real JWT/HTTP FULL and UNKNOWN capacity windows, mixed-charge recovery,
revoked-grant/disabled/expired-tenant refunds, competing resolved snapshots,
malformed owner ACKs and blocked-worker shutdown, and real mTLS sync failures.
Commands, module sums, UTC times and source manifests are adjacent to each log.
The [current matrix](acceptance-results.md) and [delivery entry](README.md)
supersede the checkpoint's pending-capacity and old-version statements.

This is a partial evidence register, not completion of GOV-ACC-V12-01. Hardware,
production Inference and deployment remain `not_verified`.

The executable test is `app/admin/service/tests/gpucontract`, selected with
`-tags gpu_joint`. `scripts/accelerator-acceptance/run-joint-contract.sh` records
UTC start/end, exit status, Git base and dirty source state in a unique Fedora
run directory. The coordinating final manifest identifies the exact tested
source; an initial base SHA must not be mistaken for a final candidate.

All execution uses Fedora task root
`/home/chabking/gov-acc-v12-01-20260923`. Evidence runs are in `evidence/gov-joint/`;
private DSNs, test certificates and process configuration are in `joint-b/` and
are not repository artifacts. Each failed attempt is retained separately.

## Observed passes before final candidate verification

The latest completed process suite at this checkpoint exited 0 in 8.28 seconds.
It ran two independent Governance OS processes against the same restricted Gov
PostgreSQL database, an independent mTLS owner process with its own restricted
PostgreSQL database, and Accelerator's separately documented B-layer assembly.
Only test process wiring and external hardware observations are fixtures;
acceptance, the common ledger, dispatch, release and usage sync execute the
production implementation.

Actual assertions include:

- Twelve concurrent requests split between two Governance processes accept one
  original operation/resource and one full MiB charge. Changed content conflicts;
  authorized historical replay works with no dispatch adapter or Resolve client;
  a new request in that state fails closed, and unauthorized replay fails.
- The owner commits CREATE then loses its response. Production dispatch retries
  the original command and reaches durable ACK with attempt count at least two.
  No second allocation record or charge is introduced.
- Original usage reaches DECLARED through real SyncGpuUsage. The versioned Acc
  helper writes a nonempty sensitive binding through actual SaveObservation and
  queries it back. A trusted owner callback with no persisted DELETE is rejected
  and original balances are unchanged.
- Persistent DELETE reaches the owner, creates a durable closing tombstone and a
  notification outbox. After the real Gov release transaction commits, the owner
  is killed before its own outbox ACK. A new owner process retries the same event
  and converges with one cumulative refund.
- Usage reaches ENDED while the persisted live observation remains. Direct
  owner ObserveRelease with the trusted tenant metadata rejects an empty scope
  and reports the nonempty actual fixture scope as ALLOCATION_STILL_PRESENT.
  This deliberately distinguishes observed allocations from an owner's asserted
  software close. It is not a demonstration of physical GPU cleanup.
- Deleting a derived sync row while both workers are stopped and restarting
  Governance reconstructs exactly the same payload/hash and acknowledges revision
  2. A separate process is killed after CREATE commit while its independent sync
  worker has not started; restart reconstructs the missing projection. A later
  refund of that old operation is found by periodic rescan.
- QUEUED/attempt-zero cancel persists its DELETE result and full refund atomically;
  a new DELETE key against the canceled CREATE repeats the local outcome without
  owner execution or another refund.
- A real claimed CREATE is deliberately delayed while its persisted DELETE is
  delivered first. The independent owner's tombstone prevents the late CREATE
  from executing. Reliable notification releases the original charge. The final
  tenant account/charge invariant recomputation reports balanced rows.

These assertions contribute to CREATE-01/02/03/06/07, DELETE-01/03/05,
RELEASE-01/04/05/06, SYNC-01/02/03/07, PROC-01/03 and BOUND-03. They do not alone
prove every clause of those IDs. The full result matrix must combine the data,
BFF, process-fault, provider and final-candidate evidence.

## Other evidence boundaries

The private control HTTP listener exists only in `_test.go`; it is not a new
production business API, OpenAPI entry or generic owner RPC. Its fixture user
is not offered as JWT evidence. The separate real HTTP BFF suite covers JWT,
permissions, plan module checks, current-tenant routing and DTO redaction.

The observation helper supplies constructed software sources, including physical
IDs and Pod references, to the real persistence path. It does not collect from
GPU hardware and is not evidence that production owner rendering/cleanup works.
The owner records software command acceptance/closure only and never creates a
Kubernetes workload. Production registry and production images do not contain
this owner.

The next capacity-reference run is still pending at this checkpoint; compilation
of that added test is not marked as its behavioral pass. Final publication,
exact module version, exact-SHA CI and cleanup are also still pending.
