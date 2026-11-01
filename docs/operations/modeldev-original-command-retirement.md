# Four original ModelDev commands

This one-time tool retires only acceptance IDs 55, 57, 59 and 60, tenant 2 / resource tenant `64c5c35a-f554-432c-b4bc-7b4f36f08420`. Their operation/execution IDs and intent/spec SHA256 values are fixed in source. It uses the single original v3 codec pinned to ModelDev `21bf4bd6540d462a61c91b629f26250899583223`. It does not resume the publisher, change canonical bytes, accepted time, actor, identity or quota, or create an HTTP/RPC entry point.

The deployed maintenance branch remains `6c17f0103994620f49c8f6f10c09f414169c6b7d`. This auxiliary branch is built as an operator CLI, not deployed as the service.

Build on the authorized Fedora host:

```sh
GOTOOLCHAIN=auto GOFLAGS=-p=2 go build -trimpath -o /private/bin/modeldev-original-command-retirement ./app/admin/service/cmd/modeldev-original-command-retirement
```

The configuration and referenced DSN files must be regular mode0600 files. Configuration fields are `GovernanceDSNFile`, `ModelDevReadOnlyDSNFile`, and `ModelDev` (the existing `ModelDevClientConfig`: `Address`, `CAFile`, `CertFile`, `KeyFile`, `Timeout` in Go duration nanoseconds). Apply requires a positive sender timeout at most 20 seconds. TLS keeps the existing exact identities `ani-governance` → `ani-modeldev-service`. No insecure transport is available.

Default execution is read only. Both connections use `default_transaction_read_only=on`; MD inventory audit additionally uses a repeatable-read READ ONLY transaction. The plan reports full row SHA256, original actor/time/identity and finite blockers for four targets. It also checks only the fixed original 50 MD and 54 Gov identities/spec hashes with the current original codec; this read scope grants no additional write scope. Audit failures stay visible and require separate original-source diagnosis, never compatibility defaults.

```sh
umask 077
/private/bin/modeldev-original-command-retirement --config /private/config.json > /private/plan.json
sha256sum /private/plan.json
```

The output is canonical JSON without a newline; its byte SHA equals `PLAN_SHA256` on stderr. Review and approve the actual plan before apply. A new plan and approval are needed after every state change, including a failed attempt or lost ACK.

Apply requires explicit `--mode apply`, exactly one allowed `--id`, and either `--phase stop` or `--phase deliver`. `stop` uses unchanged `AcceptStop`, `ClaimCloseDeliveryFor`, production mTLS `ApplyCloseIntent`, and `AckCloseDelivery`. Its receipt is the real durable creation fence. An owner with no Admission reports **CLOSING**, not CLOSED. `deliver` requires the exact saved Stop ACK, atomically locks tenant/acceptance, checks complete row SHA and original identities, rejects active global Create/Close leases and any original quota operation/charge, and allocates the existing delivery lease/generation/attempt counters. It uses production `AcceptExecution`, normal receipt validation and `AckDelivery`/`DeferDelivery`. It never SQL-writes an ACK or owner CLOSED state. ACKED commands cannot send again. Transient failures require a fresh plan and the same original command; no background retry loop is added.

The original MD creation fence is authoritative: late admission retains the Stop, and original `submission.Reserve` refuses its first send permit after that fence. The CLI's owner RPC fixture alone is not evidence of MD/KFP behavior. Required native PG tests include `TestUserStopBeforeAdmissionPersistsClosingTombstoneAcrossNewConnections`, `TestAcceptReceiptAfterEarlierCloseIsFirstAndPreservesCloseOnReplay`, and `TestCloseBeforeReservationRejectsFirstSendPermit`. Actual CLOSED/cleanup qualification still requires the original MD worker and its own facts after delivery.

The root-approved proof is an explicit external evidence trust boundary. `--proof-sha256` must be the byte SHA of a strict private JSON `OriginalCommandProof`; `--sourceproof` must be the reviewed original evidence file whose byte SHA is `SourceProofSHA256`. The tool does not manufacture, sign, or independently verify the capture provenance. Root must verify its full original evidence before approving these hashes; a user-written boolean is not completion evidence.

Required proof fields: `Schema=ani.modeldev.original-command-retirement-proof.v1`, exact `PlanSHA256`, `SourceProofSHA256`, `ConfigurationSHA256` (the private configuration file byte SHA, binding the actual DSN and TLS file references), `GovernanceSource=6c17f0103994620f49c8f6f10c09f414169c6b7d`, `ObservedAt` (UTC, at most two minutes old, not future), `Entry=kubercloud@100.64.128.12`, `Context=kubernetes-admin@ani-kcn-full`, `APIServer=https://lb.kubesphere.local:6443`, `ClusterID=f39bf6e2-f3ed-4191-ada3-caeebf86c220`, `KubeSystemUID=3eeb5379-8748-4bf8-8f6d-d7a48ce3bc24`, `HTTPMethod=POST`, `HTTPPath=/api/v1/modeldev/executions`, `HTTPStatus=503`, `HTTPReason=MODELDEV_CREATE_MAINTENANCE`, actual `HTTPResponseSHA256`, and the actually authorized `StopActor`. Evidence must show the deployed maintenance source, authenticated new Create rejection and original publisher quiescence. The tool rechecks current DB leases immediately before claims. No writes are allowed from a stale or incomplete proof, different cluster, different source, unknown row, changed canonical/row, missing Stop ACK or unexpected accounting.

```sh
/private/bin/modeldev-original-command-retirement --mode apply --phase stop --id 55 --config /private/config.json --plan /private/plan.json --plan-sha256 EXACT_PLAN_SHA --proof /private/proof.json --proof-sha256 EXACT_ROOT_APPROVED_PROOF_SHA --sourceproof /private/source-evidence.json
# Read and approve a new plan and proof before the deliver phase.
/private/bin/modeldev-original-command-retirement --mode apply --phase deliver --id 55 --config /private/config.json --plan /private/new-plan.json --plan-sha256 EXACT_NEW_PLAN_SHA --proof /private/new-proof.json --proof-sha256 EXACT_NEW_ROOT_PROOF_SHA --sourceproof /private/new-source-evidence.json
```

Save every result and actual owner receipt privately. Lost responses are unresolved, not success; the original durable lease and same-command idempotence govern recovery. Never reset a lease or flip retry flags manually. The other 50 MD aggregates, 50 Gov acceptances, quota/account tables, deployments, configuration, KFP and storage are not mutated by this tool.

The opt-in `modeldev_pg,modeldev_original_retirement` tests require the explicitly owned exclusive Gov PG fixture and `OWNER_QUALIFICATION_PRIVATE_INPUT`, a temporary mode0600 original read-only capture. It must not be committed or printed and must be deleted after necessary experiments. Tests use the real persisted decoder, PG claims, mTLS sender and ACK path; only the external owner peer is substituted. Native MD PG tests supply the separate Reserve evidence. No live apply is performed by tests.
