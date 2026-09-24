-- Fault fixture: a persisted terminal projection whose acknowledgement was lost.
-- This does not touch the business ledger or the immutable target payload.
UPDATE sys_gpu_usage_sync SET acked_revision=0,lease_until=NULL,next_attempt_at=NULL;
