package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"go-wind-admin/pkg/localdeps/go-crud/entgo/mixin"
)

// GpuDeleteAcceptance freezes the user-visible DELETE result. Execution belongs
// exclusively to the referenced quota operation; this table has no worker.
type GpuDeleteAcceptance struct{ ent.Schema }

func (GpuDeleteAcceptance) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "sys_gpu_delete_acceptances", Checks: map[string]string{"sys_gpu_delete_acceptances_tenant_positive_ck": "tenant_id > 0", "sys_gpu_delete_acceptances_result_ck": "result IN ('LOCAL_CANCELED','OWNER_DELETE')"}}}
}
func (GpuDeleteAcceptance) Mixin() []ent.Mixin {
	return []ent.Mixin{mixin.AutoIncrementId{}, mixin.CreatedAt{}, mixin.TenantID[uint32]{}}
}
func (GpuDeleteAcceptance) Fields() []ent.Field {
	return []ent.Field{
		field.String("actor_type").NotEmpty().Immutable(), field.String("actor_id").NotEmpty().Immutable(), field.String("action").NotEmpty().Immutable(), field.String("idempotency_key").NotEmpty().Immutable(), field.String("request_hash").NotEmpty().Immutable(), field.String("create_operation_id").NotEmpty().Immutable(), field.String("delete_operation_id").NotEmpty().Immutable(), field.String("result").NotEmpty().Immutable(),
	}
}
func (GpuDeleteAcceptance) Indexes() []ent.Index {
	return []ent.Index{index.Fields("tenant_id", "actor_type", "actor_id", "action", "idempotency_key").Unique().StorageKey("uix_sys_gpu_delete_acceptances_idempotency"), index.Fields("tenant_id", "delete_operation_id").Unique().StorageKey("uix_sys_gpu_delete_acceptances_operation")}
}
