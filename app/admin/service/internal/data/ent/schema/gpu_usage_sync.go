package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"github.com/tx7do/go-crud/entgo/mixin"
)

// GpuUsageSync is a rebuildable delivery projection, independent of accounting.
// Revision and payload are derived from the immutable CREATE and its charges.
type GpuUsageSync struct{ ent.Schema }

func (GpuUsageSync) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "sys_gpu_usage_sync", Checks: map[string]string{
		"sys_gpu_usage_sync_tenant_positive_ck":   "tenant_id > 0",
		"sys_gpu_usage_sync_revision_positive_ck": `revision IN (1,2) AND acked_revision >= 0 AND acked_revision <= revision AND ((revision=1 AND state='DECLARED') OR (revision=2 AND state='ENDED')) AND ((payload_json::jsonb->>'revision')::bigint=revision AND (payload_json::jsonb->>'state')::bigint=revision) IS TRUE`,
		"sys_gpu_usage_sync_payload_ref_ck":       `(jsonb_typeof(payload_json::jsonb) = 'object' AND payload_json::jsonb->'ref' IS NOT NULL AND jsonb_typeof(payload_json::jsonb->'ref') = 'object' AND (payload_json::jsonb->'ref'->>'tenant_id') IS NOT NULL AND payload_json::jsonb->'ref'->>'tenant_id' = resource_tenant_id AND (payload_json::jsonb->'ref'->>'owner_service') IS NOT NULL AND payload_json::jsonb->'ref'->>'owner_service' = owner_service AND (payload_json::jsonb->'ref'->>'resource_id') IS NOT NULL AND payload_json::jsonb->'ref'->>'resource_id' = resource_id AND (payload_json::jsonb->'ref'->>'create_operation_id') IS NOT NULL AND payload_json::jsonb->'ref'->>'create_operation_id' = operation_id AND (payload_json::jsonb->>'payload_digest') IS NOT NULL AND payload_json::jsonb->>'payload_digest' = payload_hash) IS TRUE`,
	}}}
}
func (GpuUsageSync) Mixin() []ent.Mixin {
	return []ent.Mixin{mixin.AutoIncrementId{}, mixin.TimeAt{}, mixin.TenantID[uint32]{}}
}
func (GpuUsageSync) Fields() []ent.Field {
	return []ent.Field{
		field.String("operation_id").NotEmpty().Immutable(),
		field.String("resource_tenant_id").NotEmpty().Immutable(),
		field.String("owner_service").NotEmpty().Immutable(),
		field.String("resource_id").NotEmpty().Immutable(),
		field.Int64("revision").Default(1),
		field.String("state").NotEmpty(),
		field.Text("payload_json").NotEmpty(),
		field.String("payload_hash").NotEmpty(),
		field.Int64("acked_revision").Default(0),
		field.Int64("lease_generation").Default(0),
		field.String("lease_owner").Optional().Nillable(),
		field.Time("lease_until").Optional().Nillable(),
		field.Int("attempt_count").Default(0),
		field.Bool("retry_blocked").Default(false),
		field.Time("next_attempt_at").Optional().Nillable(),
		field.String("last_error_code").Optional().Nillable(),
	}
}
func (GpuUsageSync) Indexes() []ent.Index {
	return []ent.Index{index.Fields("tenant_id", "operation_id").Unique().StorageKey("uix_sys_gpu_usage_sync_tenant_operation")}
}
