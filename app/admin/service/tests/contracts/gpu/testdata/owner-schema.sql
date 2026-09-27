-- Explicit fixture migration; never executed by owner process startup.
CREATE TABLE test_gpu_resources (
 tenant_id uuid NOT NULL,
 resource_id uuid NOT NULL,
 create_operation_id uuid NOT NULL,
 canonical_request text NOT NULL,
 closed boolean NOT NULL DEFAULT false,
 create_executed boolean NOT NULL DEFAULT false,
 PRIMARY KEY (tenant_id, resource_id),
 UNIQUE (tenant_id,resource_id,create_operation_id)
);
CREATE TABLE test_gpu_commands (
 tenant_id uuid NOT NULL,
 operation_id uuid NOT NULL,
 resource_id uuid NOT NULL,
 create_operation_id uuid NOT NULL,
 request_hash text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('CREATE','DELETE')),
 ack_json text NOT NULL,
 PRIMARY KEY(tenant_id,operation_id),
 FOREIGN KEY(tenant_id,resource_id,create_operation_id) REFERENCES test_gpu_resources(tenant_id,resource_id,create_operation_id)
);
CREATE TABLE test_gpu_notifications (
 tenant_id uuid NOT NULL,
 event_id uuid NOT NULL,
 resource_id uuid NOT NULL,
 create_operation_id uuid NOT NULL,
 payload text NOT NULL,
 acknowledged boolean NOT NULL DEFAULT false,
 attempt_count bigint NOT NULL DEFAULT 0,
 PRIMARY KEY(tenant_id,event_id),
 FOREIGN KEY(tenant_id,resource_id,create_operation_id) REFERENCES test_gpu_resources(tenant_id,resource_id,create_operation_id)
);
