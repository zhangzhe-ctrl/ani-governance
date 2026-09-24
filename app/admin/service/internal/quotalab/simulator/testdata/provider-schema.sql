-- Explicit quota_lab test fixture migration; never run by service startup.
CREATE TABLE IF NOT EXISTS sim_provider_ops (
		operation_id         text PRIMARY KEY,
		tenant_id            uuid NOT NULL,
		resource_id          text NOT NULL,
		execution_generation bigint NOT NULL DEFAULT 0,
		closed               boolean NOT NULL DEFAULT false,
		created_at           timestamptz NOT NULL DEFAULT now()
	);
CREATE TABLE IF NOT EXISTS sim_allocations (
		resource_id  text NOT NULL,
		ordinal      int  NOT NULL,
		tenant_id    uuid NOT NULL,
		operation_id text NOT NULL,
		state        text NOT NULL DEFAULT 'allocated', -- allocated / freed
		updated_at   timestamptz NOT NULL DEFAULT now(),
		PRIMARY KEY (resource_id, ordinal)
	);
CREATE TABLE IF NOT EXISTS sim_provider_capacity (
		id         int PRIMARY KEY DEFAULT 1,
		total      int NOT NULL,
		check (id = 1)
	);
INSERT INTO sim_provider_capacity (id, total) VALUES (1, 64) ON CONFLICT (id) DO NOTHING;
