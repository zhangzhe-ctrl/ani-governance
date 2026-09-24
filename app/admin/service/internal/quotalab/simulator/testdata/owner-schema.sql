-- Explicit quota_lab test fixture migration; never run by service startup.
CREATE TABLE IF NOT EXISTS sim_commands (
		operation_id      text PRIMARY KEY,
		tenant_id         uuid NOT NULL,
		resource_id       text NOT NULL,
		create_operation_id text,
		kind              text NOT NULL,           -- create / delete
		status            text NOT NULL,           -- accepted(持久接受) / completed / aborted
		actor             text NOT NULL,
		request_hash      text NOT NULL,
		name              text,
		gpu_count         int,
		charge_id         text NOT NULL,
		created_at        timestamptz NOT NULL DEFAULT now(),
		updated_at        timestamptz NOT NULL DEFAULT now()
	);
CREATE UNIQUE INDEX IF NOT EXISTS uix_sim_commands_create_resource
		ON sim_commands (tenant_id, resource_id) WHERE kind = 'create';
CREATE TABLE IF NOT EXISTS sim_units (
		charge_id     text NOT NULL,
		unit_ordinal  int  NOT NULL,
		tenant_id     uuid NOT NULL,
		resource_id   text NOT NULL,
		state         text NOT NULL,               -- allocated / released / aborted
		updated_at    timestamptz NOT NULL DEFAULT now(),
		PRIMARY KEY (charge_id, unit_ordinal)
	);
CREATE TABLE IF NOT EXISTS sim_release_facts (
		charge_id     text NOT NULL,
		unit_ordinal  int  NOT NULL,
		tenant_id     uuid NOT NULL,
		released_at   timestamptz NOT NULL DEFAULT now(),
		PRIMARY KEY (charge_id, unit_ordinal)
	);
CREATE TABLE IF NOT EXISTS sim_notify_queue (
		event_id     text PRIMARY KEY,
		charge_id    text NOT NULL,
		operation_id text NOT NULL,
		tenant_id    uuid NOT NULL,
		payload_hash text NOT NULL,
		payload_json text NOT NULL,
		reason       text NOT NULL,
		state        text NOT NULL DEFAULT 'pending', -- pending / delivered
		attempt      int NOT NULL DEFAULT 0,
		created_at   timestamptz NOT NULL DEFAULT now(),
		updated_at   timestamptz NOT NULL DEFAULT now()
	);
CREATE TABLE IF NOT EXISTS sim_control_log (
		id         bigserial PRIMARY KEY,
		action     text NOT NULL,
		detail     text,
		created_at timestamptz NOT NULL DEFAULT now()
	);
