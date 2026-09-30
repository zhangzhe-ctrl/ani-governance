package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
	"go-wind-admin/pkg/localdeps/go-crud/entgo/mixin"
)

// ModelDevAcceptance is the immutable CPU admission and its pending dispatch.
// It is deliberately independent of quota operations, accounts and charges.
type ModelDevAcceptance struct{ ent.Schema }

func (ModelDevAcceptance) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{
		Table: "sys_modeldev_acceptances",
		Checks: map[string]string{
			"sys_modeldev_acceptances_tenant_positive_ck": "tenant_id > 0",
			"sys_modeldev_acceptances_action_ck": "action = 'modeldev.execution.create'",
		},
	}}
}

func (ModelDevAcceptance) Mixin() []ent.Mixin {
	return []ent.Mixin{mixin.AutoIncrementId{}, ModelDevAdmissionTenantID{}}
}

func (ModelDevAcceptance) Policy() ent.Policy {
	return &TenantMutationGuardPolicy{}
}

func (ModelDevAcceptance) Fields() []ent.Field {
	return []ent.Field{
		field.String("resource_tenant_id").NotEmpty().Immutable(),
		field.String("actor").NotEmpty().Immutable(),
		field.String("action").NotEmpty().Immutable(),
		field.String("idempotency_key").NotEmpty().Immutable(),
		field.String("operation_id").NotEmpty().Immutable().Unique(),
		field.String("execution_id").NotEmpty().Immutable().Unique(),
		field.String("intent_hash").NotEmpty().Immutable(),
		field.Bytes("intent_canonical").NotEmpty().Immutable(),
		field.String("execution_spec_hash").NotEmpty().Immutable(),
		field.Bytes("snapshot_canonical").NotEmpty().Immutable(),
		field.Time("accepted_at").Immutable(),
		field.Enum("dispatch_state").Values("QUEUED").Default("QUEUED"),
	}
}

func (ModelDevAcceptance) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("resource_tenant_id", "actor", "action", "idempotency_key").Unique().StorageKey("uix_sys_modeldev_acceptances_idempotency"),
		index.Fields("tenant_id", "operation_id").Unique().StorageKey("uix_sys_modeldev_acceptances_tenant_operation"),
		index.Fields("tenant_id", "execution_id").Unique().StorageKey("uix_sys_modeldev_acceptances_tenant_execution"),
	}
}

// Keep the repository's TenantPrivacy policy while requiring a real tenant.
type ModelDevAdmissionTenantID struct{ mixin.TenantID[uint32] }

func (ModelDevAdmissionTenantID) Fields() []ent.Field {
	return []ent.Field{field.Uint32("tenant_id").Positive().Nillable().Immutable()}
}
